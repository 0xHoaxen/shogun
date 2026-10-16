package llm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"math"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/0xHoaxen/shogun/pkg/config"
	"github.com/0xHoaxen/shogun/pkg/netguard"
)

const (
	// maxClients bounds the SDK clients kept for reuse. Past it the pool starts
	// over, which only costs a few reconnects.
	maxClients = 256

	// callTimeout bounds one request to a provider.
	callTimeout = 5 * time.Minute

	dialTimeout = 10 * time.Second
)

// AnthropicAPI is the API backed by the Anthropic SDK. It is the only code that
// imports the SDK. It holds no credentials: each call brings the Access of the
// user it is for.
//
// Requests only go to public internet addresses, checked when connecting, so a
// provider address a user typed cannot be used to reach services inside the
// network. AllowPrivateAddresses lifts that for local development and tests.
type AnthropicAPI struct {
	http     *http.Client
	settings anthropicSettings

	mu      sync.Mutex
	clients map[string]anthropic.Client
}

// AnthropicOption configures NewAnthropicAPI.
type AnthropicOption func(*anthropicSettings)

type anthropicSettings struct {
	allowPrivate bool
	timeout      time.Duration
	retries      int
}

// CallTimeout bounds one request to a provider. The default is five minutes.
func CallTimeout(d time.Duration) AnthropicOption {
	return func(s *anthropicSettings) { s.timeout = d }
}

// MaxRetries sets how often the SDK retries a request that failed in a way that
// can be retried. The SDK's own default applies when this is not set.
func MaxRetries(n int) AnthropicOption {
	return func(s *anthropicSettings) { s.retries = n }
}

// AllowPrivateAddresses lets calls go to loopback and private addresses, so a
// fake provider on localhost works. Never use it in production.
func AllowPrivateAddresses() AnthropicOption {
	return func(s *anthropicSettings) { s.allowPrivate = true }
}

// NewAnthropicAPI returns an API that calls whichever provider each call names.
func NewAnthropicAPI(opts ...AnthropicOption) *AnthropicAPI {
	s := anthropicSettings{timeout: callTimeout, retries: -1}
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
	return &AnthropicAPI{http: &http.Client{Transport: transport}, settings: s, clients: map[string]anthropic.Client{}}
}

// clientFor returns the SDK client for an access, reusing one for the same
// address and key so its connections are reused too. The key is never kept as
// map key, only its hash.
func (a *AnthropicAPI) clientFor(access Access) anthropic.Client {
	sum := sha256.Sum256([]byte(access.APIKey))
	id := access.BaseURL + "\x00" + hex.EncodeToString(sum[:])
	a.mu.Lock()
	defer a.mu.Unlock()
	if c, ok := a.clients[id]; ok {
		return c
	}
	if len(a.clients) >= maxClients {
		a.clients = map[string]anthropic.Client{}
	}
	opts := []option.RequestOption{
		option.WithAPIKey(access.APIKey),
		option.WithBaseURL(access.BaseURL),
		option.WithHTTPClient(a.http),
		option.WithRequestTimeout(a.settings.timeout),
	}
	if a.settings.retries >= 0 {
		opts = append(opts, option.WithMaxRetries(a.settings.retries))
	}
	c := anthropic.NewClient(opts...)
	a.clients[id] = c
	return c
}

// Create implements API. The system prompt carries a cache breakpoint, so the
// shared part of a prompt is read from the prompt cache on later calls.
func (a *AnthropicAPI) Create(ctx context.Context, access Access, req CallRequest) (CallResult, error) {
	if access.BaseURL == "" || access.Model == "" || access.APIKey == "" {
		return CallResult{}, ErrProviderNotConfigured
	}
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(access.Model),
		MaxTokens: int64(req.MaxTokens),
		Messages:  toMessageParams(req.Messages),
	}
	if req.System != "" {
		params.System = []anthropic.TextBlockParam{{
			Text:         req.System,
			CacheControl: anthropic.NewCacheControlEphemeralParam(),
		}}
	}
	client := a.clientFor(access)
	msg, err := client.Messages.New(ctx, params)
	if err != nil {
		return CallResult{}, redact(err, access.APIKey)
	}
	var text strings.Builder
	for _, block := range msg.Content {
		if t, ok := block.AsAny().(anthropic.TextBlock); ok {
			text.WriteString(t.Text)
		}
	}
	return CallResult{
		Text:       text.String(),
		StopReason: string(msg.StopReason),
		Refused:    msg.StopReason == anthropic.StopReasonRefusal,
		Usage: Usage{
			InputTokens:      clampInt32(msg.Usage.InputTokens),
			OutputTokens:     clampInt32(msg.Usage.OutputTokens),
			CacheReadTokens:  clampInt32(msg.Usage.CacheReadInputTokens),
			CacheWriteTokens: clampInt32(msg.Usage.CacheCreationInputTokens),
		},
	}, nil
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

func toMessageParams(messages []Message) []anthropic.MessageParam {
	params := make([]anthropic.MessageParam, 0, len(messages))
	for _, m := range messages {
		block := anthropic.NewTextBlock(m.Content)
		if m.Role == RoleAssistant {
			params = append(params, anthropic.NewAssistantMessage(block))
			continue
		}
		params = append(params, anthropic.NewUserMessage(block))
	}
	return params
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
	_ API   = (*AnthropicAPI)(nil)
	_ error = (*redactedError)(nil)
)

// ForEnvironment returns the options for an environment name such as
// config.Base.Environment: private addresses are allowed only in local
// development and tests, where a fake provider on localhost is how the stack runs.
func ForEnvironment(environment string) []AnthropicOption {
	if environment == config.EnvLocal || environment == config.EnvTest {
		return []AnthropicOption{AllowPrivateAddresses()}
	}
	return nil
}
