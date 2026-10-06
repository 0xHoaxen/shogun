package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestNewNotice(t *testing.T) {
	tests := []struct {
		name    string
		typ     Type
		title   string
		body    string
		link    string
		wantErr error
	}{
		{"valid with a link", TypeDraftReady, "Cover letter ready", "For Acme", "/drafts/1", nil},
		{"valid without body or link", TypeOffer, "Offer", "", "", nil},
		{"unknown type", Type("bogus"), "x", "", "", ErrInvalidType},
		{"empty type", Type(""), "x", "", "", ErrInvalidType},
		{"blank title", TypeOffer, "   ", "", "", ErrEmptyTitle},
		{"title too long", TypeOffer, strings.Repeat("a", MaxTitleLength+1), "", "", ErrTooLong},
		{"body too long", TypeOffer, "x", strings.Repeat("a", MaxBodyLength+1), "", ErrTooLong},
		{"absolute url", TypeOffer, "x", "", "https://evil.example/x", ErrInvalidLink},
		{"protocol-relative url", TypeOffer, "x", "", "//evil.example/x", ErrInvalidLink},
		{"relative path", TypeOffer, "x", "", "drafts/1", ErrInvalidLink},
		{"backslash", TypeOffer, "x", "", `/\evil.example`, ErrInvalidLink},
		{"newline", TypeOffer, "x", "", "/a\nb", ErrInvalidLink},
		{"link too long", TypeOffer, "x", "", "/" + strings.Repeat("a", MaxLinkLength), ErrInvalidLink},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewNotice(tt.typ, tt.title, tt.body, tt.link)

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("want %v, got %v", tt.wantErr, err)
			}
			if tt.wantErr == nil && (got.Type != tt.typ || got.Link != tt.link) {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

func TestNewNoticeTrimsTitleAndBody(t *testing.T) {
	got, err := NewNotice(TypeDraftReady, "  Ready \n", " body ", "")

	if err != nil || got.Title != "Ready" || got.Body != "body" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestEveryTypeIsValid(t *testing.T) {
	for _, typ := range []Type{
		TypeDraftReady, TypeDraftFailed, TypeDraftSendFailed, TypeInterviewInvite, TypeOffer,
		TypeRejection, TypeReplyDetected, TypeFollowUpDue, TypeBudgetThreshold,
		TypeBudgetExhausted, TypeDailyDigest,
	} {
		if !typ.Valid() {
			t.Errorf("%s should be valid", typ)
		}
	}
}
