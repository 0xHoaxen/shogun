// Package kagami reads saved jobs from the kagami service for katana.
package kagami

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
)

// Source reads jobs from kagami. The context must carry the owner's identity,
// which the client signs onto the call.
type Source struct {
	client kagamiv1.KagamiServiceClient
}

// New returns a Source that calls kagami through client.
func New(client kagamiv1.KagamiServiceClient) *Source { return &Source{client: client} }

// JobPosting implements app.JobSource: the title and description of the job,
// empty when it has no description.
func (s *Source) JobPosting(ctx context.Context, job uuid.UUID) (string, error) {
	res, err := s.client.GetJob(ctx, &kagamiv1.GetJobRequest{Id: job.String()})
	if err != nil {
		return "", fmt.Errorf("get job: %w", err)
	}
	description := strings.TrimSpace(res.GetJob().GetDescription())
	if description == "" {
		return "", nil
	}
	return strings.TrimSpace(res.GetJob().GetTitle() + "\n" + description), nil
}
