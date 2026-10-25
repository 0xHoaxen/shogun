package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/0xHoaxen/shogun/services/kagami/internal/domain"
	"github.com/0xHoaxen/shogun/services/kagami/internal/store"
	"github.com/0xHoaxen/shogun/services/kagami/internal/store/db"
)

const defaultImportFilename = "contacts.csv"

// errDryRun rolls the import transaction back after the report is built.
var errDryRun = errors.New("app: dry run")

// ImportContactsInput is the input of ImportContacts.
type ImportContactsInput struct {
	Filename string
	CSV      []byte
	// DryRun builds the report and writes nothing.
	DryRun bool
	// Reshape maps a sheet with other columns onto the import columns first,
	// with the owner's AI provider.
	Reshape bool
	// Mappings is the user's own column mapping. When set it is used instead of
	// the AI's, with the same checks, and no model is called whatever Reshape says.
	Mappings []ColumnMapping
	// DateOrder is how the sheet writes dates of only numbers (dmy, mdy or ymd)
	// when Mappings is set; empty reads only dates that are unambiguous.
	DateOrder string
	// Selection limits the import to some rows. Nil imports every row. A duplicate
	// is imported only if it is selected.
	Selection *RowSelection
}

// ImportRowError is one row the import could not apply. Row is the 1-based CSV
// row number, counting the header as row 1.
type ImportRowError struct {
	Row     int32  `json:"row"`
	Column  string `json:"column"`
	Message string `json:"message"`
}

// ImportReport counts what an import did, or on a dry run would do.
type ImportReport struct {
	// RowsTotal counts the rows processed: all of them, or the selected ones.
	RowsTotal   int32
	RowsCreated int32
	RowsUpdated int32
	RowsFailed  int32
	// RowsSkipped counts the rows of the file a selection left out.
	RowsSkipped int32
	Errors      []ImportRowError
}

// ImportResult is the outcome of ImportContacts. ImportID is empty on a dry run.
type ImportResult struct {
	Report   ImportReport
	ImportID string
	// ShapedCSV is the sheet in the import columns when Reshape changed it,
	// and Mappings how each column was filled. Both are empty otherwise.
	ShapedCSV []byte
	Mappings  []ColumnMapping
	// DateOrder is the order the sheet's numeric dates were read in, so a screen
	// that edits the mapping can send it back.
	DateOrder string
	// Rows says, for each row, what the import read and whether the person is
	// already known. It is set on a dry run only, and holds the first
	// MaxPreviewRows rows of a longer file.
	Rows []PreviewRow
	// PreviewTruncated is true when the file had more rows than Rows holds.
	PreviewTruncated bool
}

// ImportContacts adds the contacts of a CSV and updates the ones already in
// the book, matched by email and then LinkedIn URL. A row that fails is
// skipped and reported; the other rows still apply. Everything happens in one
// transaction, so a failure of the database leaves nothing behind. On update,
// empty cells keep the stored value and the status is left alone. Each new
// contact writes contact.added.
func (s *Service) ImportContacts(ctx context.Context, in ImportContactsInput) (ImportResult, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return ImportResult{}, err
	}
	var result ImportResult
	data := in.CSV
	switch {
	case len(in.Mappings) > 0:
		shaped, shape, err := shapeWithMapping(in.CSV, in.Mappings, in.DateOrder)
		if err != nil {
			return ImportResult{}, err
		}
		data, result.ShapedCSV, result.Mappings, result.DateOrder = shaped, shaped, shape.mappings, shape.dateOrder
	case in.Reshape:
		shaped, shape, err := s.reshapeSheet(ctx, in.CSV)
		if err != nil {
			return ImportResult{}, err
		}
		if shaped != nil {
			data, result.ShapedCSV, result.Mappings, result.DateOrder = shaped, shaped, shape.mappings, shape.dateOrder
		}
	}
	allRows, err := parseImportCSV(data)
	if err != nil {
		return ImportResult{}, err
	}
	rows, skipped, err := selectRows(allRows, in.Selection)
	if err != nil {
		return ImportResult{}, err
	}
	filename := strings.TrimSpace(in.Filename)
	if filename == "" {
		filename = defaultImportFilename
	}

	if in.DryRun {
		// Read before anything is applied, so a row is judged against the book as it
		// is now and not as the dry run's own rows would leave it. Every row is
		// previewed, selected or not: the user needs to see what they left out.
		if result.Rows, err = s.previewImportRows(ctx, store.New(s.pool), owner, allRows); err != nil {
			return ImportResult{}, err
		}
		result.PreviewTruncated = len(allRows) > len(result.Rows)
	}

	err = s.inTx(ctx, func(tx pgx.Tx, repo *store.Repo) error {
		report, err := s.applyImportRows(ctx, tx, owner, rows)
		if err != nil {
			return err
		}
		report.RowsSkipped = skipped
		result.Report = report
		if in.DryRun {
			return errDryRun
		}
		recorded, err := recordImport(ctx, repo, owner, filename, report)
		result.ImportID = recorded
		return err
	})
	if errors.Is(err, errDryRun) {
		return result, nil
	}
	return result, err
}

func recordImport(ctx context.Context, repo *store.Repo, owner uuid.UUID, filename string, report ImportReport) (string, error) {
	failures, err := json.Marshal(report.Errors)
	if err != nil {
		return "", fmt.Errorf("marshal import errors: %w", err)
	}
	imp, err := repo.InsertImport(ctx, db.InsertImportParams{
		ID: store.NewID(), OwnerID: owner, Filename: filename, RowsTotal: report.RowsTotal,
		RowsCreated: report.RowsCreated, RowsUpdated: report.RowsUpdated, RowsFailed: report.RowsFailed,
		Errors: failures,
	})
	if err != nil {
		return "", err
	}
	return imp.ID.String(), nil
}

// applyImportRows applies every row on tx and totals the outcomes.
func (s *Service) applyImportRows(ctx context.Context, tx pgx.Tx, owner uuid.UUID, rows []importRow) (ImportReport, error) {
	report := ImportReport{Errors: []ImportRowError{}}
	for _, row := range rows {
		report.RowsTotal++
		created, err := s.importRow(ctx, tx, owner, row)
		if rowErr, ok := rowErrorFor(row.number, err); ok {
			report.RowsFailed++
			report.Errors = append(report.Errors, rowErr)
			continue
		}
		if err != nil {
			return report, fmt.Errorf("import row %d: %w", row.number, err)
		}
		if created {
			report.RowsCreated++
		} else {
			report.RowsUpdated++
		}
	}
	return report, nil
}

// rowErrorFor reports whether err is a problem with the row itself, which the
// import records and skips, rather than a failure of the database.
func rowErrorFor(number int32, err error) (ImportRowError, bool) {
	var ia *InvalidArgumentError
	switch {
	case err == nil:
		return ImportRowError{}, false
	case errors.As(err, &ia):
		return ImportRowError{Row: number, Column: ia.Field, Message: ia.Msg}, true
	case errors.Is(err, store.ErrDuplicate):
		return ImportRowError{Row: number, Message: "email or linkedin_url already belongs to another contact"}, true
	}
	return ImportRowError{}, false
}

// importRow applies one row inside a savepoint, so a row the database rejects
// does not abort the transaction. It reports whether the row created a contact.
func (s *Service) importRow(ctx context.Context, tx pgx.Tx, owner uuid.UUID, row importRow) (bool, error) {
	if row.problem != nil {
		return false, &InvalidArgumentError{Reason: row.problem.Reason, Field: row.problem.Field, Msg: row.problem.Msg}
	}
	sp, err := tx.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin row savepoint: %w", err)
	}
	created, err := s.upsertImportedContact(ctx, sp, store.New(sp), owner, row)
	if err != nil {
		if rbErr := sp.Rollback(ctx); rbErr != nil {
			return false, fmt.Errorf("roll back row savepoint: %w", rbErr)
		}
		return false, err
	}
	if err := sp.Commit(ctx); err != nil {
		return false, fmt.Errorf("release row savepoint: %w", err)
	}
	return created, nil
}

// upsertImportedContact creates the contact for row, or updates the one it
// matches.
func (s *Service) upsertImportedContact(ctx context.Context, tx pgx.Tx, repo *store.Repo, owner uuid.UUID, row importRow) (bool, error) {
	fields := row.fields
	if row.jobLink != "" {
		job, err := repo.GetJobByURL(ctx, owner, row.jobLink)
		if errors.Is(err, store.ErrNotFound) {
			return false, invalidField("job_link", "JOB_NOT_FOUND", "no job has this URL")
		}
		if err != nil {
			return false, err
		}
		fields.JobID = job.ID.String()
	}
	values, err := parseContactFields(fields)
	if err != nil {
		return false, err
	}

	existing, err := repo.FindDuplicate(ctx, owner, values.Email, values.LinkedinURL)
	if errors.Is(err, store.ErrNotFound) {
		status := row.status
		if status == "" {
			status = domain.ContactNotReached
		}
		// A new person with no relationship in the file gets the one the preview
		// showed, guessed from their title, so what is stored is what was checked.
		// Someone already in the book is never changed by a guess (the update below).
		if guess, guessed := previewRelationship(values.Relationship, deref(values.Role)); guessed {
			values.Relationship = &guess
		}
		_, err := s.createContact(ctx, tx, repo, owner, newContactRow{values: values, status: status, companyName: row.company})
		return true, err
	}
	if err != nil {
		return false, err
	}
	return false, s.updateImportedContact(ctx, repo, owner, existing, row, fields)
}

// updateImportedContact overwrites the stored contact with the non-empty cells
// of the row.
func (s *Service) updateImportedContact(ctx context.Context, repo *store.Repo, owner uuid.UUID, existing db.Contact, row importRow, fields ContactFields) error {
	merged := fillContactFields(toContactFields(existing), fields)
	if row.company != "" {
		company, err := repo.FindOrCreateCompanyByName(ctx, owner, row.company)
		if err != nil {
			return err
		}
		merged.CompanyID = company.ID.String()
	}
	v, err := parseContactFields(merged)
	if err != nil {
		return err
	}
	_, err = repo.UpdateContact(ctx, db.UpdateContactParams{
		ID: existing.ID, OwnerID: owner, Version: existing.Version, FullName: v.FullName, CompanyID: v.CompanyID,
		Role: v.Role, Email: v.Email, LinkedinUrl: v.LinkedinURL, XHandle: v.XHandle, Phone: v.Phone,
		Relationship: v.Relationship, HowWeMet: v.HowWeMet, PreferredChannel: v.PreferredChannel,
		LastContacted: v.LastContacted, NextFollowUp: v.NextFollowUp, TargetRole: v.TargetRole,
		JobID: v.JobID, Tags: v.Tags, Notes: v.Notes,
	})
	return err
}

// fillContactFields returns base with every non-empty field of in laid over it.
func fillContactFields(base, in ContactFields) ContactFields {
	pick := func(current, next string) string {
		if next != "" {
			return next
		}
		return current
	}
	base.FullName = pick(base.FullName, in.FullName)
	base.CompanyID = pick(base.CompanyID, in.CompanyID)
	base.Role = pick(base.Role, in.Role)
	base.Email = pick(base.Email, in.Email)
	base.LinkedinURL = pick(base.LinkedinURL, in.LinkedinURL)
	base.XHandle = pick(base.XHandle, in.XHandle)
	base.Phone = pick(base.Phone, in.Phone)
	base.Relationship = pick(base.Relationship, in.Relationship)
	base.HowWeMet = pick(base.HowWeMet, in.HowWeMet)
	base.PreferredChannel = pick(base.PreferredChannel, in.PreferredChannel)
	base.LastContacted = pick(base.LastContacted, in.LastContacted)
	base.NextFollowUp = pick(base.NextFollowUp, in.NextFollowUp)
	base.TargetRole = pick(base.TargetRole, in.TargetRole)
	base.JobID = pick(base.JobID, in.JobID)
	base.Notes = pick(base.Notes, in.Notes)
	if len(in.Tags) > 0 {
		base.Tags = in.Tags
	}
	return base
}
