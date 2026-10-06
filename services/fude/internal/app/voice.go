package app

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/0xHoaxen/shogun/services/fude/internal/domain"
	"github.com/0xHoaxen/shogun/services/fude/internal/store"
	"github.com/0xHoaxen/shogun/services/fude/internal/store/db"
)

// AddVoiceSample stores a sample of the owner's own writing that drafts are
// shaped after. Its embedding is computed later by a background job.
func (s *Service) AddVoiceSample(ctx context.Context, channel domain.Channel, text string) (db.InsertVoiceSampleRow, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return db.InsertVoiceSampleRow{}, err
	}
	text = strings.TrimSpace(text)
	switch {
	case !channel.Valid():
		return db.InsertVoiceSampleRow{}, invalidField("channel", "INVALID_CHANNEL", "channel is missing or unknown")
	case text == "":
		return db.InsertVoiceSampleRow{}, invalidField("text", "TEXT_REQUIRED", "text is required")
	case len(text) > maxVoiceSampleLen:
		return db.InsertVoiceSampleRow{}, invalidField("text", "TEXT_TOO_LONG", "text is longer than %d bytes", maxVoiceSampleLen)
	}
	var sample db.InsertVoiceSampleRow
	err = s.inTx(ctx, func(tx pgx.Tx, repo *store.Repo) error {
		var insErr error
		sample, insErr = repo.InsertVoiceSample(ctx, db.InsertVoiceSampleParams{
			ID: store.NewID(), OwnerID: owner, Channel: string(channel), Text: text,
		})
		if insErr != nil {
			return insErr
		}
		return s.enqueueEmbed(ctx, tx, sample.ID)
	})
	return sample, err
}
