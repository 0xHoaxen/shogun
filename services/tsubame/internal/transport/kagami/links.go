// Package kagami reads from the kagami service for tsubame.
package kagami

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/app"
)

// Linker finds the contact and job a message is about. The context must carry
// the owner's identity, which the client signs onto the call.
type Linker struct {
	client kagamiv1.KagamiServiceClient
}

// New returns a Linker that calls kagami through client.
func New(client kagamiv1.KagamiServiceClient) *Linker { return &Linker{client: client} }

// FindLinks implements app.Linker.
func (l *Linker) FindLinks(ctx context.Context, from string, urls []string) (app.Links, error) {
	res, err := l.client.FindMailLinks(ctx, &kagamiv1.FindMailLinksRequest{FromEmail: from, Urls: urls})
	if err != nil {
		return app.Links{}, fmt.Errorf("find mail links: %w", err)
	}
	contact, err := optionalID(res.GetContactId())
	if err != nil {
		return app.Links{}, fmt.Errorf("contact id: %w", err)
	}
	job, err := optionalID(res.GetJobId())
	if err != nil {
		return app.Links{}, fmt.Errorf("job id: %w", err)
	}
	return app.Links{ContactID: contact, JobID: job}, nil
}

func optionalID(s string) (*uuid.UUID, error) {
	if s == "" {
		return nil, nil
	}
	id, err := uuid.Parse(s)
	if err != nil {
		return nil, err
	}
	return &id, nil
}
