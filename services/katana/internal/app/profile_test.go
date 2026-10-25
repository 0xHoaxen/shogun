package app_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/proto"

	eventsv1 "github.com/0xHoaxen/shogun/gen/go/shogun/events/v1"
	katanav1 "github.com/0xHoaxen/shogun/gen/go/shogun/katana/v1"
	"github.com/0xHoaxen/shogun/services/katana/internal/app"
	"github.com/0xHoaxen/shogun/services/katana/internal/store"
)

func profileSvc(t *testing.T) (*app.Service, *pgxpool.Pool) {
	t.Helper()
	pool := newPool(t)
	return newSvc(t, pool, nil, nil), pool
}

func years(y float64) *float64 { return &y }

func basicProfile() app.SaveProfileInput {
	return app.SaveProfileInput{
		Name: "Priya Raman", Headline: "Staff Engineer", Email: "priya@example.com", YearsExperience: years(9.5),
		Roles: []app.ProfileRole{
			{Title: "Staff Engineer", Company: "Lumen", Start: "2021-03", Current: true},
			{Title: "Engineer", Company: "Acme", Start: "2017-06", End: "2021-02"},
		},
		Skills: []app.ProfileSkill{{Name: "Go", Level: 7, Source: app.SourceResume}, {Name: "Rust", Level: 2, Source: app.SourceManual}},
	}
}

func profileEvents(t *testing.T, pool *pgxpool.Pool) []*katanav1.ProfileUpdated {
	t.Helper()
	rows, err := pool.Query(context.Background(), `SELECT payload FROM outbox WHERE type = 'profile.updated' ORDER BY id`)
	if err != nil {
		t.Fatalf("query outbox: %v", err)
	}
	defer rows.Close()
	var out []*katanav1.ProfileUpdated
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var env eventsv1.Envelope
		if err := proto.Unmarshal(raw, &env); err != nil {
			t.Fatal(err)
		}
		var ev katanav1.ProfileUpdated
		if err := env.GetPayload().UnmarshalTo(&ev); err != nil {
			t.Fatal(err)
		}
		out = append(out, &ev)
	}
	return out
}

func TestSaveProfileCreatesThenUpdatesWithTheVersionAndTellsTheBus(t *testing.T) {
	svc, pool := profileSvc(t)
	owner := uuid.New()
	ctx := context.Background()

	first, err := svc.SaveProfile(ctx, owner, basicProfile())
	if err != nil || first.Version != 1 {
		t.Fatalf("create: %+v, %v", first, err)
	}
	changed := basicProfile()
	changed.Version, changed.Headline = 1, "Principal Engineer"
	changed.Roles = changed.Roles[:1]
	second, err := svc.SaveProfile(ctx, owner, changed)
	if err != nil || second.Version != 2 {
		t.Fatalf("update: %+v, %v", second, err)
	}

	got, err := svc.GetProfile(ctx, owner)
	if err != nil || got.Headline != "Principal Engineer" || got.Version != 2 || len(got.Roles) != 1 || !got.Roles[0].Current || got.Roles[0].End != "" {
		t.Fatalf("got %+v, %v; want the update with one role", got, err)
	}
	events := profileEvents(t, pool)
	if len(events) != 2 || events[0].Version != 1 || events[1].Version != 2 || events[1].OwnerId != owner.String() {
		t.Fatalf("events = %v, want one per save with the new version", events)
	}
}

func TestSaveProfileRefusesAStaleOrMissingVersionAndWritesNothing(t *testing.T) {
	svc, pool := profileSvc(t)
	owner := uuid.New()
	ctx := context.Background()
	if _, err := svc.SaveProfile(ctx, owner, basicProfile()); err != nil {
		t.Fatal(err)
	}
	tests := map[string]int32{"create again": 0, "stale": 5}
	for name, version := range tests {
		t.Run(name, func(t *testing.T) {
			in := basicProfile()
			in.Version, in.Name = version, "Changed"

			_, err := svc.SaveProfile(ctx, owner, in)

			if !errors.Is(err, store.ErrVersionConflict) {
				t.Fatalf("err = %v, want a version conflict", err)
			}
		})
	}
	if got, _ := svc.GetProfile(ctx, owner); got.Name != "Priya Raman" {
		t.Errorf("name = %q, a refused save changed the profile", got.Name)
	}
	if n := len(profileEvents(t, pool)); n != 1 {
		t.Errorf("%d events, want only the first save's", n)
	}
	in := basicProfile()
	in.Version = 1
	if _, err := svc.SaveProfile(ctx, uuid.New(), in); !errors.Is(err, store.ErrVersionConflict) {
		t.Errorf("updating a profile that does not exist: %v", err)
	}
}

func TestSkillLevelsOnlyGoUpAndSkillsNotSentAreKept(t *testing.T) {
	svc, _ := profileSvc(t)
	owner := uuid.New()
	ctx := context.Background()
	if _, err := svc.SaveProfile(ctx, owner, basicProfile()); err != nil {
		t.Fatal(err)
	}
	next := app.SaveProfileInput{Version: 1, Name: "Priya Raman", Skills: []app.ProfileSkill{
		{Name: "go", Level: 3, Source: app.SourceManual},     // lower: ignored
		{Name: "Rust", Level: 6, Source: app.SourceManual},   // higher: raised
		{Name: "Python", Level: 4, Source: app.SourceManual}, // new
	}}

	if _, err := svc.SaveProfile(ctx, owner, next); err != nil {
		t.Fatal(err)
	}

	list, err := svc.ListSkills(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	skills := list.Skills
	want := []app.SkillRow{
		{Name: "Go", Level: 7, Source: app.SourceResume, Have: true},
		{Name: "Rust", Level: 6, Source: app.SourceManual, Have: true},
		{Name: "Python", Level: 4, Source: app.SourceManual, Have: true},
	}
	if len(skills) != len(want) {
		t.Fatalf("skills = %+v, want %+v", skills, want)
	}
	for i := range want {
		if skills[i] != want[i] {
			t.Errorf("skill %d = %+v, want %+v", i, skills[i], want[i])
		}
	}
}

func TestOneOwnerNeverSeesAnothersProfileOrSkills(t *testing.T) {
	svc, _ := profileSvc(t)
	ctx := context.Background()
	a, b := uuid.New(), uuid.New()
	if _, err := svc.SaveProfile(ctx, a, basicProfile()); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.GetProfile(ctx, b); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("profile of b: %v, want not found", err)
	}
	if list, err := svc.ListSkills(ctx, b); err != nil || len(list.Skills) != 0 {
		t.Errorf("skills of b: %+v, %v", list, err)
	}
}

func TestSaveProfileRefusesWhatDoesNotHoldUp(t *testing.T) {
	tests := []struct {
		name   string
		change func(in *app.SaveProfileInput)
	}{
		{"negative version", func(in *app.SaveProfileInput) { in.Version = -1 }},
		{"long name", func(in *app.SaveProfileInput) { in.Name = strings.Repeat("a", 121) }},
		{"bad email", func(in *app.SaveProfileInput) { in.Email = "nope" }},
		{"years too many", func(in *app.SaveProfileInput) { in.YearsExperience = years(61) }},
		{"role with nothing", func(in *app.SaveProfileInput) { in.Roles = []app.ProfileRole{{}} }},
		{"bad month", func(in *app.SaveProfileInput) { in.Roles[0].Start = "2021-13" }},
		{"current role with an end", func(in *app.SaveProfileInput) { in.Roles[0].End = "2022-01" }},
		{"ends before it starts", func(in *app.SaveProfileInput) { in.Roles[1].End = "2016-01" }},
		{"too many roles", func(in *app.SaveProfileInput) { in.Roles = make([]app.ProfileRole, 26) }},
		{"skill level 11", func(in *app.SaveProfileInput) { in.Skills[0].Level = 11 }},
		{"skill without a name", func(in *app.SaveProfileInput) { in.Skills[0].Name = " " }},
		{"skill from a job", func(in *app.SaveProfileInput) { in.Skills[0].Source = "job" }},
		{"too many skills", func(in *app.SaveProfileInput) { in.Skills = make([]app.ProfileSkill, 201) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, pool := profileSvc(t)
			in := basicProfile()
			tt.change(&in)

			_, err := svc.SaveProfile(context.Background(), uuid.New(), in)

			var bad *app.InvalidProfileError
			if !errors.As(err, &bad) {
				t.Fatalf("err = %v, want InvalidProfileError", err)
			}
			if n := len(profileEvents(t, pool)); n != 0 {
				t.Errorf("%d events for a refused save", n)
			}
		})
	}
}

func TestAFailedSaveLeavesNoProfileAndNoEvent(t *testing.T) {
	svc, pool := profileSvc(t)
	owner := uuid.New()
	// Valid for the app but refused by the database's own check.
	if _, err := pool.Exec(context.Background(), `ALTER TABLE skills ADD CONSTRAINT no_rust CHECK (name_key <> 'rust')`); err != nil {
		t.Fatal(err)
	}

	_, err := svc.SaveProfile(context.Background(), owner, basicProfile())

	if err == nil {
		t.Fatal("want the database to refuse the skill")
	}
	if _, err := svc.GetProfile(context.Background(), owner); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("profile after a failed save: %v, want none", err)
	}
	if n := len(profileEvents(t, pool)); n != 0 {
		t.Errorf("%d events after a failed save", n)
	}
}
