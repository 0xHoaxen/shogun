package grpc

import (
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	taikov1 "github.com/0xHoaxen/shogun/gen/go/shogun/taiko/v1"
	"github.com/0xHoaxen/shogun/services/taiko/internal/app"
	"github.com/0xHoaxen/shogun/services/taiko/internal/domain"
	"github.com/0xHoaxen/shogun/services/taiko/internal/store/db"
)

var typeToProto = map[domain.Type]taikov1.NotificationType{
	domain.TypeDraftReady:        taikov1.NotificationType_NOTIFICATION_TYPE_DRAFT_READY,
	domain.TypeDraftFailed:       taikov1.NotificationType_NOTIFICATION_TYPE_DRAFT_FAILED,
	domain.TypeDraftSendFailed:   taikov1.NotificationType_NOTIFICATION_TYPE_DRAFT_SEND_FAILED,
	domain.TypeInterviewInvite:   taikov1.NotificationType_NOTIFICATION_TYPE_INTERVIEW_INVITE,
	domain.TypeOffer:             taikov1.NotificationType_NOTIFICATION_TYPE_OFFER,
	domain.TypeRejection:         taikov1.NotificationType_NOTIFICATION_TYPE_REJECTION,
	domain.TypeReplyDetected:     taikov1.NotificationType_NOTIFICATION_TYPE_REPLY_DETECTED,
	domain.TypeFollowUpDue:       taikov1.NotificationType_NOTIFICATION_TYPE_FOLLOW_UP_DUE,
	domain.TypeBudgetThreshold:   taikov1.NotificationType_NOTIFICATION_TYPE_BUDGET_THRESHOLD,
	domain.TypeBudgetExhausted:   taikov1.NotificationType_NOTIFICATION_TYPE_BUDGET_EXHAUSTED,
	domain.TypeDailyDigest:       taikov1.NotificationType_NOTIFICATION_TYPE_DAILY_DIGEST,
	domain.TypeProfileSuggestion: taikov1.NotificationType_NOTIFICATION_TYPE_PROFILE_SUGGESTION,
	domain.TypeDiscoveryMatch:    taikov1.NotificationType_NOTIFICATION_TYPE_DISCOVERY_MATCH,
}

func notificationToProto(n db.Notification) *taikov1.Notification {
	out := &taikov1.Notification{
		Id:        n.ID.String(),
		Type:      typeToProto[domain.Type(n.Type)],
		Title:     n.Title,
		Body:      deref(n.Body),
		Link:      deref(n.Link),
		CreatedAt: timestamppb.New(n.CreatedAt),
	}
	if n.ReadAt != nil {
		out.ReadAt = timestamppb.New(*n.ReadAt)
	}
	return out
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func settingsToProto(v app.SettingsView) *taikov1.ChannelSettings {
	out := &taikov1.ChannelSettings{InAppEnabled: v.InAppEnabled, Version: v.Version}
	if v.Quiet != nil {
		out.Quiet = &taikov1.QuietHours{
			FromMinute: int32(v.Quiet.From / time.Minute),
			ToMinute:   int32(v.Quiet.To / time.Minute),
		}
	}
	return out
}

func settingsFromProto(in *taikov1.ChannelSettings) domain.ChannelSettings {
	out := domain.ChannelSettings{InAppEnabled: in.GetInAppEnabled()}
	if q := in.GetQuiet(); q != nil {
		out.Quiet = &domain.QuietHours{
			From: time.Duration(q.GetFromMinute()) * time.Minute,
			To:   time.Duration(q.GetToMinute()) * time.Minute,
		}
	}
	return out
}
