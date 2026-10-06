package app_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/0xHoaxen/shogun/pkg/hanko"
	"github.com/0xHoaxen/shogun/pkg/llm"
	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/pkg/postgres/postgrestest"
	"github.com/0xHoaxen/shogun/services/fude/internal/app"
	"github.com/0xHoaxen/shogun/services/fude/internal/domain"
	"github.com/0xHoaxen/shogun/services/fude/internal/store"
	"github.com/0xHoaxen/shogun/services/fude/internal/store/db"
	"github.com/0xHoaxen/shogun/services/fude/migrations"
)

func TestMain(m *testing.M) { os.Exit(postgrestest.Run(m)) }

type fakeLLM struct {
	text    string
	err     error
	calls   int
	feature string
	req     llm.Request
}

func (f *fakeLLM) Complete(_ context.Context, feature string, req llm.Request) (llm.Response, error) {
	f.calls++
	f.feature, f.req = feature, req
	return llm.Response{Text: f.text, Model: "claude-test"}, f.err
}

type fakeSource struct {
	target app.TargetContext
	err    error
}

func (f fakeSource) Describe(context.Context, uuid.UUID, domain.TargetType, uuid.UUID) (app.TargetContext, error) {
	return f.target, f.err
}

type env struct {
	pool  *pgxpool.Pool
	gen   *app.Generator
	llm   *fakeLLM
	owner uuid.UUID
	now   time.Time
}

func newEnv(t *testing.T, source fakeSource) *env {
	t.Helper()
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgrestest.NewDatabase(t), "fude")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	model := &fakeLLM{text: "Subject: Hello Lumen\n\nDear Lumen team,\nI'd like to help."}
	return &env{
		pool: pool, llm: model, owner: store.NewID(), now: now,
		gen: app.NewGenerator(pool, model, source, slog.New(slog.DiscardHandler), func() time.Time { return now }),
	}
}

func (e *env) newDraft(t *testing.T, kind, channel string) db.Draft {
	t.Helper()
	target := store.NewID()
	d, err := store.New(e.pool).InsertDraft(context.Background(), db.InsertDraftParams{
		ID: store.NewID(), OwnerID: e.owner, Kind: kind, TargetType: "job", TargetID: &target, Channel: channel,
	})
	if err != nil {
		t.Fatalf("insert draft: %v", err)
	}
	return d
}

func (e *env) args(d db.Draft, version int32) app.GenerateArgs {
	return app.GenerateArgs{OwnerID: e.owner, DraftID: d.ID, Version: version, ExtraContext: "mention Go"}
}

func (e *env) draft(t *testing.T, d db.Draft) db.Draft {
	t.Helper()
	got, err := store.New(e.pool).GetDraft(context.Background(), e.owner, d.ID)
	if err != nil {
		t.Fatalf("get draft: %v", err)
	}
	return got
}

func (e *env) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestGenerateWritesFirstVersionAndReadyEvent(t *testing.T) {
	e := newEnv(t, fakeSource{target: app.TargetContext{Summary: "Backend role at Lumen"}})
	d := e.newDraft(t, "cover_letter", "email")
	ctx := context.Background()
	if _, err := store.New(e.pool).InsertVoiceSample(ctx, db.InsertVoiceSampleParams{ID: store.NewID(), OwnerID: e.owner, Channel: "email", Text: "my own style"}); err != nil {
		t.Fatal(err)
	}

	err := e.gen.Generate(ctx, e.args(d, 1))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	got := e.draft(t, d)
	if got.State != "pending" || got.CurrentVersion != 1 || got.FailureReason != nil {
		t.Fatalf("got draft %+v", got)
	}
	versions, _ := store.New(e.pool).ListDraftVersions(ctx, d.ID)
	if len(versions) != 1 {
		t.Fatalf("got %d versions", len(versions))
	}
	v := versions[0]
	if *v.Subject != "Hello Lumen" || !strings.HasPrefix(v.Body, "Dear Lumen team") || v.CreatedBy != "ai" ||
		*v.Model != "claude-test" || *v.ExtraContext != "mention Go" ||
		string(v.BodySha256) != string(hanko.BodyDigest("Hello Lumen", v.Body)) {
		t.Fatalf("got version %+v", v)
	}
	if e.llm.feature != "fude.cover_letter" || !strings.Contains(e.llm.req.System, "my own style") ||
		!strings.Contains(e.llm.req.Messages[0].Content, "Backend role at Lumen") {
		t.Fatalf("call: feature %q, request %+v", e.llm.feature, e.llm.req)
	}
	if n := e.count(t, `SELECT count(*) FROM outbox WHERE type = 'draft.ready'`); n != 1 {
		t.Fatalf("got %d draft.ready rows, want 1", n)
	}
}

func TestGenerateUsesTheOwnersTemplateForTheContactStatus(t *testing.T) {
	e := newEnv(t, fakeSource{target: app.TargetContext{Summary: "Priya", ContactStatus: "replied"}})
	d := e.newDraft(t, "outreach", "linkedin")
	for _, row := range []struct{ status, text string }{{"replied", "THANK THEM FOR REPLYING"}, {"not_reached", "COLD OPEN"}} {
		if _, err := e.pool.Exec(context.Background(), `INSERT INTO templates (id, owner_id, kind, contact_status, channel, instructions)
			VALUES ($1, $2, 'outreach', $3, 'linkedin', $4)`, store.NewID(), e.owner, row.status, row.text); err != nil {
			t.Fatal(err)
		}
	}

	if err := e.gen.Generate(context.Background(), e.args(d, 1)); err != nil {
		t.Fatalf("generate: %v", err)
	}

	user := e.llm.req.Messages[0].Content
	if !strings.Contains(user, "THANK THEM FOR REPLYING") || strings.Contains(user, "COLD OPEN") || e.llm.feature != "fude.outreach" {
		t.Fatalf("feature %q, prompt:\n%s", e.llm.feature, user)
	}
}

func TestGenerateAddsASecondVersionToAPendingDraft(t *testing.T) {
	e := newEnv(t, fakeSource{})
	d := e.newDraft(t, "cover_letter", "email")
	ctx := context.Background()
	if err := e.gen.Generate(ctx, e.args(d, 1)); err != nil {
		t.Fatal(err)
	}
	e.llm.text = "Subject: Second\n\nA fresh take."

	err := e.gen.Generate(ctx, e.args(d, 2))

	got := e.draft(t, d)
	if err != nil || got.State != "pending" || got.CurrentVersion != 2 {
		t.Fatalf("got %+v, %v", got, err)
	}
	if n := e.count(t, `SELECT count(*) FROM draft_versions WHERE draft_id = $1`, d.ID); n != 2 {
		t.Fatalf("got %d versions, want 2", n)
	}
}

func TestGenerateSkipsWorkThatIsAlreadyDoneOrNoLongerWanted(t *testing.T) {
	e := newEnv(t, fakeSource{})
	ctx := context.Background()
	done := e.newDraft(t, "cover_letter", "email")
	if err := e.gen.Generate(ctx, e.args(done, 1)); err != nil {
		t.Fatal(err)
	}
	discarded := e.newDraft(t, "cover_letter", "email")
	if _, err := e.pool.Exec(ctx, `UPDATE drafts SET state = 'discarded' WHERE id = $1`, discarded.ID); err != nil {
		t.Fatal(err)
	}
	callsBefore := e.llm.calls

	retry := e.gen.Generate(ctx, e.args(done, 1))
	gone := e.gen.Generate(ctx, e.args(discarded, 1))

	if retry != nil || gone != nil || e.llm.calls != callsBefore {
		t.Fatalf("retry %v, discarded %v, model calls %d -> %d; want no calls", retry, gone, callsBefore, e.llm.calls)
	}
	if n := e.count(t, `SELECT count(*) FROM outbox WHERE type = 'draft.ready'`); n != 1 {
		t.Fatalf("got %d draft.ready rows, want only the first", n)
	}
}

func TestGenerateReturnsFailuresWithoutChangingTheDraft(t *testing.T) {
	budget := &llm.BudgetError{ResetsAt: time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)}
	tests := []struct {
		name       string
		source     fakeSource
		llmErr     error
		text       string
		wantReason string
	}{
		{"budget exhausted", fakeSource{}, budget, "x", app.ReasonBudgetExhausted},
		{"metering down", fakeSource{}, llm.ErrMeteringUnavailable, "x", app.ReasonMeteringUnavailable},
		{"model refused", fakeSource{}, llm.ErrRefused, "x", app.ReasonModelRefused},
		{"other model error", fakeSource{}, errors.New("boom"), "x", app.ReasonGenerationFailed},
		{"target unreadable", fakeSource{err: errors.New("kagami down")}, nil, "x", app.ReasonContextUnavailable},
		{"empty answer", fakeSource{}, nil, "Subject: only", app.ReasonEmptyDraft},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, tt.source)
			e.llm.err, e.llm.text = tt.llmErr, tt.text
			d := e.newDraft(t, "cover_letter", "email")

			err := e.gen.Generate(context.Background(), e.args(d, 1))

			if err == nil || app.FailureReason(err) != tt.wantReason {
				t.Fatalf("got %v with reason %q, want %q", err, app.FailureReason(err), tt.wantReason)
			}
			var be *llm.BudgetError
			if tt.wantReason == app.ReasonBudgetExhausted && (!errors.As(err, &be) || be.ResetsAt.IsZero()) {
				t.Fatalf("budget error must keep its reset time, got %v", err)
			}
			if got := e.draft(t, d); got.State != "generating" || got.CurrentVersion != 0 {
				t.Fatalf("draft changed: %+v", got)
			}
			if n := e.count(t, `SELECT count(*) FROM outbox`); n != 0 {
				t.Fatalf("got %d outbox rows, want 0", n)
			}
		})
	}
}

func TestFailMarksAGeneratingDraftFailedAndEmitsOnce(t *testing.T) {
	e := newEnv(t, fakeSource{})
	d := e.newDraft(t, "cover_letter", "email")
	ctx := context.Background()

	err := e.gen.Fail(ctx, e.args(d, 1), app.ReasonGenerationFailed)
	again := e.gen.Fail(ctx, e.args(d, 1), app.ReasonGenerationFailed)

	got := e.draft(t, d)
	if err != nil || again != nil || got.State != "failed" || got.FailureReason == nil || *got.FailureReason != app.ReasonGenerationFailed {
		t.Fatalf("got %+v, %v, %v", got, err, again)
	}
	if n := e.count(t, `SELECT count(*) FROM outbox WHERE type = 'draft.failed'`); n != 1 {
		t.Fatalf("got %d draft.failed rows, want 1", n)
	}
}

func TestFailOfARegenerateKeepsThePendingDraft(t *testing.T) {
	e := newEnv(t, fakeSource{})
	d := e.newDraft(t, "cover_letter", "email")
	ctx := context.Background()
	if err := e.gen.Generate(ctx, e.args(d, 1)); err != nil {
		t.Fatal(err)
	}

	err := e.gen.Fail(ctx, e.args(d, 2), app.ReasonBudgetExhausted)

	got := e.draft(t, d)
	if err != nil || got.State != "pending" || got.CurrentVersion != 1 || got.FailureReason != nil {
		t.Fatalf("got %+v, %v", got, err)
	}
	if n := e.count(t, `SELECT count(*) FROM outbox WHERE type = 'draft.failed'`); n != 1 {
		t.Fatalf("got %d draft.failed rows, want 1", n)
	}
}

func TestFailIgnoresAVersionThatAlreadyExists(t *testing.T) {
	e := newEnv(t, fakeSource{})
	d := e.newDraft(t, "cover_letter", "email")
	ctx := context.Background()
	if err := e.gen.Generate(ctx, e.args(d, 1)); err != nil {
		t.Fatal(err)
	}

	err := e.gen.Fail(ctx, e.args(d, 1), app.ReasonGenerationFailed)

	if err != nil || e.draft(t, d).State != "pending" || e.count(t, `SELECT count(*) FROM outbox WHERE type = 'draft.failed'`) != 0 {
		t.Fatalf("a finished version must not be failed, err %v", err)
	}
}

func TestGenerateAddressesAnEmailDraftToTheContactOnlyWhenItHasNoRecipient(t *testing.T) {
	tests := []struct {
		name      string
		recipient *string
		want      string
	}{
		{"created without a recipient", nil, "priya@lumen.example"},
		{"keeps the one the owner gave", strPtr("owner@given.example"), "owner@given.example"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, fakeSource{target: app.TargetContext{Summary: "Priya", Email: "priya@lumen.example"}})
			d, err := store.New(e.pool).InsertDraft(context.Background(), db.InsertDraftParams{
				ID: store.NewID(), OwnerID: e.owner, Kind: "outreach", TargetType: "contact", TargetID: ptr(store.NewID()),
				Channel: "email", Recipient: tt.recipient,
			})
			if err != nil {
				t.Fatal(err)
			}

			err = e.gen.Generate(context.Background(), e.args(d, 1))

			got := e.draft(t, d)
			if err != nil || got.Recipient == nil || *got.Recipient != tt.want {
				t.Fatalf("err %v, recipient %v, want %s", err, got.Recipient, tt.want)
			}
		})
	}
}

func TestGenerateLeavesALinkedinDraftWithoutARecipient(t *testing.T) {
	e := newEnv(t, fakeSource{target: app.TargetContext{Summary: "Priya", Email: "priya@lumen.example"}})
	d := e.newDraft(t, "outreach", "linkedin")

	err := e.gen.Generate(context.Background(), e.args(d, 1))

	if got := e.draft(t, d); err != nil || got.Recipient != nil {
		t.Fatalf("err %v, recipient %v; a copy-only draft has no recipient", err, got.Recipient)
	}
}

func strPtr(s string) *string { return &s }

func ptr[T any](v T) *T { return &v }
