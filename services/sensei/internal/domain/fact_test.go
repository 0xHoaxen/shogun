package domain_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/services/sensei/internal/domain"
)

func valid() domain.Fact {
	return domain.Fact{EventID: uuid.New(), OwnerID: uuid.New(), Type: "job.added", OccurredAt: time.Date(2026, 10, 7, 14, 0, 0, 0, time.FixedZone("x", 3600)), Dimension: map[string]string{"source": " LinkedIn "}}
}

func TestFactValidateCleansDimensionsAndUsesUTC(t *testing.T) {
	got, err := valid().Validate()

	if err != nil || got.Dimension["source"] != "linkedin" || got.OccurredAt.Location() != time.UTC || got.OccurredAt.Hour() != 13 {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestFactValidateRefusesWhatCanNeverBeStored(t *testing.T) {
	tests := map[string]func(*domain.Fact){
		"no event id": func(f *domain.Fact) { f.EventID = uuid.Nil },
		"no owner":    func(f *domain.Fact) { f.OwnerID = uuid.Nil },
		"no type":     func(f *domain.Fact) { f.Type = "" },
		"no time":     func(f *domain.Fact) { f.OccurredAt = time.Time{} },
		"too many dims": func(f *domain.Fact) {
			f.Dimension = map[string]string{}
			for _, k := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"} {
				f.Dimension[k] = "x"
			}
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			f := valid()
			mutate(&f)

			if _, err := f.Validate(); !errors.Is(err, domain.ErrInvalidFact) {
				t.Fatalf("err = %v, want ErrInvalidFact", err)
			}
		})
	}
}

func TestLabelAndEnumLabel(t *testing.T) {
	if got := domain.Label("  "); got != domain.Unknown {
		t.Errorf("blank label = %q", got)
	}
	if got := domain.Label(strings.Repeat("A", 300)); len(got) != 100 || got != strings.Repeat("a", 100) {
		t.Errorf("long label has %d bytes", len(got))
	}
	cases := map[string]string{"JOB_STATUS_APPLIED": "applied", "JOB_STATUS_UNSPECIFIED": "unknown", "CONTACT_STATUS_REACHED_OUT": "reached_out"}
	for name, want := range cases {
		prefix := "JOB_STATUS_"
		if strings.HasPrefix(name, "CONTACT") {
			prefix = "CONTACT_STATUS_"
		}
		if got := domain.EnumLabel(name, prefix); got != want {
			t.Errorf("EnumLabel(%s) = %q, want %q", name, got, want)
		}
	}
}
