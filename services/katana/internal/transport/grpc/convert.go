package grpc

import (
	"encoding/json"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	katanav1 "github.com/0xHoaxen/shogun/gen/go/shogun/katana/v1"
	"github.com/0xHoaxen/shogun/services/katana/internal/domain"
	"github.com/0xHoaxen/shogun/services/katana/internal/store/db"
	"github.com/0xHoaxen/shogun/services/katana/internal/wire"
)

func suggestionToProto(s db.Suggestion) *katanav1.Suggestion {
	var evidence []domain.Evidence
	// Stored by this service from validated links; an unreadable column shows
	// as no evidence rather than failing the whole list.
	_ = json.Unmarshal(s.Evidence, &evidence)
	out := &katanav1.Suggestion{
		Id: s.ID.String(), Target: wire.TargetToProto(domain.Target(s.Target)), Section: s.Section,
		Before: deref(s.Before), After: s.After, Reason: s.Reason,
		State: wire.StateToProto(domain.State(s.State)), CreatedAt: timestamppb.New(s.CreatedAt),
	}
	for _, e := range evidence {
		out.Evidence = append(out.Evidence, &katanav1.Evidence{Label: e.Label, Url: e.URL})
	}
	if s.DecidedAt != nil {
		out.DecidedAt = timestamppb.New(*s.DecidedAt)
	}
	return out
}

// parseID reads a UUID from a request field.
func parseID(field, raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, &badRequest{reason: reasonInvalidID, msg: field + " is not a valid id"}
	}
	return id, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
