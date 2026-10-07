package app

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	fudev1 "github.com/0xHoaxen/shogun/gen/go/shogun/fude/v1"
	"github.com/0xHoaxen/shogun/services/dojo/internal/store"
)

// maxPostActivities caps how many activities one post is written from.
const maxPostActivities = 10

// GeneratePost asks fude for a draft post about the chosen activities and
// returns the draft's id. The post is for LinkedIn or X, and goes through the
// approval queue like any draft.
func (s *Service) GeneratePost(ctx context.Context, activityIDs []uuid.UUID, channel fudev1.Channel) (string, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return "", err
	}
	ids := slices.Compact(sortedCopy(activityIDs))
	switch {
	case len(ids) == 0:
		return "", invalidInput("choose at least one activity")
	case len(ids) > maxPostActivities:
		return "", invalidInput(fmt.Sprintf("choose at most %d activities", maxPostActivities))
	case channel != fudev1.Channel_CHANNEL_LINKEDIN && channel != fudev1.Channel_CHANNEL_X:
		return "", invalidInput("a post is for linkedin or x")
	}
	if s.fude == nil {
		return "", ErrDraftsUnavailable
	}
	repo := store.New(s.pool)
	lines := make([]string, 0, len(ids))
	insights := map[uuid.UUID]bool{}
	for _, id := range ids {
		activity, err := repo.GetActivity(ctx, owner, id)
		if err != nil {
			return "", wrapOp("generate post", notFound(err, ErrActivityNotFound))
		}
		line := fmt.Sprintf("- %s: %s", activity.OccurredOn.Format(time.DateOnly), activity.Summary)
		if activity.ItemID != nil {
			item, err := repo.GetItem(ctx, owner, *activity.ItemID)
			if err != nil && !isNotFound(err) {
				return "", wrapOp("generate post", err)
			}
			if err == nil {
				line += fmt.Sprintf(" [%s]", item.Title)
				if in := deref(item.Insight); in != "" && !insights[item.ID] {
					insights[item.ID] = true
					line += fmt.Sprintf("\n  Takeaway from %q: %s", item.Title, in)
				}
			}
		}
		lines = append(lines, line)
	}
	res, err := s.fude.GenerateDraft(ctx, &fudev1.GenerateDraftRequest{
		Kind:         fudev1.DraftKind_DRAFT_KIND_POST,
		TargetType:   fudev1.TargetType_TARGET_TYPE_LEARNING_ACTIVITY,
		TargetId:     ids[0].String(),
		Channel:      channel,
		ExtraContext: "Write the post about this learning:\n" + strings.Join(lines, "\n"),
	})
	if err != nil {
		return "", fmt.Errorf("generate post: %w: %w", ErrDraftsUnavailable, err)
	}
	return res.GetDraft().GetId(), nil
}

// sortedCopy returns the ids sorted, so duplicates sit together and the
// target is the same for the same choice, whatever order it was made in.
func sortedCopy(ids []uuid.UUID) []uuid.UUID {
	out := slices.Clone(ids)
	slices.SortFunc(out, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	return out
}
