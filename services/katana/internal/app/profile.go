package app

import (
	"context"
	"fmt"
	"math"
	"net/mail"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	katanav1 "github.com/0xHoaxen/shogun/gen/go/shogun/katana/v1"
	"github.com/0xHoaxen/shogun/pkg/outbox"
	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/services/katana/internal/store"
	"github.com/0xHoaxen/shogun/services/katana/internal/store/db"
)

const (
	eventProfileUpdated = "profile.updated"

	maxProfileSkills = 200
	// The same caps as a parsed resume (see resume_parse.go).
	maxTextRunes  = 120
	maxHeadline   = 200
	maxSkillRunes = 60

	reasonProfileInvalid = "PROFILE_INVALID"
)

// Where a saved skill came from. A third, "job", is for skills taken from
// saved roles and is not one a user can send.
const (
	// SourceResume is a skill read from the owner's resume.
	SourceResume = "resume"
	// SourceManual is a skill the owner added themselves.
	SourceManual = "manual"
)

// ProfileRole is one saved job. It has the shape of a parsed one.
type ProfileRole = ParsedRole

// ProfileSkill is a skill the owner kept, with a level from 0 to 10.
type ProfileSkill struct {
	Name   string
	Level  int
	Source string
}

// Profile is what the owner kept. Version is for the next SaveProfile.
type Profile struct {
	Name, Headline, Location, Email string
	YearsExperience                 *float64
	Version                         int32
	Roles                           []ProfileRole
}

// SaveProfileInput is the profile the owner kept. Version is 0 to create the
// profile, else the version they read.
type SaveProfileInput struct {
	Version                         int32
	Name, Headline, Location, Email string
	YearsExperience                 *float64
	Roles                           []ProfileRole
	Skills                          []ProfileSkill
}

// InvalidProfileError means the profile to save did not hold up. Nothing was written.
type InvalidProfileError struct{ Msg string }

func (e *InvalidProfileError) Error() string { return reasonProfileInvalid + ": " + e.Msg }

func invalidProfile(format string, args ...any) error {
	return &InvalidProfileError{Msg: fmt.Sprintf(format, args...)}
}

// SaveProfile stores the profile the owner kept, in one transaction with its
// profile.updated event. Roles are replaced by the list sent. Skills are added
// or raised: one that is not sent is left alone, and a level never goes down.
// A stale Version is store.ErrVersionConflict.
func (s *Service) SaveProfile(ctx context.Context, owner uuid.UUID, in SaveProfileInput) (Profile, error) {
	in, err := checkProfile(in)
	if err != nil {
		return Profile{}, err
	}
	now := s.now().UTC()
	var saved db.Profile
	err = postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		repo := store.New(tx)
		var err error
		if in.Version == 0 {
			saved, err = repo.InsertProfile(ctx, db.InsertProfileParams{
				OwnerID: owner, Name: in.Name, Headline: in.Headline, Location: in.Location,
				Email: in.Email, YearsExperience: in.YearsExperience, Now: now,
			})
		} else {
			saved, err = repo.UpdateProfile(ctx, db.UpdateProfileParams{
				OwnerID: owner, Version: in.Version, Name: in.Name, Headline: in.Headline, Location: in.Location,
				Email: in.Email, YearsExperience: in.YearsExperience, Now: now,
			})
		}
		if err != nil {
			return err
		}
		roles := make([]db.InsertProfileRoleParams, len(in.Roles))
		for i, r := range in.Roles {
			roles[i] = db.InsertProfileRoleParams{
				Position: int32(i), Title: r.Title, Company: r.Company,
				StartMonth: store.MonthPtr(r.Start), EndMonth: store.MonthPtr(r.End), IsCurrent: r.Current,
			}
		}
		if err := repo.ReplaceProfileRoles(ctx, owner, roles); err != nil {
			return err
		}
		for _, sk := range in.Skills {
			if err := repo.UpsertSkill(ctx, db.UpsertSkillParams{
				OwnerID: owner, NameKey: strings.ToLower(sk.Name), Name: sk.Name,
				Level: int16(sk.Level), Source: sk.Source, Now: now,
			}); err != nil {
				return err
			}
		}
		if _, err := outbox.Write(ctx, tx, eventSource, eventProfileUpdated, owner.String(), &katanav1.ProfileUpdated{
			OwnerId: owner.String(), Version: saved.Version,
		}); err != nil {
			return fmt.Errorf("write %s event: %w", eventProfileUpdated, err)
		}
		return nil
	})
	if err != nil {
		return Profile{}, wrap("save profile", err)
	}
	return Profile{
		Name: saved.Name, Headline: saved.Headline, Location: saved.Location, Email: saved.Email,
		YearsExperience: saved.YearsExperience, Version: saved.Version, Roles: in.Roles,
	}, nil
}

// GetProfile returns the owner's profile, or store.ErrNotFound.
func (s *Service) GetProfile(ctx context.Context, owner uuid.UUID) (Profile, error) {
	repo := store.New(s.pool)
	row, err := repo.GetProfile(ctx, owner)
	if err != nil {
		return Profile{}, err
	}
	rows, err := repo.ListProfileRoles(ctx, owner)
	if err != nil {
		return Profile{}, err
	}
	roles := make([]ProfileRole, len(rows))
	for i, r := range rows {
		roles[i] = ProfileRole{Title: r.Title, Company: r.Company, Current: r.IsCurrent}
		if r.StartMonth != nil {
			roles[i].Start = *r.StartMonth
		}
		if r.EndMonth != nil {
			roles[i].End = *r.EndMonth
		}
	}
	return Profile{
		Name: row.Name, Headline: row.Headline, Location: row.Location, Email: row.Email,
		YearsExperience: row.YearsExperience, Version: row.Version, Roles: roles,
	}, nil
}

// ListSkills returns the owner's skills, strongest first.
func (s *Service) ListSkills(ctx context.Context, owner uuid.UUID) ([]ProfileSkill, error) {
	rows, err := store.New(s.pool).ListSkills(ctx, owner)
	if err != nil {
		return nil, err
	}
	skills := make([]ProfileSkill, len(rows))
	for i, r := range rows {
		skills[i] = ProfileSkill{Name: r.Name, Level: int(r.Level), Source: r.Source}
	}
	return skills, nil
}

// checkProfile returns in trimmed, or says what is wrong with it.
func checkProfile(in SaveProfileInput) (SaveProfileInput, error) {
	if in.Version < 0 {
		return in, invalidProfile("version cannot be negative")
	}
	in.Name = strings.TrimSpace(in.Name)
	in.Headline = strings.TrimSpace(in.Headline)
	in.Location = strings.TrimSpace(in.Location)
	in.Email = strings.TrimSpace(in.Email)
	if len([]rune(in.Name)) > maxTextRunes || len([]rune(in.Location)) > maxTextRunes || len([]rune(in.Headline)) > maxHeadline {
		return in, invalidProfile("name, headline or location is too long")
	}
	if in.Email != "" {
		if addr, err := mail.ParseAddress(in.Email); err != nil || addr.Address != in.Email || len(in.Email) > 254 {
			return in, invalidProfile("email is not a valid address")
		}
	}
	if y := in.YearsExperience; y != nil && (math.IsNaN(*y) || *y < 0 || *y > maxYears) {
		return in, invalidProfile("years of experience must be between 0 and %d", maxYears)
	}
	roles, err := checkRolesToSave(in.Roles)
	if err != nil {
		return in, err
	}
	skills, err := checkSkillsToSave(in.Skills)
	if err != nil {
		return in, err
	}
	in.Roles, in.Skills = roles, skills
	return in, nil
}

func checkRolesToSave(in []ProfileRole) ([]ProfileRole, error) {
	if len(in) > maxParsedRoles {
		return nil, invalidProfile("at most %d roles", maxParsedRoles)
	}
	roles := make([]ProfileRole, 0, len(in))
	for _, r := range in {
		r.Title, r.Company = strings.TrimSpace(r.Title), strings.TrimSpace(r.Company)
		r.Start, r.End = strings.TrimSpace(r.Start), strings.TrimSpace(r.End)
		switch {
		case r.Title == "" && r.Company == "":
			return nil, invalidProfile("a role needs a title or a company")
		case len([]rune(r.Title)) > maxTextRunes || len([]rune(r.Company)) > maxTextRunes:
			return nil, invalidProfile("a role's title or company is too long")
		case r.Start != "" && !validMonth(r.Start), r.End != "" && !validMonth(r.End):
			return nil, invalidProfile("role dates must be YYYY-MM")
		case r.Current && r.End != "":
			return nil, invalidProfile("a current role has no end date")
		case r.Start != "" && r.End != "" && r.End < r.Start:
			return nil, invalidProfile("a role ends before it starts")
		}
		roles = append(roles, r)
	}
	return roles, nil
}

func checkSkillsToSave(in []ProfileSkill) ([]ProfileSkill, error) {
	if len(in) > maxProfileSkills {
		return nil, invalidProfile("at most %d skills", maxProfileSkills)
	}
	skills := make([]ProfileSkill, 0, len(in))
	for _, sk := range in {
		sk.Name = strings.TrimSpace(sk.Name)
		switch {
		case sk.Name == "" || len([]rune(sk.Name)) > maxSkillRunes:
			return nil, invalidProfile("a skill needs a name of up to %d characters", maxSkillRunes)
		case sk.Level < 0 || sk.Level > maxSkillScore:
			return nil, invalidProfile("a skill level is from 0 to %d", maxSkillScore)
		case sk.Source != SourceResume && sk.Source != SourceManual:
			return nil, invalidProfile("a skill's source is resume or manual")
		}
		skills = append(skills, sk)
	}
	return skills, nil
}
