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

// Access is the provider a call is made with: the caller's own address, model
// and key, handed back by soroban when it reserves the call. It is secret and
// lives only as long as the call.
type Access struct {
	BaseURL string
	Model   string
	APIKey  string
}

// CallRequest is what is sent to the model once a call is allowed.
type CallRequest struct {
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

// API is the model behind Client. OpenAIAPI is the real one; llmtest has a
// fake. It holds no credentials of its own: every call brings the Access of the
// user it is made for.
type API interface {
	// Create runs the request with the given provider. The system prompt is cached.
	Create(ctx context.Context, access Access, req CallRequest) (CallResult, error)
}

// Meter is the part of soroban's client Client needs. The generated
// sorobanv1.SorobanServiceClient satisfies it.
type Meter interface {
	Reserve(ctx context.Context, in *sorobanv1.ReserveRequest, opts ...grpc.CallOption) (*sorobanv1.ReserveResponse, error)
	Commit(ctx context.Context, in *sorobanv1.CommitRequest, opts ...grpc.CallOption) (*sorobanv1.CommitResponse, error)
	Release(ctx context.Context, in *sorobanv1.ReleaseRequest, opts ...grpc.CallOption) (*sorobanv1.ReleaseResponse, error)
}
