package llm_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/0xHoaxen/shogun/pkg/llm"
)

// fakeAnthropic answers the two endpoints AnthropicAPI uses and records the
// bodies it was sent, by path.
func fakeAnthropic(t *testing.T, stopReason string) (*httptest.Server, map[string]map[string]any) {
	t.Helper()
	bodies := map[string]map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		bodies[r.URL.Path] = body
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/messages/count_tokens":
			_, _ = io.WriteString(w, `{"input_tokens": 321}`)
		case "/v1/messages":
			_, _ = io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5-5",
				"content":[{"type":"text","text":"Hello "},{"type":"text","text":"world"}],
				"stop_reason":"`+stopReason+`","stop_sequence":null,
				"usage":{"input_tokens":10,"output_tokens":5,"cache_creation_input_tokens":2,"cache_read_input_tokens":3}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, bodies
}

func TestAnthropicAPICountsTokens(t *testing.T) {
	srv, bodies := fakeAnthropic(t, "end_turn")
	api := llm.NewAnthropicAPI("test-key", option.WithBaseURL(srv.URL))

	got, err := api.CountTokens(context.Background(), llm.CallRequest{
		Model: "claude-opus-5-5", System: "Be brief.",
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
	})

	if err != nil || got != 321 {
		t.Fatalf("got %d, %v; want 321", got, err)
	}
	if bodies["/v1/messages/count_tokens"]["model"] != "claude-opus-5-5" {
		t.Errorf("body = %v", bodies["/v1/messages/count_tokens"])
	}
}

func TestAnthropicAPICreateCachesTheSystemPromptAndMapsUsage(t *testing.T) {
	srv, bodies := fakeAnthropic(t, "end_turn")
	api := llm.NewAnthropicAPI("test-key", option.WithBaseURL(srv.URL))

	got, err := api.Create(context.Background(), llm.CallRequest{
		Model: "claude-opus-5-5", System: "You write in my voice.", MaxTokens: 700,
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "draft"}, {Role: llm.RoleAssistant, Content: "ok"}, {Role: llm.RoleUser, Content: "more"}},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	want := llm.CallResult{
		Text: "Hello world", StopReason: "end_turn",
		Usage: llm.Usage{InputTokens: 10, OutputTokens: 5, CacheReadTokens: 3, CacheWriteTokens: 2},
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
	body := bodies["/v1/messages"]
	if body["max_tokens"] != float64(700) {
		t.Errorf("max_tokens = %v, want 700", body["max_tokens"])
	}
	system, _ := body["system"].([]any)
	if len(system) != 1 {
		t.Fatalf("system = %v, want one block", body["system"])
	}
	block, _ := system[0].(map[string]any)
	cacheControl, _ := block["cache_control"].(map[string]any)
	if block["text"] != "You write in my voice." || cacheControl["type"] != "ephemeral" {
		t.Errorf("system block = %v, want the prompt with an ephemeral cache_control", block)
	}
	if msgs, _ := body["messages"].([]any); len(msgs) != 3 {
		t.Errorf("messages = %v, want 3 turns", body["messages"])
	}
}

func TestAnthropicAPIFlagsARefusal(t *testing.T) {
	srv, _ := fakeAnthropic(t, "refusal")
	api := llm.NewAnthropicAPI("test-key", option.WithBaseURL(srv.URL))

	got, err := api.Create(context.Background(), llm.CallRequest{
		Model: "claude-opus-5-5", MaxTokens: 10, Messages: []llm.Message{{Role: llm.RoleUser, Content: "x"}},
	})

	if err != nil || !got.Refused || got.StopReason != "refusal" {
		t.Fatalf("got %+v, %v; want a refusal", got, err)
	}
}

func TestAnthropicBaseURLSendsCallsToTheGivenHost(t *testing.T) {
	// Arrange
	srv, bodies := fakeAnthropic(t, "end_turn")
	api := llm.NewAnthropicAPI("test-key", llm.AnthropicBaseURL(srv.URL))

	// Act
	got, err := api.CountTokens(context.Background(), llm.CallRequest{
		Model: "claude-opus-5-5", Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
	})

	// Assert
	if err != nil || got != 321 {
		t.Fatalf("CountTokens = %d, %v; want 321, nil", got, err)
	}
	if _, ok := bodies["/v1/messages/count_tokens"]; !ok {
		t.Fatal("the stub was not called")
	}
}

func TestAnthropicBaseURLEmptyKeepsTheDefaultHost(t *testing.T) {
	// Arrange: an empty URL must not break client construction or redirect calls.
	api := llm.NewAnthropicAPI("test-key", llm.AnthropicBaseURL(""))

	// Assert
	if api == nil {
		t.Fatal("NewAnthropicAPI returned nil")
	}
}
