// Package dojo reads learning activities and items from the dojo service for
// fude.
package dojo

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	dojov1 "github.com/0xHoaxen/shogun/gen/go/shogun/dojo/v1"
	"github.com/0xHoaxen/shogun/services/fude/internal/app"
	"github.com/0xHoaxen/shogun/services/fude/internal/domain"
)

const (
	kindPrefix = "ITEM_KIND_"
	// maxInsightLen keeps a long takeaway from filling the prompt.
	maxInsightLen = 3000
)

// Source describes learning targets from dojo and hands every other target to
// the next source. The context must carry the owner's identity, which the
// client signs onto the call.
type Source struct {
	client dojov1.DojoServiceClient
	next   app.ContextSource
}

// New returns a Source that reads learning targets through client and asks
// next about the rest.
func New(client dojov1.DojoServiceClient, next app.ContextSource) *Source {
	return &Source{client: client, next: next}
}

// Describe implements app.ContextSource. A learning target is an activity's id,
// or the id of a finished item, which the item_completed event names.
func (s *Source) Describe(ctx context.Context, owner uuid.UUID, target domain.TargetType, id uuid.UUID) (app.TargetContext, error) {
	if target != domain.TargetLearningActivity {
		return s.next.Describe(ctx, owner, target, id)
	}
	res, err := s.client.GetActivity(ctx, &dojov1.GetActivityRequest{Id: id.String()})
	switch {
	case err == nil:
		return activity(res), nil
	case status.Code(err) == codes.NotFound:
		return s.item(ctx, id)
	default:
		return app.TargetContext{}, fmt.Errorf("get activity: %w", err)
	}
}

func (s *Source) item(ctx context.Context, id uuid.UUID) (app.TargetContext, error) {
	res, err := s.client.GetItem(ctx, &dojov1.GetItemRequest{Id: id.String()})
	if err != nil {
		return app.TargetContext{}, fmt.Errorf("get item: %w", err)
	}
	i := res.GetItem()
	return app.TargetContext{Summary: lines(
		"Finished: "+i.GetTitle(),
		"Kind: "+kindName(i.GetKind()),
		"Link: "+i.GetUrl(),
		"Started on: "+i.GetStartedOn(),
		"Completed on: "+i.GetCompletedOn(),
		"Takeaway: "+clip(i.GetInsight(), maxInsightLen),
	)}, nil
}

func activity(res *dojov1.GetActivityResponse) app.TargetContext {
	a, i := res.GetActivity(), res.GetItem()
	minutes := ""
	if a.GetMinutes() > 0 {
		minutes = fmt.Sprintf("%d", a.GetMinutes())
	}
	return app.TargetContext{Summary: lines(
		"Learned: "+a.GetSummary(),
		"Date: "+a.GetOccurredOn(),
		"Minutes spent: "+minutes,
		"Tags: "+strings.Join(a.GetTags(), ", "),
		"Part of: "+i.GetTitle(),
		"Kind: "+kindName(i.GetKind()),
		"Takeaway: "+clip(i.GetInsight(), maxInsightLen),
	)}
}

// kindName turns an item kind into a lower-case word, or "" for none.
func kindName(k dojov1.ItemKind) string {
	if k == dojov1.ItemKind_ITEM_KIND_UNSPECIFIED {
		return ""
	}
	return strings.ToLower(strings.TrimPrefix(k.String(), kindPrefix))
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
