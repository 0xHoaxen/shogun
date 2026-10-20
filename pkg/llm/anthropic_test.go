package llm_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/0xHoaxen/shogun/pkg/llm"
	"github.com/0xHoaxen/shogun/pkg/netguard"
)

// seenRequest is one request a fake provider received.
type seenRequest struct {
	path string
	key  string
	body map[string]any
}

// fakeAnthropic answers the messages endpoint and records what it was sent.
func fakeAnthropic(t *testing.T, stopReason string) (*httptest.Server, func() []seenRequest) {
	t.Helper()
	var (
		mu       sync.Mutex
		received []seenRequest
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		mu.Lock()
		received = append(received, seenRequest{r.URL.Path, r.Header.Get("X-Api-Key"), body})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/v1/messages" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5-5",
			"content":[{"type":"text","text":"Hello "},{"type":"text","text":"world"}],
			"stop_reason":"`+stopReason+`","stop_sequence":null,
			"usage":{"input_tokens":10,"output_tokens":5,"cache_creation_input_tokens":2,"cache_read_input_tokens":3}}`)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []seenRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]seenRequest(nil), received...)
	}
}

func accessFor(srv *httptest.Server, key string) llm.Access {
	return llm.Access{BaseURL: srv.URL, Model: "claude-opus-5-5", APIKey: key}
}

func TestAnthropicAPICreateUsesTheAccessItIsGiven(t *testing.T) {
	srv, requests := fakeAnthropic(t, "end_turn")
	api := llm.NewAnthropicAPI(llm.AllowPrivateAddresses())

	_, err := api.Create(context.Background(), llm.Access{BaseURL: srv.URL, Model: "my-model-7", APIKey: "key-of-ada"}, llm.CallRequest{
		MaxTokens: 10, Messages: []llm.Message{{Role: llm.RoleUser, Content: "x"}},
	})

	got := requests()
	if err != nil || len(got) != 1 || got[0].key != "key-of-ada" || got[0].body["model"] != "my-model-7" {
		t.Fatalf("err %v, requests %+v; want the key and model of the access on the wire", err, got)
	}
}

func TestOneAPIServesManyUsersWithoutMixingTheirKeys(t *testing.T) {
	srv, requests := fakeAnthropic(t, "end_turn")
	api := llm.NewAnthropicAPI(llm.AllowPrivateAddresses())
	call := llm.CallRequest{MaxTokens: 10, Messages: []llm.Message{{Role: llm.RoleUser, Content: "x"}}}

	for _, key := range []string{"key-of-ada", "key-of-bob", "key-of-ada", "key-of-cy"} {
		if _, err := api.Create(context.Background(), accessFor(srv, key), call); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	var keys []string
	for _, r := range requests() {
		keys = append(keys, r.key)
	}
	if strings.Join(keys, ",") != "key-of-ada,key-of-bob,key-of-ada,key-of-cy" {
		t.Fatalf("keys on the wire = %v; each call must carry its own user's key", keys)
	}
}

func TestAnthropicAPICreateCachesTheSystemPromptAndMapsUsage(t *testing.T) {
	srv, requests := fakeAnthropic(t, "end_turn")
	api := llm.NewAnthropicAPI(llm.AllowPrivateAddresses())

	got, err := api.Create(context.Background(), accessFor(srv, "k"), llm.CallRequest{
		System: "You write in my voice.", MaxTokens: 700,
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
	body := requests()[0].body
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
	api := llm.NewAnthropicAPI(llm.AllowPrivateAddresses())

	got, err := api.Create(context.Background(), accessFor(srv, "k"), llm.CallRequest{
		MaxTokens: 10, Messages: []llm.Message{{Role: llm.RoleUser, Content: "x"}},
	})

	if err != nil || !got.Refused || got.StopReason != "refusal" {
		t.Fatalf("got %+v, %v; want a refusal", got, err)
	}
}

func TestAProviderOnThisMachineIsRefusedUnlessPrivateAddressesAreAllowed(t *testing.T) {
	// Arrange: a "provider" on loopback is how a user would try to reach services
	// inside the network through this one.
	srv, requests := fakeAnthropic(t, "end_turn")
	api := llm.NewAnthropicAPI()

	// Act
	_, err := api.Create(context.Background(), accessFor(srv, "k"), llm.CallRequest{
		MaxTokens: 10, Messages: []llm.Message{{Role: llm.RoleUser, Content: "x"}},
	})

	// Assert
	if !errors.Is(err, netguard.ErrNotPublic) {
		t.Fatalf("err = %v, want ErrNotPublic", err)
	}
	if got := requests(); len(got) != 0 {
		t.Fatalf("the provider on this machine was reached %d times", len(got))
	}
}

func TestAnErrorFromTheProviderNeverContainsTheKey(t *testing.T) {
	// A provider that echoes the credentials it was sent must not put them in our logs.
	const key = "sk-very-secret-key-123456"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"type":"error","error":{"type":"authentication_error","message":"bad key `+r.Header.Get("X-Api-Key")+`"}}`)
	}))
	defer srv.Close()
	api := llm.NewAnthropicAPI(llm.AllowPrivateAddresses())

	_, err := api.Create(context.Background(), accessFor(srv, key), llm.CallRequest{
		MaxTokens: 10, Messages: []llm.Message{{Role: llm.RoleUser, Content: "x"}},
	})

	if err == nil {
		t.Fatal("want the provider's refusal")
	}
	if strings.Contains(err.Error(), key) {
		t.Fatalf("the error repeats the key: %v", err)
	}
}

func TestAnthropicAPIRefusesAnIncompleteAccess(t *testing.T) {
	api := llm.NewAnthropicAPI(llm.AllowPrivateAddresses())
	call := llm.CallRequest{MaxTokens: 10, Messages: []llm.Message{{Role: llm.RoleUser, Content: "x"}}}
	for name, access := range map[string]llm.Access{
		"no address": {Model: "m", APIKey: "k"},
		"no model":   {BaseURL: "https://llm.example.com", APIKey: "k"},
		"no key":     {BaseURL: "https://llm.example.com", Model: "m"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := api.Create(context.Background(), access, call); !errors.Is(err, llm.ErrProviderNotConfigured) {
				t.Fatalf("err = %v, want ErrProviderNotConfigured", err)
			}
		})
	}
}

func TestPrivateAddressesAreAllowedOnlyInLocalDevelopmentAndTests(t *testing.T) {
	srv, requests := fakeAnthropic(t, "end_turn")
	call := llm.CallRequest{MaxTokens: 10, Messages: []llm.Message{{Role: llm.RoleUser, Content: "x"}}}
	for environment, allowed := range map[string]bool{"local": true, "test": true, "staging": false, "production": false, "": false} {
		t.Run(environment, func(t *testing.T) {
			before := len(requests())

			_, err := llm.NewAnthropicAPI(llm.ForEnvironment(environment)...).Create(context.Background(), accessFor(srv, "k"), call)

			if allowed && err != nil {
				t.Fatalf("a fake provider on localhost was refused in %q: %v", environment, err)
			}
			if !allowed && (!errors.Is(err, netguard.ErrNotPublic) || len(requests()) != before) {
				t.Fatalf("err = %v in %q, want ErrNotPublic and no request", err, environment)
			}
		})
	}
}

// replyWith is a provider that always answers with the given status.
func replyWith(t *testing.T, status int) llm.Access {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, `{"type":"error","error":{"type":"x","message":"secret detail from the provider"}}`)
	}))
	t.Cleanup(srv.Close)
	return accessFor(srv, "k")
}

func TestClassifyFailureNamesWhatWentWrongWithoutTheProvidersWords(t *testing.T) {
	call := llm.CallRequest{MaxTokens: 10, Messages: []llm.Message{{Role: llm.RoleUser, Content: "x"}}}
	api := llm.NewAnthropicAPI(llm.AllowPrivateAddresses(), llm.MaxRetries(0))
	tests := []struct {
		status int
		want   llm.FailureKind
	}{
		{http.StatusUnauthorized, llm.FailureRejected},
		{http.StatusForbidden, llm.FailureRejected},
		{http.StatusTooManyRequests, llm.FailureRateLimited},
		{http.StatusNotFound, llm.FailureNotFound},
		{http.StatusBadRequest, llm.FailureNotFound},
		{http.StatusInternalServerError, llm.FailureProviderError},
		{http.StatusBadGateway, llm.FailureProviderError},
	}
	for _, tt := range tests {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			_, err := api.Create(context.Background(), replyWith(t, tt.status), call)

			if got := llm.ClassifyFailure(err); got != tt.want {
				t.Fatalf("ClassifyFailure = %q for status %d (err %v), want %q", got, tt.status, err, tt.want)
			}
		})
	}
}

func TestClassifyFailureSeesThroughTheClientsWrapping(t *testing.T) {
	// Complete wraps the provider's error, and a draft job wraps that again.
	api := llm.NewAnthropicAPI(llm.AllowPrivateAddresses(), llm.MaxRetries(0))
	_, err := api.Create(context.Background(), replyWith(t, http.StatusUnauthorized), llm.CallRequest{
		MaxTokens: 10, Messages: []llm.Message{{Role: llm.RoleUser, Content: "x"}},
	})

	wrapped := fmt.Errorf("draft job: %w", fmt.Errorf("claude call: %w", err))

	if got := llm.ClassifyFailure(wrapped); got != llm.FailureRejected {
		t.Fatalf("ClassifyFailure = %q, want rejected", got)
	}
}

func TestClassifyFailureForNetworkProblems(t *testing.T) {
	// Not public: the guard refused the address.
	srv, _ := fakeAnthropic(t, "end_turn")
	_, notPublic := llm.NewAnthropicAPI().Create(context.Background(), accessFor(srv, "k"), llm.CallRequest{
		MaxTokens: 10, Messages: []llm.Message{{Role: llm.RoleUser, Content: "x"}},
	})
	// Unreachable: nothing listens there.
	closed := httptest.NewServer(http.NotFoundHandler())
	access := accessFor(closed, "k")
	closed.Close()
	_, unreachable := llm.NewAnthropicAPI(llm.AllowPrivateAddresses(), llm.MaxRetries(0)).Create(context.Background(), access, llm.CallRequest{
		MaxTokens: 10, Messages: []llm.Message{{Role: llm.RoleUser, Content: "x"}},
	})

	if got := llm.ClassifyFailure(notPublic); got != llm.FailureNotPublic {
		t.Errorf("a guarded address = %q, want not_public", got)
	}
	if got := llm.ClassifyFailure(unreachable); got != llm.FailureUnreachable {
		t.Errorf("a closed port = %q (err %v), want unreachable", got, unreachable)
	}
	if got := llm.ClassifyFailure(context.DeadlineExceeded); got != llm.FailureUnreachable {
		t.Errorf("a timeout = %q, want unreachable", got)
	}
	if got := llm.ClassifyFailure(errors.New("something else")); got != llm.FailureOther {
		t.Errorf("an unknown error = %q, want other", got)
	}
	if got := llm.ClassifyFailure(nil); got != "" {
		t.Errorf("no error = %q, want empty", got)
	}
}
