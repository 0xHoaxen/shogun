// Package llm is the only way a service calls Claude. Every call is reserved
// against soroban's budgets before it runs and committed with its real usage
// after, so spend cannot pass a hard limit. If soroban cannot be reached the
// call fails closed: nothing is spent unmetered.
package llm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	sorobanv1 "github.com/0xHoaxen/shogun/gen/go/shogun/soroban/v1"
)

const (
	// cacheWriteSurchargeDivisor sizes the input estimate's margin when a
	// system prompt may be written to the prompt cache, which costs a quarter
	// more than plain input: the estimate is input tokens plus input/4.
	cacheWriteSurchargeDivisor = 4

	// settleTimeout bounds the Commit or Release that follows a call, which
	// runs even when the caller's context is already cancelled.
	settleTimeout = 10 * time.Second

	// commitAttempts is how many times a failing Commit is tried.
	commitAttempts = 3
)

// Errors Complete returns. Use errors.Is.
var (
	// ErrUnknownFeature means the feature is not in the client's config.
	ErrUnknownFeature = errors.New("llm: unknown feature")
	// ErrInvalidRequest means the request has no messages or a bad role.
	ErrInvalidRequest = errors.New("llm: invalid request")
	// ErrBudgetExhausted means a hard budget refused the call. The error is a
	// *BudgetError with the reset time.
	ErrBudgetExhausted = errors.New("llm: budget exhausted")
	// ErrMeteringUnavailable means soroban could not be reached or failed, so
	// the call was not made.
	ErrMeteringUnavailable = errors.New("llm: cost metering unavailable")
	// ErrRefused means the model declined the request. Its usage was metered.
	ErrRefused = errors.New("llm: request refused by the model")
)

// Request is one call to Complete.
type Request struct {
	// System is the system prompt. It is cached by the API across calls.
	System   string
	Messages []Message
	// MaxTokens overrides the feature's output cap; zero uses the feature's.
	MaxTokens int32
	// Cache serves an identical earlier request from memory instead of
	// calling the model, for the config's CacheTTL. Leave it off where a
	// repeat should produce a fresh answer, such as a regenerated draft.
	Cache bool
}

// Response is the model's answer.
type Response struct {
	Text       string
	Model      string
	StopReason string
	Usage      Usage
	// FromCache is set when no call was made.
	FromCache bool
}

// Client calls Claude for one service.
type Client struct {
	cfg   Config
	api   API
	meter Meter
	cache Cache
	log   *slog.Logger
}

// Option configures a Client.
type Option func(*Client)

// WithCache serves requests marked Request.Cache from cache.
func WithCache(cache Cache) Option { return func(c *Client) { c.cache = cache } }

// WithLogger sets the logger. The default discards.
func WithLogger(log *slog.Logger) Option { return func(c *Client) { c.log = log } }

// New returns a Client that runs cfg's features on api, metered by meter.
func New(cfg Config, api API, meter Meter, opts ...Option) (*Client, error) {
	if cfg.Service == "" || len(cfg.Features) == 0 {
		return nil, errors.New("llm: config needs a service and features")
	}
	if api == nil || meter == nil {
		return nil, errors.New("llm: api and meter are required")
	}
	if cfg.CacheTTL <= 0 {
		cfg.CacheTTL = DefaultCacheTTL
	}
	c := &Client{cfg: cfg, api: api, meter: meter, log: slog.New(slog.DiscardHandler)}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// Complete runs req for feature. The context must carry the owner's identity
// (authz.WithIdentity), which the meter call is signed with.
func (c *Client) Complete(ctx context.Context, feature string, req Request) (Response, error) {
	fc, ok := c.cfg.Features[feature]
	if !ok {
		return Response{}, fmt.Errorf("%w: %q", ErrUnknownFeature, feature)
	}
	if err := validate(req); err != nil {
		return Response{}, err
	}
	call := CallRequest{Model: fc.Model, System: req.System, Messages: req.Messages, MaxTokens: fc.MaxTokens}
	if req.MaxTokens > 0 {
		call.MaxTokens = req.MaxTokens
	}

	var key string
	if req.Cache && c.cache != nil {
		key = cacheKey(call)
		if hit, ok := c.cache.Get(key); ok {
			hit.FromCache = true
			return hit, nil
		}
	}

	reservationID, err := c.reserve(ctx, feature, call)
	if err != nil {
		return Response{}, err
	}
	result, err := c.api.Create(ctx, call)
	if err != nil {
		c.release(ctx, reservationID)
		return Response{}, fmt.Errorf("claude call: %w", err)
	}
	c.commit(ctx, reservationID, result.Usage)
	if result.Refused {
		return Response{}, ErrRefused
	}

	resp := Response{Text: result.Text, Model: call.Model, StopReason: result.StopReason, Usage: result.Usage}
	if key != "" {
		c.cache.Set(key, resp, c.cfg.CacheTTL)
	}
	return resp, nil
}

func validate(req Request) error {
	if len(req.Messages) == 0 {
		return fmt.Errorf("%w: no messages", ErrInvalidRequest)
	}
	for _, m := range req.Messages {
		if m.Role != RoleUser && m.Role != RoleAssistant {
			return fmt.Errorf("%w: role %q", ErrInvalidRequest, m.Role)
		}
	}
	return nil
}

// reserve estimates the call and holds its cost, returning the reservation id.
func (c *Client) reserve(ctx context.Context, feature string, call CallRequest) (string, error) {
	inputTokens, err := c.api.CountTokens(ctx, call)
	if err != nil {
		return "", fmt.Errorf("count tokens: %w", err)
	}
	estimate := inputTokens
	if call.System != "" {
		estimate += inputTokens / cacheWriteSurchargeDivisor
	}
	res, err := c.meter.Reserve(ctx, &sorobanv1.ReserveRequest{
		Service:         c.cfg.Service,
		Feature:         feature,
		Model:           call.Model,
		EstInputTokens:  estimate,
		EstOutputTokens: call.MaxTokens,
	})
	if err != nil {
		return "", reserveError(err)
	}
	return res.GetReservationId(), nil
}

// release gives a hold back after a failed call. It runs even if ctx is done.
func (c *Client) release(ctx context.Context, reservationID string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), settleTimeout)
	defer cancel()
	if _, err := c.meter.Release(ctx, &sorobanv1.ReleaseRequest{ReservationId: reservationID}); err != nil {
		// The sweeper gives the hold back when it expires.
		c.log.Error("release reservation", slog.String("reservation_id", reservationID), slog.Any("error", err))
	}
}

// commit records the real usage of a call that ran. It is retried, and runs
// even if ctx is done. The text was already paid for, so a commit that still
// fails is logged for reconciliation and does not discard the answer.
func (c *Client) commit(ctx context.Context, reservationID string, usage Usage) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), settleTimeout)
	defer cancel()
	req := &sorobanv1.CommitRequest{
		ReservationId: reservationID,
		Usage: &sorobanv1.Usage{
			InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens,
			CacheReadTokens: usage.CacheReadTokens, CacheWriteTokens: usage.CacheWriteTokens,
		},
	}
	var err error
	for range commitAttempts {
		if _, err = c.meter.Commit(ctx, req); err == nil {
			return
		}
	}
	c.log.Error("commit usage; spend is unrecorded",
		slog.String("reservation_id", reservationID),
		slog.Int("output_tokens", int(usage.OutputTokens)), slog.Any("error", err))
}

// cacheKey is the hash of everything that decides the answer.
func cacheKey(call CallRequest) string {
	// A struct of strings, numbers and slices always marshals.
	b, _ := json.Marshal(call)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
