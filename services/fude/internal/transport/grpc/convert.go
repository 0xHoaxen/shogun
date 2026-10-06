package grpc

import (
	"google.golang.org/protobuf/types/known/timestamppb"

	fudev1 "github.com/0xHoaxen/shogun/gen/go/shogun/fude/v1"
	"github.com/0xHoaxen/shogun/services/fude/internal/domain"
	"github.com/0xHoaxen/shogun/services/fude/internal/store/db"
	"github.com/0xHoaxen/shogun/services/fude/internal/wire"
)

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func draftToProto(d db.Draft) *fudev1.Draft {
	targetID := ""
	if d.TargetID != nil {
		targetID = d.TargetID.String()
	}
	return &fudev1.Draft{
		Id:             d.ID.String(),
		Kind:           wire.KindToProto(domain.Kind(d.Kind)),
		TargetType:     wire.TargetTypeToProto(domain.TargetType(d.TargetType)),
		TargetId:       targetID,
		Channel:        wire.ChannelToProto(domain.Channel(d.Channel)),
		State:          wire.StateToProto(domain.DraftState(d.State)),
		CurrentVersion: d.CurrentVersion,
		Recipient:      deref(d.Recipient),
		FailureReason:  deref(d.FailureReason),
		Version:        d.Version,
		CreatedAt:      timestamppb.New(d.CreatedAt),
		UpdatedAt:      timestamppb.New(d.UpdatedAt),
	}
}

func versionToProto(v db.DraftVersion) *fudev1.DraftVersion {
	return &fudev1.DraftVersion{
		DraftId:      v.DraftID.String(),
		Version:      v.Version,
		Subject:      deref(v.Subject),
		Body:         v.Body,
		BodySha256:   v.BodySha256,
		ExtraContext: deref(v.ExtraContext),
		Model:        deref(v.Model),
		CreatedBy:    wire.AuthorToProto(v.CreatedBy),
		CreatedAt:    timestamppb.New(v.CreatedAt),
	}
}
