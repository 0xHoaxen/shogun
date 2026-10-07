package grpc

import (
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	shinobiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/shinobi/v1"
	"github.com/0xHoaxen/shogun/services/shinobi/internal/domain"
	"github.com/0xHoaxen/shogun/services/shinobi/internal/store"
	"github.com/0xHoaxen/shogun/services/shinobi/internal/store/db"
)

var kindToProto = map[domain.SourceKind]shinobiv1.SourceKind{
	domain.KindRSS:  shinobiv1.SourceKind_SOURCE_KIND_RSS,
	domain.KindAPI:  shinobiv1.SourceKind_SOURCE_KIND_API,
	domain.KindFile: shinobiv1.SourceKind_SOURCE_KIND_FILE,
}

func kindFromProto(k shinobiv1.SourceKind) domain.SourceKind {
	for d, p := range kindToProto {
		if p == k {
			return d
		}
	}
	return ""
}

func mappingToProto(m *domain.FieldMapping) *shinobiv1.FieldMapping {
	if m == nil {
		return nil
	}
	return &shinobiv1.FieldMapping{
		ItemsPath: m.ItemsPath, Id: m.ID, Title: m.Title, Company: m.Company, Url: m.URL,
		Location: m.Location, PostedAt: m.PostedAt, Description: m.Description,
	}
}

func mappingFromProto(m *shinobiv1.FieldMapping) *domain.FieldMapping {
	if m == nil {
		return nil
	}
	return &domain.FieldMapping{
		ItemsPath: m.GetItemsPath(), ID: m.GetId(), Title: m.GetTitle(), Company: m.GetCompany(), URL: m.GetUrl(),
		Location: m.GetLocation(), PostedAt: m.GetPostedAt(), Description: m.GetDescription(),
	}
}

func sourceToProto(s db.Source) *shinobiv1.Source {
	// Stored by this service from validated input; an unreadable column shows as
	// an empty config rather than failing the whole list.
	cfg, _ := store.ConfigOf(s)
	out := &shinobiv1.Source{
		Id: s.ID.String(), Name: s.Name, Kind: kindToProto[domain.SourceKind(s.Kind)], Schedule: s.Schedule,
		Enabled: s.Enabled, LastError: deref(s.LastError),
		Config: &shinobiv1.SourceConfig{Url: cfg.URL, Document: cfg.Document, Mapping: mappingToProto(cfg.Mapping)},
	}
	if s.LastRunAt != nil {
		out.LastRunAt = timestamppb.New(*s.LastRunAt)
	}
	return out
}

func sourceInputFromProto(s *shinobiv1.Source) domain.SourceInput {
	return domain.SourceInput{
		Name: s.GetName(), Kind: kindFromProto(s.GetKind()), Schedule: s.GetSchedule(), Enabled: s.GetEnabled(),
		Config: domain.SourceConfig{
			URL: s.GetConfig().GetUrl(), Document: s.GetConfig().GetDocument(), Mapping: mappingFromProto(s.GetConfig().GetMapping()),
		},
	}
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
