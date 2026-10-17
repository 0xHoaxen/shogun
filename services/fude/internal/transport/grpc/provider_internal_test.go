package grpc

import (
	"testing"

	fudev1 "github.com/0xHoaxen/shogun/gen/go/shogun/fude/v1"
	"github.com/0xHoaxen/shogun/pkg/llm"
)

func TestFailureToProtoMapsEveryKind(t *testing.T) {
	for kind, want := range map[llm.FailureKind]fudev1.ProviderFailure{
		llm.FailureRejected:      fudev1.ProviderFailure_PROVIDER_FAILURE_REJECTED,
		llm.FailureRateLimited:   fudev1.ProviderFailure_PROVIDER_FAILURE_RATE_LIMITED,
		llm.FailureNotFound:      fudev1.ProviderFailure_PROVIDER_FAILURE_NOT_FOUND,
		llm.FailureUnreachable:   fudev1.ProviderFailure_PROVIDER_FAILURE_UNREACHABLE,
		llm.FailureNotPublic:     fudev1.ProviderFailure_PROVIDER_FAILURE_NOT_PUBLIC,
		llm.FailureProviderError: fudev1.ProviderFailure_PROVIDER_FAILURE_PROVIDER_ERROR,
		llm.FailureOther:         fudev1.ProviderFailure_PROVIDER_FAILURE_OTHER,
		"":                       fudev1.ProviderFailure_PROVIDER_FAILURE_UNSPECIFIED,
		"brand_new_kind":         fudev1.ProviderFailure_PROVIDER_FAILURE_OTHER,
	} {
		if got := failureToProto(kind); got != want {
			t.Errorf("%q maps to %v, want %v", kind, got, want)
		}
	}
}
