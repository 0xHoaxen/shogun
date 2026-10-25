package app_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/0xHoaxen/shogun/pkg/llm"
	"github.com/0xHoaxen/shogun/services/katana/internal/app"
)

const posting = "Backend Engineer. You will write Go and PostgreSQL services on Kubernetes. Python is a plus."

type fakeJobs struct {
	text  string
	err   error
	calls int
}

func (f *fakeJobs) JobPosting(context.Context, uuid.UUID) (string, error) {
	f.calls++
	return f.text, f.err
}

func skillsSvc(t *testing.T, model app.Completer, jobs app.JobSource) (*app.Service, *pgxpool.Pool) {
	t.Helper()
	pool := newPool(t)
	return newSvc(t, pool, nil, nil, app.WithSuggestions(nil, model, nil), app.WithJobSkills(jobs)), pool
}

func TestExtractJobSkillsStoresWhatThePostingAsksForAndCountsTheJobAsRead(t *testing.T) {
	model := &recorder{text: `{"skills":["Go","postgresql","Rust","go"]}`}
	svc, _ := skillsSvc(t, model, &fakeJobs{text: posting})
	owner, job := uuid.New(), uuid.New()

	err := svc.ExtractJobSkills(context.Background(), owner, job)

	list, lerr := svc.ListSkills(context.Background(), owner)
	if err != nil || lerr != nil || list.RolesRead != 1 || len(list.Skills) != 2 {
		t.Fatalf("err %v %v, list %+v; want Go and postgresql only (Rust is not in the posting)", err, lerr, list)
	}
	if model.feature != "katana.job_skills" || model.req.Cache || !strings.HasPrefix(model.req.Messages[0].Content, `{"posting":"`) {
		t.Errorf("feature %q, cache %v, content %.30s", model.feature, model.req.Cache, model.req.Messages[0].Content)
	}
	for _, sk := range list.Skills {
		if sk.Have || sk.AskedIn != 1 || sk.Level != 0 {
			t.Errorf("skill %+v, want asked once and not had", sk)
		}
	}
}

func TestAJobIsReadOnceAndAPostingWithoutTextCostsNothing(t *testing.T) {
	model := &recorder{text: `{"skills":["Go"]}`}
	source := &fakeJobs{text: posting}
	svc, _ := skillsSvc(t, model, source)
	owner, job := uuid.New(), uuid.New()
	ctx := context.Background()

	_ = svc.ExtractJobSkills(ctx, owner, job)
	err := svc.ExtractJobSkills(ctx, owner, job)

	if err != nil || model.calls != 1 || source.calls != 1 {
		t.Fatalf("err %v, %d model calls, %d reads; want the second run to do nothing", err, model.calls, source.calls)
	}

	empty := &recorder{}
	svc2, pool2 := skillsSvc(t, empty, &fakeJobs{text: "  "})
	if err := svc2.ExtractJobSkills(ctx, owner, job); err != nil || empty.calls != 0 {
		t.Fatalf("empty posting: err %v, %d calls", err, empty.calls)
	}
	var n int
	if err := pool2.QueryRow(ctx, `SELECT count(*) FROM job_skill_runs`).Scan(&n); err != nil || n != 0 {
		t.Errorf("%d run rows for a job with no posting (%v)", n, err)
	}
}

func TestJobSkillsFailuresReachTheWorkerAsTheErrorsItActsOn(t *testing.T) {
	ctx := context.Background()
	for _, target := range []error{llm.ErrProviderNotConfigured, llm.ErrBudgetExhausted} {
		svc, _ := skillsSvc(t, &recorder{err: target}, &fakeJobs{text: posting})
		if err := svc.ExtractJobSkills(ctx, uuid.New(), uuid.New()); !errors.Is(err, target) {
			t.Errorf("err = %v, want %v kept", err, target)
		}
	}
	svc, _ := skillsSvc(t, &recorder{text: "not json"}, &fakeJobs{text: posting})
	if err := svc.ExtractJobSkills(ctx, uuid.New(), uuid.New()); err == nil {
		t.Error("an answer that is not JSON was accepted")
	}
	off := newSvc(t, newPool(t), nil, nil)
	if err := off.ExtractJobSkills(ctx, uuid.New(), uuid.New()); !errors.Is(err, app.ErrJobSkillsOff) {
		t.Errorf("err = %v, want ErrJobSkillsOff", err)
	}
	svc, _ = skillsSvc(t, &recorder{text: `{"skills":[]}`}, &fakeJobs{err: errors.New("kagami down")})
	if err := svc.ExtractJobSkills(ctx, uuid.New(), uuid.New()); err == nil {
		t.Error("a failed job read was swallowed")
	}
}

func TestListSkillsJoinsWhatTheOwnerHasWithWhatRolesAskForMostAskedFirst(t *testing.T) {
	ctx := context.Background()
	model := &recorder{}
	source := &fakeJobs{}
	svc, _ := skillsSvc(t, model, source)
	owner := uuid.New()
	if _, err := svc.SaveProfile(ctx, owner, app.SaveProfileInput{Name: "P", Skills: []app.ProfileSkill{
		{Name: "Go", Level: 7, Source: app.SourceResume}, {Name: "Rust", Level: 2, Source: app.SourceManual},
	}}); err != nil {
		t.Fatal(err)
	}
	postings := []struct{ text, answer string }{
		{"We use Go and Kubernetes", `{"skills":["Go","Kubernetes"]}`},
		{"Go with Kubernetes and Terraform", `{"skills":["go","Kubernetes","Terraform"]}`},
		{"Kubernetes only", `{"skills":["Kubernetes"]}`},
	}
	for _, p := range postings {
		source.text, model.text = p.text, p.answer
		if err := svc.ExtractJobSkills(ctx, owner, uuid.New()); err != nil {
			t.Fatal(err)
		}
	}

	list, err := svc.ListSkills(ctx, owner)

	want := []app.SkillRow{
		{Name: "Kubernetes", AskedIn: 3},
		{Name: "Go", Level: 7, Source: app.SourceResume, Have: true, AskedIn: 2},
		{Name: "Terraform", AskedIn: 1},
		{Name: "Rust", Level: 2, Source: app.SourceManual, Have: true},
	}
	if err != nil || list.RolesRead != 3 || len(list.Skills) != len(want) {
		t.Fatalf("err %v, list %+v", err, list)
	}
	for i := range want {
		if list.Skills[i] != want[i] {
			t.Errorf("row %d = %+v, want %+v", i, list.Skills[i], want[i])
		}
	}
	other, err := svc.ListSkills(ctx, uuid.New())
	if err != nil || other.RolesRead != 0 || len(other.Skills) != 0 {
		t.Errorf("another owner sees %+v (%v)", other, err)
	}
}
