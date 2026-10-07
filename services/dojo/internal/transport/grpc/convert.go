package grpc

import (
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	dojov1 "github.com/0xHoaxen/shogun/gen/go/shogun/dojo/v1"
	"github.com/0xHoaxen/shogun/services/dojo/internal/domain"
	"github.com/0xHoaxen/shogun/services/dojo/internal/store/db"
	"github.com/0xHoaxen/shogun/services/dojo/internal/wire"
)

func itemToProto(i db.Item) *dojov1.Item {
	return &dojov1.Item{
		Id: i.ID.String(), Title: i.Title, Kind: wire.KindToProto(domain.ItemKind(i.Kind)), Url: deref(i.Url),
		Status: wire.StatusToProto(domain.ItemStatus(i.Status)), StartedOn: dateString(i.StartedOn),
		CompletedOn: dateString(i.CompletedOn), Insight: deref(i.Insight), Version: i.Version,
		CreatedAt: timestamppb.New(i.CreatedAt), UpdatedAt: timestamppb.New(i.UpdatedAt),
	}
}

func itemsToProto(rows []db.Item) []*dojov1.Item {
	out := make([]*dojov1.Item, 0, len(rows))
	for _, r := range rows {
		out = append(out, itemToProto(r))
	}
	return out
}

func activityToProto(a db.Activity) *dojov1.Activity {
	var minutes int32
	if a.Minutes != nil {
		minutes = *a.Minutes
	}
	itemID := ""
	if a.ItemID != nil {
		itemID = a.ItemID.String()
	}
	return &dojov1.Activity{
		Id: a.ID.String(), ItemId: itemID, Summary: a.Summary, Minutes: minutes,
		OccurredOn: a.OccurredOn.Format(time.DateOnly), Tags: a.Tags, CreatedAt: timestamppb.New(a.CreatedAt),
	}
}

func activitiesToProto(rows []db.Activity) []*dojov1.Activity {
	out := make([]*dojov1.Activity, 0, len(rows))
	for _, r := range rows {
		out = append(out, activityToProto(r))
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

// parseOptionalID reads a UUID that may be left empty.
func parseOptionalID(field, raw string) (*uuid.UUID, error) {
	if raw == "" {
		return nil, nil
	}
	id, err := parseID(field, raw)
	if err != nil {
		return nil, err
	}
	return &id, nil
}

// parseDate reads a YYYY-MM-DD date that may be left empty (the zero time).
func parseDate(field, raw string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, nil
	}
	d, err := time.Parse(time.DateOnly, raw)
	if err != nil {
		return time.Time{}, &badRequest{reason: reasonInvalidDate, msg: field + " must be YYYY-MM-DD"}
	}
	return d, nil
}

func dateString(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.DateOnly)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
