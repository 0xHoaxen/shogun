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
	"github.com/0xHoaxen/shogun/pkg/authz"
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
	// ErrProviderNotConfigured means the user has not set an AI provider, so
	// their AI features are off. Nothing was spent.
	ErrProviderNotConfigured = errors.New("llm: no AI provider is configured")
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
	call, err := c.callFor(feature, req)
	if err != nil {
		return Response{}, err
	}

	// A cached answer belongs to the user who paid for it, and only to them. A
	// call that does not say who it is for is never cached.
	var key string
	if req.Cache && c.cache != nil {
		if id, ok := authz.FromContext(ctx); ok && id.OwnerID != "" {
			key = cacheKey(id.OwnerID, call)
		}
	}
	if key != "" {
		if hit, ok := c.cache.Get(key); ok {
			hit.FromCache = true
			return hit, nil
		}
	}

	reservationID, access, err := c.reserve(ctx, feature, call, "")
	if err != nil {
		return Response{}, err
	}
	resp, err := c.run(ctx, reservationID, access, call)
	if err != nil {
		return Response{}, err
	}
	if key != "" {
		c.cache.Set(key, resp, c.cfg.CacheTTL)
	}
	return resp, nil
}

// CompleteWith runs req for feature with the given provider instead of the
// caller's saved one, for a connection test of details the user has not saved.
// The cost is still reserved and committed with soroban, which learns only the
// model name, never the key; soroban allows this for the test feature alone. The
// answer is never cached. Like Complete, the context must carry the identity.
func (c *Client) CompleteWith(ctx context.Context, feature string, access Access, req Request) (Response, error) {
	if access.BaseURL == "" || access.Model == "" || access.APIKey == "" {
		return Response{}, ErrProviderNotConfigured
	}
	call, err := c.callFor(feature, req)
	if err != nil {
		return Response{}, err
	}
	reservationID, _, err := c.reserve(ctx, feature, call, access.Model)
	if err != nil {
		return Response{}, err
	}
	return c.run(ctx, reservationID, access, call)
}

// callFor checks req and builds what is sent for feature.
func (c *Client) callFor(feature string, req Request) (CallRequest, error) {
	fc, ok := c.cfg.Features[feature]
	if !ok {
		return CallRequest{}, fmt.Errorf("%w: %q", ErrUnknownFeature, feature)
	}
	if err := validate(req); err != nil {
		return CallRequest{}, err
	}
	call := CallRequest{System: req.System, Messages: req.Messages, MaxTokens: fc.MaxTokens}
	if req.MaxTokens > 0 {
		call.MaxTokens = req.MaxTokens
	}
	return call, nil
}

// run makes the call a reservation allows, then commits its usage, or releases
// the hold if the call failed.
func (c *Client) run(ctx context.Context, reservationID string, access Access, call CallRequest) (Response, error) {
	result, err := c.api.Create(ctx, access, call)
	if err != nil {
		c.release(ctx, reservationID)
		return Response{}, fmt.Errorf("claude call: %w", err)
	}
	c.commit(ctx, reservationID, result.Usage)
	if result.Refused {
		return Response{}, ErrRefused
	}
	return Response{Text: result.Text, Model: access.Model, StopReason: result.StopReason, Usage: result.Usage}, nil
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

// reserve estimates the call, holds its cost against the user's budgets and
// returns the reservation id with the provider to make the call with. The
// estimate is made here, not by the provider: counting tokens would need the
// user's key before their budget has been checked, and many OpenAI-compatible
// providers have no token-counting endpoint at all.
//
// unsavedModel is set for a test of details the user has not saved: soroban then
// prices the call by that model and hands back no provider.
func (c *Client) reserve(ctx context.Context, feature string, call CallRequest, unsavedModel string) (string, Access, error) {
	estimate := EstimateInputTokens(call.System, call.Messages)
	if call.System != "" {
		estimate += estimate / cacheWriteSurchargeDivisor
	}
	req := &sorobanv1.ReserveRequest{
		Service:         c.cfg.Service,
		Feature:         feature,
		EstInputTokens:  estimate,
		EstOutputTokens: call.MaxTokens,
	}
	if unsavedModel != "" {
		req.UnsavedModel = &unsavedModel
	}
	res, err := c.meter.Reserve(ctx, req)
	if err != nil {
		return "", Access{}, reserveError(err)
	}
	if unsavedModel != "" {
		return res.GetReservationId(), Access{}, nil
	}
	provider := res.GetProvider()
	access := Access{BaseURL: provider.GetBaseUrl(), Model: provider.GetModel(), APIKey: provider.GetApiKey()}
	if access.BaseURL == "" || access.Model == "" || access.APIKey == "" {
		// Never fall back to other credentials: give the hold back and stop.
		c.release(ctx, res.GetReservationId())
		return "", Access{}, ErrProviderNotConfigured
	}
	return res.GetReservationId(), access, nil
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

// cacheKey is the hash of everything that decides the answer, including whose
// it is, so that no two users ever share an entry.
func cacheKey(owner string, call CallRequest) string {
	// A struct of strings, numbers and slices always marshals.
	b, _ := json.Marshal(struct {
		Owner string
		Call  CallRequest
	}{owner, call})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
