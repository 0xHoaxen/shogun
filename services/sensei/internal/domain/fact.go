package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Event types sensei turns into facts.
const (
	TypeJobAdded           = "job.added"
	TypeJobStatusChanged   = "job.status_changed"
	TypeJobFollowUpDue     = "job.follow_up_due"
	TypeContactAdded       = "contact.added"
	TypeContactStatus      = "contact.status_changed"
	TypeContactFollowUpDue = "contact.follow_up_due"
	TypeMailClassified     = "mail.classified"
	TypeMailReplyDetected  = "mail.reply_detected"
	TypeDraftApproved      = "draft.approved"
	TypeDraftSent          = "draft.sent"
	TypeCostThreshold      = "cost.threshold_reached"
)

// Dimension keys facts carry.
const (
	DimJob            = "job_id"
	DimSource         = "source"
	DimFrom           = "from"
	DimTo             = "to"
	DimStatus         = "status"
	DimChannel        = "channel"
	DimClassification = "classification"
	DimLinked         = "linked"
	DimDraft          = "draft_id"
	DimScope          = "scope"
	DimPercent        = "percent"
	DimPeriod         = "period"

	// Unknown stands for a dimension the event did not say.
	Unknown = "unknown"
)

// Limits on what a fact keeps.
const (
	maxDimensions     = 8
	maxDimensionValue = 100
)

// ErrInvalidRange marks a stats query whose days make no sense. Transport maps
// it to InvalidArgument.
var ErrInvalidRange = errors.New("domain: invalid range")

func invalidRange(msg string) error { return fmt.Errorf("%w: %s", ErrInvalidRange, msg) }

// ErrInvalidFact marks a fact that can never be stored, however often it is
// retried.
var ErrInvalidFact = errors.New("domain: invalid fact")

// Fact is one thing that happened, projected from one event. It holds ids and
// short labels only: never text from a message, a name or an address.
type Fact struct {
	EventID    uuid.UUID
	OwnerID    uuid.UUID
	Type       string
	Dimension  map[string]string
	OccurredAt time.Time
}

// Validate returns the fact with its dimension values cleaned and cut, or
// ErrInvalidFact.
func (f Fact) Validate() (Fact, error) {
	if f.EventID == uuid.Nil || f.OwnerID == uuid.Nil || f.Type == "" || f.OccurredAt.IsZero() {
		return f, ErrInvalidFact
	}
	if len(f.Dimension) > maxDimensions {
		return f, ErrInvalidFact
	}
	clean := make(map[string]string, len(f.Dimension))
	for k, v := range f.Dimension {
		clean[k] = Label(v)
	}
	f.Dimension = clean
	f.OccurredAt = f.OccurredAt.UTC()
	return f, nil
}

// Label turns a value into a short, lower-case label for a dimension: empty
// becomes Unknown and long text is cut.
func Label(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" {
		return Unknown
	}
	if len(v) > maxDimensionValue {
		v = v[:maxDimensionValue]
	}
	return v
}

// EnumLabel turns a proto enum name such as JOB_STATUS_APPLIED into "applied",
// given its prefix. An unspecified value is Unknown.
func EnumLabel(name, prefix string) string {
	label := strings.ToLower(strings.TrimPrefix(name, prefix))
	if label == "unspecified" {
		return Unknown
	}
	return Label(label)
}
