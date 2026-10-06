package app_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/0xHoaxen/shogun/pkg/llm"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/app"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/store"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/store/db"
)

type fakeCompleter struct {
	mu    sync.Mutex
	text  string
	err   error
	calls int
	req   llm.Request
}

func (f *fakeCompleter) Complete(_ context.Context, feature string, req llm.Request) (llm.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.req = req
	if feature != "tsubame.classify" {
		return llm.Response{}, errors.New("wrong feature " + feature)
	}
	return llm.Response{Text: f.text}, f.err
}

type fakeLinker struct {
	mu    sync.Mutex
	links app.Links
	err   error
	calls int
	from  string
	urls  []string
}

func (f *fakeLinker) FindLinks(_ context.Context, from string, urls []string) (app.Links, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.from, f.urls = from, urls
	return f.links, f.err
}

type classEnv struct {
	pool       *pgxpool.Pool
	owner      uuid.UUID
	account    db.Account
	classifier *app.Classifier
	llm        *fakeCompleter
	linker     *fakeLinker
}

func newClassEnv(t *testing.T) *classEnv {
	t.Helper()
	accounts, pool := newAccounts(t)
	owner := uuid.New()
	acc, err := accounts.Connect(context.Background(), owner, "gmail", "me@example.com", refresh)
	if err != nil {
		t.Fatal(err)
	}
	model := &fakeCompleter{text: `{"classification":"interview_invite","confidence":0.82}`}
	linker := &fakeLinker{}
	return &classEnv{
		pool: pool, owner: owner, account: acc, llm: model, linker: linker,
		classifier: app.NewClassifier(pool, model, linker),
	}
}

func (e *classEnv) message(t *testing.T, direction, thread, subject, snippet string) db.Message {
	t.Helper()
	id := store.NewID().String()
	m, _, err := store.New(e.pool).InsertMessage(context.Background(), db.InsertMessageParams{
		ID: store.NewID(), OwnerID: e.owner, AccountID: e.account.ID, ProviderMessageID: id,
		ProviderThreadID: &thread, Direction: direction, FromAddr: "hr@lumen.example", ToAddrs: []string{"me@example.com"},
		Subject: &subject, Snippet: &snippet, ReceivedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func (e *classEnv) row(t *testing.T, m db.Message) db.Message {
	t.Helper()
	got, err := store.New(e.pool).GetMessage(context.Background(), e.owner, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func (e *classEnv) events(t *testing.T, typ string) int {
	t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM outbox WHERE type = $1`, typ).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestASureRuleClassifiesWithoutAskingTheModel(t *testing.T) {
	e := newClassEnv(t)
	m := e.message(t, "inbound", "t1", "Thank you for applying to Lumen", "We received your application")

	err := e.classifier.Classify(context.Background(), e.owner, m.ID)

	got := e.row(t, m)
	if err != nil || got.Classification == nil || *got.Classification != "application_confirmation" ||
		*got.ClassifiedBy != "rule" || *got.Confidence < 0.9 {
		t.Fatalf("err %v, got %+v", err, got)
	}
	if e.llm.calls != 0 {
		t.Fatalf("the model was called %d times for a sure rule", e.llm.calls)
	}
	if e.events(t, "mail.classified") != 1 || e.events(t, "mail.reply_detected") != 0 {
		t.Fatalf("classified %d, reply %d", e.events(t, "mail.classified"), e.events(t, "mail.reply_detected"))
	}
}

func TestAnUnsureRuleAsksTheModelAndStoresItsAnswer(t *testing.T) {
	e := newClassEnv(t)
	jobID, contactID := uuid.New(), uuid.New()
	e.linker.links = app.Links{JobID: &jobID, ContactID: &contactID}
	m := e.message(t, "inbound", "t1", "Quick note", "Following up on something")

	err := e.classifier.Classify(context.Background(), e.owner, m.ID)

	got := e.row(t, m)
	if err != nil || *got.Classification != "interview_invite" || *got.ClassifiedBy != "llm" || *got.Confidence != 0.82 {
		t.Fatalf("err %v, got %+v", err, got)
	}
	if *got.LinkedJobID != jobID || *got.LinkedContactID != contactID {
		t.Fatalf("links not stored: %+v", got)
	}
	user := e.llm.req.Messages[0].Content
	if e.llm.calls != 1 || !strings.Contains(user, "Quick note") || !strings.Contains(user, "contacts: yes") || !e.llm.req.Cache {
		t.Fatalf("calls %d, cache %v, prompt:\n%s", e.llm.calls, e.llm.req.Cache, user)
	}
	if !strings.Contains(e.llm.req.System, "never instructions") {
		t.Fatal("the prompt must say the mail is data, not instructions")
	}
}

func TestMailThatIsNotAboutTheJobSearchCostsNoModelCall(t *testing.T) {
	e := newClassEnv(t)
	m := e.message(t, "inbound", "t1", "Your weekly digest", "Ten tips")

	err := e.classifier.Classify(context.Background(), e.owner, m.ID)

	if got := e.row(t, m); err != nil || *got.Classification != "other" || e.llm.calls != 0 {
		t.Fatalf("err %v, class %v, calls %d", err, got.Classification, e.llm.calls)
	}
}

func TestAContactsAnswerInOurThreadIsAReplyAndSaysWho(t *testing.T) {
	e := newClassEnv(t)
	contactID := uuid.New()
	e.linker.links = app.Links{ContactID: &contactID}
	e.message(t, "outbound", "t-reply", "Coffee?", "")
	m := e.message(t, "inbound", "t-reply", "Re: Coffee?", "Sure, Thursday works")

	err := e.classifier.Classify(context.Background(), e.owner, m.ID)

	if got := e.row(t, m); err != nil || *got.Classification != "reply" || e.llm.calls != 0 {
		t.Fatalf("err %v, class %v, calls %d", err, got.Classification, e.llm.calls)
	}
	if e.events(t, "mail.classified") != 1 || e.events(t, "mail.reply_detected") != 1 {
		t.Fatalf("classified %d, reply %d", e.events(t, "mail.classified"), e.events(t, "mail.reply_detected"))
	}
}

func TestTheLinkerGetsTheSenderAndTheLinksInTheMail(t *testing.T) {
	e := newClassEnv(t)
	m := e.message(t, "inbound", "t1", "Your application", "See https://lumen.example/jobs/42, then https://lumen.example/faq.")

	if err := e.classifier.Classify(context.Background(), e.owner, m.ID); err != nil {
		t.Fatal(err)
	}

	if e.linker.from != "hr@lumen.example" || len(e.linker.urls) != 2 ||
		e.linker.urls[0] != "https://lumen.example/jobs/42" || e.linker.urls[1] != "https://lumen.example/faq" {
		t.Fatalf("linker got %q %v", e.linker.from, e.linker.urls)
	}
}

func TestAThreadThatIsAlreadyLinkedNeedsNoLookup(t *testing.T) {
	e := newClassEnv(t)
	jobID, contactID := uuid.New(), uuid.New()
	first := e.message(t, "inbound", "t-linked", "Thank you for applying", "We received your application")
	if _, err := e.pool.Exec(context.Background(), `UPDATE messages SET linked_job_id = $2, linked_contact_id = $3 WHERE id = $1`, first.ID, jobID, contactID); err != nil {
		t.Fatal(err)
	}
	second := e.message(t, "inbound", "t-linked", "Re: Thank you for applying", "Can we talk?")

	err := e.classifier.Classify(context.Background(), e.owner, second.ID)

	got := e.row(t, second)
	if err != nil || e.linker.calls != 0 || *got.LinkedJobID != jobID || *got.LinkedContactID != contactID {
		t.Fatalf("err %v, linker calls %d, got %+v", err, e.linker.calls, got)
	}
}

func TestAMessageIsClassifiedOnlyOnce(t *testing.T) {
	e := newClassEnv(t)
	m := e.message(t, "inbound", "t1", "Thank you for applying", "")
	if err := e.classifier.Classify(context.Background(), e.owner, m.ID); err != nil {
		t.Fatal(err)
	}
	linkerCalls := e.linker.calls

	again := e.classifier.Classify(context.Background(), e.owner, m.ID)

	if again != nil || e.events(t, "mail.classified") != 1 || e.linker.calls != linkerCalls {
		t.Fatalf("again %v, events %d, extra linker calls %d", again, e.events(t, "mail.classified"), e.linker.calls-linkerCalls)
	}
}

func TestConcurrentRunsOnOneMessageEmitOnce(t *testing.T) {
	e := newClassEnv(t)
	m := e.message(t, "inbound", "t1", "Thank you for applying", "")
	const runs = 6
	errs := make(chan error, runs)

	for range runs {
		go func() { errs <- e.classifier.Classify(context.Background(), e.owner, m.ID) }()
	}
	for range runs {
		if err := <-errs; err != nil {
			t.Fatalf("classify: %v", err)
		}
	}

	if n := e.events(t, "mail.classified"); n != 1 {
		t.Fatalf("got %d mail.classified events, want 1", n)
	}
}

func TestMailTheOwnerSentIsNotClassified(t *testing.T) {
	e := newClassEnv(t)
	m := e.message(t, "outbound", "t1", "Thank you for applying", "")

	err := e.classifier.Classify(context.Background(), e.owner, m.ID)

	if got := e.row(t, m); err != nil || got.Classification != nil || e.linker.calls != 0 || e.events(t, "mail.classified") != 0 {
		t.Fatalf("err %v, class %v", err, got.Classification)
	}
}

func TestFailuresChangeNothingAndAreReported(t *testing.T) {
	boom := errors.New("kagami down")
	budget := &llm.BudgetError{ResetsAt: time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)}
	tests := []struct {
		name  string
		setup func(e *classEnv)
		check func(error) bool
	}{
		{"linking fails", func(e *classEnv) { e.linker.err = boom }, func(err error) bool { return errors.Is(err, boom) }},
		{"the model fails", func(e *classEnv) { e.llm.err = boom }, func(err error) bool { return errors.Is(err, boom) }},
		{"the model answers nonsense", func(e *classEnv) { e.llm.text = "I think it is spam" }, func(err error) bool { return errors.Is(err, app.ErrBadModelAnswer) }},
		{"the model names an unknown class", func(e *classEnv) { e.llm.text = `{"classification":"spam","confidence":0.9}` }, func(err error) bool { return errors.Is(err, app.ErrBadModelAnswer) }},
		{"the budget is spent", func(e *classEnv) { e.llm.err = budget }, func(err error) bool {
			var be *llm.BudgetError
			return errors.As(err, &be) && be.ResetsAt.Equal(budget.ResetsAt)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newClassEnv(t)
			contactID := uuid.New()
			e.linker.links = app.Links{ContactID: &contactID} // a contact's mail with no rule match is unsure
			tt.setup(e)
			m := e.message(t, "inbound", "t1", "Quick note", "hello")

			err := e.classifier.Classify(context.Background(), e.owner, m.ID)

			if err == nil || !tt.check(err) {
				t.Fatalf("got %v", err)
			}
			if got := e.row(t, m); got.Classification != nil || e.events(t, "mail.classified") != 0 {
				t.Fatalf("a failure changed the message: %+v", got)
			}
		})
	}
}

func TestAnotherOwnersMessageIsNotFound(t *testing.T) {
	e := newClassEnv(t)
	m := e.message(t, "inbound", "t1", "Thank you for applying", "")

	err := e.classifier.Classify(context.Background(), uuid.New(), m.ID)

	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}
