// Package kagami reads jobs and contacts from the kagami service for fude.
package kagami

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
	"github.com/0xHoaxen/shogun/services/fude/internal/app"
	"github.com/0xHoaxen/shogun/services/fude/internal/domain"
)

const (
	contactStatusPrefix = "CONTACT_STATUS_"
	// maxDescriptionLen keeps a long job posting from filling the prompt.
	maxDescriptionLen = 3000
)

// Source describes drafts' targets from kagami. The context must carry the
// owner's identity, which the client signs onto the call.
type Source struct {
	client kagamiv1.KagamiServiceClient
}

// New returns a Source that calls kagami through client.
func New(client kagamiv1.KagamiServiceClient) *Source { return &Source{client: client} }

// Describe implements app.ContextSource. Learning activities are not readable
// until dojo exists, so they describe as nothing.
func (s *Source) Describe(ctx context.Context, _ uuid.UUID, target domain.TargetType, id uuid.UUID) (app.TargetContext, error) {
	switch target {
	case domain.TargetJob:
		return s.job(ctx, id)
	case domain.TargetContact:
		return s.contact(ctx, id)
	default:
		return app.TargetContext{}, nil
	}
}

func (s *Source) job(ctx context.Context, id uuid.UUID) (app.TargetContext, error) {
	res, err := s.client.GetJob(ctx, &kagamiv1.GetJobRequest{Id: id.String()})
	if err != nil {
		return app.TargetContext{}, fmt.Errorf("get job: %w", err)
	}
	j := res.GetJob()
	return app.TargetContext{Summary: lines(
		"Role: "+j.GetTitle(),
		"Company: "+res.GetCompany().GetName(),
		"Location: "+j.GetLocation(),
		"Posting: "+j.GetUrl(),
		"Description: "+clip(j.GetDescription(), maxDescriptionLen),
	)}, nil
}

func (s *Source) contact(ctx context.Context, id uuid.UUID) (app.TargetContext, error) {
	res, err := s.client.GetContact(ctx, &kagamiv1.GetContactRequest{Id: id.String()})
	if err != nil {
		return app.TargetContext{}, fmt.Errorf("get contact: %w", err)
	}
	c := res.GetContact()
	status := strings.ToLower(strings.TrimPrefix(c.GetStatus().String(), contactStatusPrefix))
	return app.TargetContext{
		ContactStatus: status,
		Email:         c.GetEmail(),
		Summary: lines(
			"Name: "+c.GetFullName(),
			"Role: "+c.GetRole(),
			"Company: "+c.GetCompanyName(),
			"Relationship: "+c.GetRelationship(),
			"How we met: "+c.GetHowWeMet(),
			"Target role: "+c.GetTargetRole(),
			"Notes: "+c.GetNotes(),
		),
	}, nil
}

// lines joins the "Label: value" lines that have a value.
func lines(all ...string) string {
	kept := make([]string, 0, len(all))
	for _, l := range all {
		if _, value, _ := strings.Cut(l, ": "); strings.TrimSpace(value) != "" {
			kept = append(kept, l)
		}
	}
	return strings.Join(kept, "\n")
}

// clip shortens s to at most n bytes, marking the cut.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
