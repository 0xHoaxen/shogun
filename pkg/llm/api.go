package llm

import (
	"context"

	"google.golang.org/grpc"

	sorobanv1 "github.com/0xHoaxen/shogun/gen/go/shogun/soroban/v1"
)

// Role is who wrote a message.
type Role string

// Message roles.
const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message is one turn of a conversation.
type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
}

// Usage is the token counts of one call. InputTokens is the part of the prompt
// that was neither read from nor written to the prompt cache.
type Usage struct {
	InputTokens      int32
	OutputTokens     int32
	CacheReadTokens  int32
	CacheWriteTokens int32
}

// CallRequest is what is sent to the model once a call is allowed.
type CallRequest struct {
	Model     string    `json:"model"`
	System    string    `json:"system"`
	Messages  []Message `json:"messages"`
	MaxTokens int32     `json:"max_tokens"`
}

// CallResult is what the model answered.
type CallResult struct {
	Text       string
	StopReason string
	Usage      Usage
	// Refused is set when the model declined the request. Its usage is real
	// and is still metered.
	Refused bool
}

// API is the model behind Client. AnthropicAPI is the real one; llmtest has a
// fake.
type API interface {
	// CountTokens returns the prompt's input tokens.
	CountTokens(ctx context.Context, req CallRequest) (int32, error)
	// Create runs the request. The system prompt is cached.
	Create(ctx context.Context, req CallRequest) (CallResult, error)
}

// Meter is the part of soroban's client Client needs. The generated
// sorobanv1.SorobanServiceClient satisfies it.
type Meter interface {
	Reserve(ctx context.Context, in *sorobanv1.ReserveRequest, opts ...grpc.CallOption) (*sorobanv1.ReserveResponse, error)
	Commit(ctx context.Context, in *sorobanv1.CommitRequest, opts ...grpc.CallOption) (*sorobanv1.CommitResponse, error)
	Release(ctx context.Context, in *sorobanv1.ReleaseRequest, opts ...grpc.CallOption) (*sorobanv1.ReleaseResponse, error)
}
