package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/0xHoaxen/shogun/pkg/config"
	"github.com/0xHoaxen/shogun/pkg/netguard"
)

const (
	// callTimeout bounds one request to a provider.
	callTimeout = 5 * time.Minute

	dialTimeout = 10 * time.Second

	// defaultRetries is how often a request that failed in a way that can be
	// retried is tried again when MaxRetries is not set.
	defaultRetries = 2
	// retryBase is the wait before the first retry; it doubles after each.
	retryBase = 500 * time.Millisecond

	// maxResponseBytes bounds what is read of a provider's answer.
	maxResponseBytes = 8 << 20
	// maxErrorText bounds how much of a provider's error body is kept.
	maxErrorText = 512

	// finishContentFilter is the finish_reason of an answer the provider held back.
	finishContentFilter = "content_filter"
)

// ErrEmptyAnswer means the provider answered without any choice to read.
var ErrEmptyAnswer = errors.New("llm: provider answered with no choices")

// StatusError is a provider answering with a status other than 2xx.
type StatusError struct {
	StatusCode int
	text       string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("llm: provider answered %d: %s", e.StatusCode, e.text)
}

// OpenAIAPI is the API for any provider that speaks the OpenAI chat
// completions format, which OpenAI, Anthropic, Mistral, OpenRouter and most
// gateways do. It holds no credentials: each call brings the Access of the user
// it is for.
//
// Requests only go to public internet addresses, checked when connecting, so a
// provider address a user typed cannot be used to reach services inside the
// network. AllowPrivateAddresses lifts that for local development and tests.
type OpenAIAPI struct {
	http     *http.Client
	settings apiSettings
}

// APIOption configures NewOpenAIAPI.
type APIOption func(*apiSettings)

type apiSettings struct {
	allowPrivate bool
	timeout      time.Duration
	retries      int
}

// CallTimeout bounds one request to a provider. The default is five minutes.
func CallTimeout(d time.Duration) APIOption {
	return func(s *apiSettings) { s.timeout = d }
}

// MaxRetries sets how often a request that failed with 429, a 5xx or a broken
// connection is tried again. The default is two.
func MaxRetries(n int) APIOption {
	return func(s *apiSettings) { s.retries = n }
}

// AllowPrivateAddresses lets calls go to loopback and private addresses, so a
// fake provider on localhost works. Never use it in production.
func AllowPrivateAddresses() APIOption {
	return func(s *apiSettings) { s.allowPrivate = true }
}

// NewOpenAIAPI returns an API that calls whichever provider each call names.
func NewOpenAIAPI(opts ...APIOption) *OpenAIAPI {
	s := apiSettings{timeout: callTimeout, retries: defaultRetries}
	for _, opt := range opts {
		opt(&s)
	}
	transport := netguard.Transport()
	if s.allowPrivate {
		transport = &http.Transport{
			Proxy:                 nil,
			DialContext:           (&net.Dialer{Timeout: dialTimeout}).DialContext,
			TLSHandshakeTimeout:   dialTimeout,
			ResponseHeaderTimeout: s.timeout,
			MaxIdleConns:          10,
			IdleConnTimeout:       time.Minute,
		}
	}
	return &OpenAIAPI{http: &http.Client{Transport: transport}, settings: s}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model     string        `json:"model"`
	MaxTokens int32         `json:"max_tokens"`
	Messages  []chatMessage `json:"messages"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
			Refusal string `json:"refusal"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens        int64 `json:"prompt_tokens"`
		CompletionTokens    int64 `json:"completion_tokens"`
		PromptTokensDetails struct {
			CachedTokens int64 `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	} `json:"usage"`
}

// Create implements API. It posts to {BaseURL}/chat/completions.
func (a *OpenAIAPI) Create(ctx context.Context, access Access, req CallRequest) (CallResult, error) {
	if access.BaseURL == "" || access.Model == "" || access.APIKey == "" {
		return CallResult{}, ErrProviderNotConfigured
	}
	body, err := json.Marshal(chatRequest{Model: access.Model, MaxTokens: req.MaxTokens, Messages: toChatMessages(req)})
	if err != nil {
		return CallResult{}, fmt.Errorf("encode request: %w", err)
	}
	endpoint := strings.TrimRight(access.BaseURL, "/") + "/chat/completions"

	var resp chatResponse
	for attempt := 0; ; attempt++ {
		resp, err = a.post(ctx, endpoint, access.APIKey, body)
		if err == nil || attempt >= a.settings.retries || !retryable(err) {
			break
		}
		if waitErr := sleep(ctx, retryBase<<attempt); waitErr != nil {
			break
		}
	}
	if err != nil {
		return CallResult{}, redact(err, access.APIKey)
	}
	return toCallResult(resp)
}

// post makes one attempt, bounded by the call timeout.
func (a *OpenAIAPI) post(ctx context.Context, endpoint, key string, body []byte) (chatResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, a.settings.timeout)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return chatResponse{}, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+key)
	res, err := a.http.Do(httpReq)
	if err != nil {
		return chatResponse{}, fmt.Errorf("call provider: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes))
	if err != nil {
		return chatResponse{}, fmt.Errorf("read answer: %w", err)
	}
	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		return chatResponse{}, &StatusError{StatusCode: res.StatusCode, text: errorText(raw)}
	}
	var out chatResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return chatResponse{}, fmt.Errorf("decode answer: %w", err)
	}
	return out, nil
}

func toChatMessages(req CallRequest) []chatMessage {
	msgs := make([]chatMessage, 0, len(req.Messages)+1)
	if req.System != "" {
		msgs = append(msgs, chatMessage{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		msgs = append(msgs, chatMessage{Role: string(m.Role), Content: m.Content})
	}
	return msgs
}

// toCallResult reads the first choice. Cached prompt tokens are reported apart
// from the rest, as Usage counts them.
func toCallResult(resp chatResponse) (CallResult, error) {
	if len(resp.Choices) == 0 {
		return CallResult{}, ErrEmptyAnswer
	}
	choice := resp.Choices[0]
	cached := resp.Usage.PromptTokensDetails.CachedTokens
	return CallResult{
		Text:       choice.Message.Content,
		StopReason: choice.FinishReason,
		Refused:    choice.FinishReason == finishContentFilter || choice.Message.Refusal != "",
		Usage: Usage{
			InputTokens:     clampInt32(resp.Usage.PromptTokens - cached),
			OutputTokens:    clampInt32(resp.Usage.CompletionTokens),
			CacheReadTokens: clampInt32(cached),
		},
	}, nil
}

// retryable says whether another attempt could succeed: the provider was busy
// or failed on its side, or the connection broke. An address the guard refused
// never becomes public.
func retryable(err error) bool {
	if errors.Is(err, netguard.ErrNotPublic) {
		return false
	}
	var status *StatusError
	if errors.As(err, &status) {
		return status.StatusCode == http.StatusTooManyRequests || status.StatusCode >= http.StatusInternalServerError
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// errorText keeps the start of an error body, which is only ever logged.
func errorText(raw []byte) string {
	text := strings.TrimSpace(string(raw))
	if len(text) > maxErrorText {
		text = text[:maxErrorText]
	}
	return text
}

// redactedError keeps err for errors.Is and errors.As but shows text with the
// key removed, because a provider may echo the credentials it was sent.
type redactedError struct {
	text string
	err  error
}

func (e *redactedError) Error() string { return e.text }
func (e *redactedError) Unwrap() error { return e.err }

// redact hides key in err's text. An error that does not contain it is returned
// as it is.
func redact(err error, key string) error {
	if err == nil || key == "" || !strings.Contains(err.Error(), key) {
		return err
	}
	return &redactedError{text: strings.ReplaceAll(err.Error(), key, "[redacted]"), err: err}
}

// clampInt32 narrows a token count to int32, saturating instead of wrapping.
func clampInt32(n int64) int32 {
	switch {
	case n > math.MaxInt32:
		return math.MaxInt32
	case n < 0:
		return 0
	default:
		return int32(n)
	}
}

var (
	_ API   = (*OpenAIAPI)(nil)
	_ error = (*redactedError)(nil)
	_ error = (*StatusError)(nil)
)

// ForEnvironment returns the options for an environment name such as
// config.Base.Environment: private addresses are allowed only in local
// development and tests, where a fake provider on localhost is how the stack runs.
func ForEnvironment(environment string) []APIOption {
	if environment == config.EnvLocal || environment == config.EnvTest {
		return []APIOption{AllowPrivateAddresses()}
	}
	return nil
}
