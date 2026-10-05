package llm

import (
	"context"
	"math"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// AnthropicAPI is the API backed by the Anthropic SDK. It is the only code
// that imports the SDK.
type AnthropicAPI struct {
	client anthropic.Client
}

// NewAnthropicAPI returns an API that authenticates with apiKey. Extra options
// are for tests, such as option.WithBaseURL.
func NewAnthropicAPI(apiKey string, opts ...option.RequestOption) *AnthropicAPI {
	all := append([]option.RequestOption{option.WithAPIKey(apiKey)}, opts...)
	return &AnthropicAPI{client: anthropic.NewClient(all...)}
}

// CountTokens implements API.
func (a *AnthropicAPI) CountTokens(ctx context.Context, req CallRequest) (int32, error) {
	params := anthropic.MessageCountTokensParams{
		Model:    anthropic.Model(req.Model),
		Messages: toMessageParams(req.Messages),
	}
	if req.System != "" {
		params.System = anthropic.MessageCountTokensParamsSystemUnion{
			OfTextBlockArray: []anthropic.TextBlockParam{{Text: req.System}},
		}
	}
	count, err := a.client.Messages.CountTokens(ctx, params)
	if err != nil {
		return 0, err
	}
	return clampInt32(count.InputTokens), nil
}

// Create implements API. The system prompt carries a cache breakpoint, so the
// shared part of a prompt is read from the prompt cache on later calls.
func (a *AnthropicAPI) Create(ctx context.Context, req CallRequest) (CallResult, error) {
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(req.Model),
		MaxTokens: int64(req.MaxTokens),
		Messages:  toMessageParams(req.Messages),
	}
	if req.System != "" {
		params.System = []anthropic.TextBlockParam{{
			Text:         req.System,
			CacheControl: anthropic.NewCacheControlEphemeralParam(),
		}}
	}
	msg, err := a.client.Messages.New(ctx, params)
	if err != nil {
		return CallResult{}, err
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

var _ API = (*AnthropicAPI)(nil)
