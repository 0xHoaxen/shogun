package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/services/kagami/internal/domain"
	"github.com/0xHoaxen/shogun/services/kagami/internal/store"
)

// Why a previewed row matches a contact already in the book.
const (
	DuplicateByEmail       = "email"
	DuplicateByLinkedin    = "linkedin"
	DuplicateByNameCompany = "name_company"
)

// MaxPreviewCell is the most characters of a cell a preview row repeats. The
// whole sheet already travels in the same response when it was reshaped, and the
// response has to stay under gRPC's 4 MiB limit, so a preview row holds enough to
// recognise a person on screen and no more. The import itself uses the full cell.
const MaxPreviewCell = 60

// MaxPreviewRows is how many rows of a file a dry run previews. The report still
// counts every row. A reshaped sheet travels in the same response and can be 3 MiB,
// so the preview has to stay small enough to share gRPC's 4 MiB with it.
const MaxPreviewRows = 1000

// PreviewRow is what the import would read from one row, and what it would do.
type PreviewRow struct {
	// Row is the 1-based CSV row number, counting the header as row 1.
	Row          int32
	FullName     string
	Role         string
	Company      string
	Email        string
	Relationship string
	// RelationshipGuessed is true when the file had no relationship and
	// Relationship comes from the role.
	RelationshipGuessed bool
	// Duplicate is true when the person is already in the book; the import would
	// update that contact, not add one.
	Duplicate          bool
	DuplicateContactID string
	DuplicateReason    string
	// RepeatsRow is the earlier row of this file that is the same person, or 0.
	RepeatsRow int32
	// Problem says why the row cannot be imported, in words; empty when it can.
	Problem string
}

// previewImportRows reads every row the way the import does and says who each is
// and whether they are already known. It only reads: nothing is created, not
// even a company.
func (s *Service) previewImportRows(ctx context.Context, repo *store.Repo, owner uuid.UUID, rows []importRow) ([]PreviewRow, error) {
	out := make([]PreviewRow, 0, min(len(rows), MaxPreviewRows))
	seen := newSeenInFile()
	for _, row := range rows[:min(len(rows), MaxPreviewRows)] {
		p := PreviewRow{
			Row: row.number, FullName: clip(row.fields.FullName), Role: clip(row.fields.Role),
			Company: clip(row.company), Email: clip(row.fields.Email),
		}
		values, problem := previewValues(row)
		if problem != "" {
			p.Problem = clip(problem)
			out = append(out, p)
			continue
		}
		p.Relationship, p.RelationshipGuessed = previewRelationship(values.Relationship, deref(values.Role))
		if err := s.markDuplicate(ctx, repo, owner, row, values, &p); err != nil {
			return nil, fmt.Errorf("preview row %d: %w", row.number, err)
		}
		p.RepeatsRow = seen.repeat(row.number, values, row.company)
		out = append(out, p)
	}
	return out, nil
}

// clip cuts a cell to MaxPreviewCell characters, marking the cut with an ellipsis.
func clip(cell string) string {
	if utf8.RuneCountInString(cell) <= MaxPreviewCell {
		return cell
	}
	return string([]rune(cell)[:MaxPreviewCell-1]) + "…"
}

// previewValues validates a row as the import would. It returns the cleaned
// values, or the problem in words.
func previewValues(row importRow) (contactValues, string) {
	if row.problem != nil {
		return contactValues{}, row.problem.Msg
	}
	fields := row.fields
	// A job_link is looked up by the real import; the preview does not need the
	// job, so it leaves it out and only checks the rest.
	values, err := parseContactFields(fields)
	var ia *InvalidArgumentError
	if errors.As(err, &ia) {
		return contactValues{}, ia.Msg
	}
	if err != nil {
		return contactValues{}, "this row could not be read"
	}
	return values, ""
}

// previewRelationship keeps a relationship the file gave and otherwise guesses
// one from the role.
func previewRelationship(given *string, role string) (string, bool) {
	if given != nil && *given != "" {
		return *given, false
	}
	return domain.GuessRelationship(role)
}

// markDuplicate sets the duplicate fields of p when the row is someone already in
// the book: by email, then LinkedIn (as the real import matches), then by name
// and company. A name alone is never enough.
func (s *Service) markDuplicate(ctx context.Context, repo *store.Repo, owner uuid.UUID, row importRow, values contactValues, p *PreviewRow) error {
	existing, err := repo.FindDuplicate(ctx, owner, values.Email, values.LinkedinURL)
	reason := ""
	switch {
	case err == nil:
		reason = DuplicateByEmail
		if values.Email == nil || !strings.EqualFold(deref(existing.Email), *values.Email) {
			reason = DuplicateByLinkedin
		}
	case !errors.Is(err, store.ErrNotFound):
		return err
	}
	if reason == "" {
		name, company := domain.NormalizeName(values.FullName), strings.TrimSpace(row.company)
		if name == "" || company == "" {
			return nil
		}
		existing, err = repo.FindContactByNameAndCompany(ctx, owner, name, company)
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		reason = DuplicateByNameCompany
	}
	p.Duplicate, p.DuplicateContactID, p.DuplicateReason = true, existing.ID.String(), reason
	return nil
}

// seenInFile remembers which row of a file first held each person, so a later
// row for the same person can say so. People are matched by email, by LinkedIn
// URL, or by name and company, the same ways as against the book.
type seenInFile struct {
	first map[string]int32
}

func newSeenInFile() *seenInFile { return &seenInFile{first: map[string]int32{}} }

// keysOf are the ways a row can be recognised again. The kind is part of each key
// so an email can never be mistaken for a URL.
func keysOf(v contactValues, company string) []string {
	var keys []string
	if v.Email != nil && *v.Email != "" {
		keys = append(keys, "email\x00"+strings.ToLower(*v.Email))
	}
	if v.LinkedinURL != nil && *v.LinkedinURL != "" {
		keys = append(keys, "linkedin\x00"+*v.LinkedinURL)
	}
	if name, co := domain.NormalizeName(v.FullName), strings.ToLower(strings.TrimSpace(company)); name != "" && co != "" {
		keys = append(keys, "name\x00"+name+"\x00"+co)
	}
	return keys
}

// repeat returns the number of the earlier row that is the same person as this
// one, or 0, and remembers this row for the rows after it.
func (s *seenInFile) repeat(number int32, v contactValues, company string) int32 {
	keys := keysOf(v, company)
	var earlier int32
	for _, key := range keys {
		if first, ok := s.first[key]; ok && (earlier == 0 || first < earlier) {
			earlier = first
		}
	}
	for _, key := range keys {
		if _, ok := s.first[key]; !ok {
			s.first[key] = number
		}
	}
	return earlier
}
