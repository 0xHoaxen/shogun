package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/0xHoaxen/shogun/pkg/llm"
	"github.com/0xHoaxen/shogun/services/kagami/internal/domain"
)

const (
	featureImportReshape = "kagami.import_reshape"

	// reshapeSampleRows and reshapeCellRunes bound what of a sheet the model
	// sees: its header and a few cut-down rows, never the whole file.
	reshapeSampleRows = 5
	reshapeCellRunes  = 80

	reasonReshapeFailed = "IMPORT_RESHAPE_FAILED"
	reasonNoNameColumn  = "IMPORT_NO_NAME_COLUMN"
)

// reshapeSystem is the instruction for the model. It maps columns only; kagami
// moves the values itself, so the model cannot invent or change a contact.
const reshapeSystem = `You map the columns of a contacts spreadsheet onto a fixed set of import columns.
You are given the sheet's header and a few sample rows as JSON.

Import columns:
- full_name (required): the person's name. Map first and last name columns together, in that order.
- company, role (job title), email, linkedin_url, x_handle, phone
- relationship (how the owner knows them, e.g. recruiter, engineer, friend), how_we_met
- status: one of not_reached, reached_out, conversation_started, replied, referral_asked
- preferred_channel, last_contacted (date), next_follow_up (date), target_role, job_link (a job posting URL)
- tags, notes

Rules:
- "sources" must be header names copied exactly. Use a header in at most one mapping.
- Leave out import columns the sheet has nothing for. Never guess a column from its values alone when its header says otherwise.
- date_order says how the sheet writes dates made only of numbers: "dmy", "mdy" or "ymd". Use "" if it has none.
- status_values maps each status the samples show, as written, to one of the five statuses. Leave out values with no clear match.

Answer with JSON only, no other text:
{"columns":[{"target":"full_name","sources":["First Name","Last Name"]}],"date_order":"","status_values":{}}
If no column holds a person's name, answer {"columns":[]}.`

// Completer is the part of *llm.Client that reshaping needs.
type Completer interface {
	Complete(ctx context.Context, feature string, req llm.Request) (llm.Response, error)
}

// ErrReshapeOff means kagami has no model to reshape with.
var ErrReshapeOff = errors.New("app: AI import is not set up")

// ReshapeError means the sheet could not be mapped onto the import columns:
// the provider failed, or its answer was not a usable mapping.
type ReshapeError struct {
	Reason string
	Msg    string
}

func (e *ReshapeError) Error() string { return fmt.Sprintf("%s: %s", e.Reason, e.Msg) }

func reshapeFailed(format string, args ...any) error {
	return &ReshapeError{Reason: reasonReshapeFailed, Msg: fmt.Sprintf(format, args...)}
}

// Option sets an optional part of a Service.
type Option func(*Service)

// WithReshaper sets the model that maps sheets with other columns onto the
// import columns. Without it a reshape import fails with ErrReshapeOff.
func WithReshaper(completer Completer) Option {
	return func(s *Service) { s.reshaper = completer }
}

// reshapeImportCSV returns data written in the import columns, and how each was
// filled. A sheet that already has them comes back nil, and the model is not
// called.
func (s *Service) reshapeImportCSV(ctx context.Context, data []byte) ([]byte, []ColumnMapping, error) {
	shaped, shape, err := s.reshapeSheet(ctx, data)
	return shaped, shape.mappings, err
}

// reshapeSheet is reshapeImportCSV with the whole shape the model found, so the
// date order can be handed back to the caller.
func (s *Service) reshapeSheet(ctx context.Context, data []byte) ([]byte, sheetShape, error) {
	if len(data) > maxImportBytes {
		return nil, sheetShape{}, invalid("IMPORT_TOO_LARGE", "file is larger than %d bytes", maxImportBytes)
	}
	sh, err := readSheet(data)
	if err != nil {
		return nil, sheetShape{}, err
	}
	if sh.hasImportHeader() {
		return nil, sheetShape{}, nil
	}
	if s.reshaper == nil {
		return nil, sheetShape{}, ErrReshapeOff
	}
	prompt, err := reshapePrompt(sh)
	if err != nil {
		return nil, sheetShape{}, err
	}
	res, err := s.reshaper.Complete(ctx, featureImportReshape, llm.Request{
		System:   reshapeSystem,
		Messages: []llm.Message{{Role: llm.RoleUser, Content: prompt}},
		Cache:    true,
	})
	if err != nil {
		return nil, sheetShape{}, modelFailure(err)
	}
	shape, err := parseReshapeAnswer(res.Text, sh.header)
	if err != nil {
		return nil, sheetShape{}, err
	}
	shaped, err := shapeSheet(sh, shape)
	if err != nil {
		return nil, sheetShape{}, err
	}
	return shaped, shape, nil
}

// Reason of the InvalidArgumentError for a column mapping the user sent.
const reasonInvalidMapping = "INVALID_MAPPING"

// shapeWithMapping writes data in the import columns using the mapping and date
// order the user chose, with no model involved. The mapping is checked as hard
// as one from the model.
func shapeWithMapping(data []byte, mappings []ColumnMapping, dateOrder string) ([]byte, sheetShape, error) {
	if len(data) > maxImportBytes {
		return nil, sheetShape{}, invalid("IMPORT_TOO_LARGE", "file is larger than %d bytes", maxImportBytes)
	}
	sh, err := readSheet(data)
	if err != nil {
		return nil, sheetShape{}, err
	}
	order := strings.ToLower(strings.TrimSpace(dateOrder))
	if _, known := numericDateLayouts[order]; order != "" && !known {
		return nil, sheetShape{}, invalid(reasonInvalidMapping, "date_order must be dmy, mdy or ymd, or empty")
	}
	answer := reshapeAnswer{}
	for _, m := range mappings {
		answer.Columns = append(answer.Columns, struct {
			Target  string   `json:"target"`
			Sources []string `json:"sources"`
		}{m.Target, m.Sources})
	}
	checked, err := checkMappings(answer, sh.header, func(format string, args ...any) error {
		return invalid(reasonInvalidMapping, format, args...)
	})
	if err != nil {
		return nil, sheetShape{}, err
	}
	shape := sheetShape{mappings: checked, dateOrder: order, statuses: map[string]domain.ContactStatus{}}
	shaped, err := shapeSheet(sh, shape)
	if err != nil {
		return nil, sheetShape{}, err
	}
	return shaped, shape, nil
}

// modelFailure keeps the errors the caller acts on (no provider, no budget, no
// metering) and turns the provider's own failures into a ReshapeError naming
// only their kind, since the provider's text can echo what it was sent.
func modelFailure(err error) error {
	for _, keep := range []error{llm.ErrProviderNotConfigured, llm.ErrBudgetExhausted, llm.ErrMeteringUnavailable} {
		if errors.Is(err, keep) {
			return fmt.Errorf("ask the model: %w", err)
		}
	}
	if errors.Is(err, llm.ErrRefused) {
		return reshapeFailed("the AI provider declined to read the sheet")
	}
	return reshapeFailed("the AI provider failed (%s)", llm.ClassifyFailure(err))
}

func reshapePrompt(sh sheet) (string, error) {
	samples := make([][]string, 0, reshapeSampleRows)
	for _, record := range sh.records {
		if len(samples) == reshapeSampleRows {
			break
		}
		if filledCells(record) == 0 {
			continue
		}
		row := make([]string, len(record))
		for i, cell := range record {
			row[i] = cutRunes(strings.TrimSpace(cell), reshapeCellRunes)
		}
		samples = append(samples, row)
	}
	body, err := json.Marshal(struct {
		Header []string   `json:"header"`
		Rows   [][]string `json:"rows"`
	}{sh.header, samples})
	if err != nil {
		return "", fmt.Errorf("marshal sheet sample: %w", err)
	}
	return string(body), nil
}

func cutRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

type reshapeAnswer struct {
	Columns []struct {
		Target  string   `json:"target"`
		Sources []string `json:"sources"`
	} `json:"columns"`
	DateOrder    string            `json:"date_order"`
	StatusValues map[string]string `json:"status_values"`
}

// parseReshapeAnswer reads the model's answer and checks it against the sheet:
// every target an import column, every source a column of the sheet, used
// once, and full_name filled.
func parseReshapeAnswer(text string, header []string) (sheetShape, error) {
	var answer reshapeAnswer
	if err := json.Unmarshal([]byte(answerJSON(text)), &answer); err != nil {
		return sheetShape{}, reshapeFailed("the AI provider's answer was not a column mapping")
	}
	mappings, err := checkMappings(answer, header, func(format string, args ...any) error {
		return reshapeFailed("the AI provider's mapping is not usable: "+format, args...)
	})
	if err != nil {
		return sheetShape{}, err
	}
	order := strings.ToLower(strings.TrimSpace(answer.DateOrder))
	if _, ok := numericDateLayouts[order]; !ok {
		order = ""
	}
	statuses := map[string]domain.ContactStatus{}
	for from, to := range answer.StatusValues {
		if status := domain.ContactStatus(strings.ToLower(strings.TrimSpace(to))); status.Valid() {
			statuses[strings.ToLower(strings.TrimSpace(from))] = status
		}
	}
	return sheetShape{mappings: mappings, dateOrder: order, statuses: statuses}, nil
}

// checkMappings validates a mapping against the sheet's header. bad builds the
// error for a mapping that is not usable, so the same rules serve a model's
// answer and a mapping the user sent, each in its own words.
func checkMappings(answer reshapeAnswer, header []string, bad func(format string, args ...any) error) ([]ColumnMapping, error) {
	targets := map[string]bool{}
	sources := map[string]bool{}
	mappings := make([]ColumnMapping, 0, len(answer.Columns))
	for _, column := range answer.Columns {
		target := strings.ToLower(strings.TrimSpace(column.Target))
		if !slices.Contains(importColumns, target) || targets[target] {
			return nil, bad("%q is not an import column, or is mapped twice", column.Target)
		}
		targets[target] = true
		m := ColumnMapping{Target: target}
		for _, source := range column.Sources {
			if !slices.Contains(header, source) || sources[source] {
				return nil, bad("%q is not a column of the sheet, or is used twice", source)
			}
			sources[source] = true
			m.Sources = append(m.Sources, source)
		}
		if len(m.Sources) > 0 {
			mappings = append(mappings, m)
		}
	}
	if !slices.ContainsFunc(mappings, func(m ColumnMapping) bool { return m.Target == "full_name" }) {
		return nil, &ReshapeError{Reason: reasonNoNameColumn, Msg: "no column of the sheet holds a name"}
	}
	return mappings, nil
}

// answerJSON is the JSON object in text, which some models wrap in a code fence
// or a sentence despite being told not to.
func answerJSON(text string) string {
	start, end := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if start < 0 || end < start {
		return text
	}
	return text[start : end+1]
}
