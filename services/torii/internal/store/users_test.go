package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/proto"

	eventsv1 "github.com/0xHoaxen/shogun/gen/go/shogun/events/v1"
	toriiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/torii/v1"
	"github.com/0xHoaxen/shogun/services/torii/internal/domain"
	"github.com/0xHoaxen/shogun/services/torii/internal/store"
)

func newUser(sub string) domain.User {
	return domain.User{
		ID:          store.NewID(),
		GoogleSub:   sub,
		Email:       sub + "@example.com",
		DisplayName: "User " + sub,
	}
}

func TestUpsertCreatesActiveMember(t *testing.T) {
	// Arrange
	_, pool := newSessions(t)
	users := store.NewUsers(pool)

	// Act
	got, err := users.Upsert(context.Background(), newUser("a"))
	// Assert
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if got.Role != domain.RoleMember || got.Status != domain.StatusActive {
		t.Fatalf("role %q status %q, want member active", got.Role, got.Status)
	}
	if got.GmailAllowed || got.Timezone != domain.DefaultTimezone || got.Version != 1 {
		t.Fatalf("gmail %v tz %q version %d, want false %q 1", got.GmailAllowed, got.Timezone, got.Version, domain.DefaultTimezone)
	}
}

func TestUpsertRefreshesProfileButKeepsAdminSettings(t *testing.T) {
	// Arrange
	_, pool := newSessions(t)
	users := store.NewUsers(pool)
	ctx := context.Background()
	first, err := users.Upsert(ctx, newUser("a"))
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if _, err := users.SetStatus(ctx, first.ID, domain.StatusDisabled); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	again := newUser("a")
	again.Email = "renamed@example.com"
	again.DisplayName = "Renamed"

	// Act
	got, err := users.Upsert(ctx, again)
	// Assert
	if err != nil {
		t.Fatalf("Upsert again: %v", err)
	}
	if got.ID != first.ID {
		t.Fatalf("id %v, want the first row's %v", got.ID, first.ID)
	}
	if got.Email != "renamed@example.com" || got.DisplayName != "Renamed" {
		t.Fatalf("profile not refreshed: %+v", got)
	}
	if got.Status != domain.StatusDisabled {
		t.Fatalf("status %q, want disabled kept across logins", got.Status)
	}
}

func TestByIDUnknownIsNotFound(t *testing.T) {
	_, pool := newSessions(t)

	_, err := store.NewUsers(pool).ByID(context.Background(), store.NewID())

	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestSetRoleStatusGmailAndTimezone(t *testing.T) {
	// Arrange
	_, pool := newSessions(t)
	users := store.NewUsers(pool)
	ctx := context.Background()
	u, err := users.Upsert(ctx, newUser("a"))
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	// Act
	admin, roleErr := users.SetRole(ctx, u.ID, domain.RoleAdmin)
	gmail, gmailErr := users.SetGmailAllowed(ctx, u.ID, true)
	zoned, tzErr := users.SetTimezone(ctx, u.ID, "Europe/Berlin")

	// Assert
	if roleErr != nil || gmailErr != nil || tzErr != nil {
		t.Fatalf("role %v gmail %v tz %v", roleErr, gmailErr, tzErr)
	}
	if admin.Role != domain.RoleAdmin || !gmail.GmailAllowed || zoned.Timezone != "Europe/Berlin" {
		t.Fatalf("role %q gmail %v tz %q", admin.Role, gmail.GmailAllowed, zoned.Timezone)
	}
	if zoned.Version != 4 {
		t.Fatalf("version %d, want 4 after three updates", zoned.Version)
	}
}

func TestSetOnUnknownUserIsNotFound(t *testing.T) {
	_, pool := newSessions(t)

	_, err := store.NewUsers(pool).SetStatus(context.Background(), store.NewID(), domain.StatusDisabled)

	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestListUsersPagesByCreation(t *testing.T) {
	// Arrange
	_, pool := newSessions(t)
	users := store.NewUsers(pool)
	ctx := context.Background()
	for _, sub := range []string{"a", "b", "c"} {
		if _, err := users.Upsert(ctx, newUser(sub)); err != nil {
			t.Fatalf("Upsert %s: %v", sub, err)
		}
	}

	// Act
	page1, next, err := users.List(ctx, 2, "")
	page2, last, err2 := users.List(ctx, 2, next)

	// Assert
	if err != nil || err2 != nil {
		t.Fatalf("List: %v %v", err, err2)
	}
	if len(page1) != 2 || next == "" || len(page2) != 1 || last != "" {
		t.Fatalf("pages %d/%d next %q last %q, want 2/1 and a token only after page 1", len(page1), len(page2), next, last)
	}
}

// outboxEvents returns the types and payloads of the events written to the outbox, oldest first.
func outboxEvents(t *testing.T, pool *pgxpool.Pool) []*eventsv1.Envelope {
	t.Helper()
	rows, err := pool.Query(context.Background(), `SELECT payload FROM outbox ORDER BY id`)
	if err != nil {
		t.Fatalf("query outbox: %v", err)
	}
	defer rows.Close()
	var out []*eventsv1.Envelope
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			t.Fatalf("scan: %v", err)
		}
		var env eventsv1.Envelope
		if err := proto.Unmarshal(raw, &env); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		out = append(out, &env)
	}
	return out
}

func TestFirstLoginEmitsUserActivatedOnceAndLaterLoginsEmitNothing(t *testing.T) {
	// Arrange
	_, pool := newSessions(t)
	users := store.NewUsers(pool)
	ctx := context.Background()
	u := newUser("a")

	// Act
	first, err := users.Upsert(ctx, u)
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if _, err := users.Upsert(ctx, u); err != nil {
		t.Fatalf("Upsert again: %v", err)
	}
	events := outboxEvents(t, pool)

	// Assert
	if len(events) != 1 || events[0].GetType() != "user.activated" || events[0].GetSubject() != first.ID.String() {
		t.Fatalf("events = %v, want one user.activated about the user", events)
	}
	var p toriiv1.UserActivated
	if err := events[0].GetPayload().UnmarshalTo(&p); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if p.GetOwnerId() != first.ID.String() || p.GetStatus() != "active" || p.GetTimezone() != domain.DefaultTimezone || p.GetVersion() != 1 {
		t.Fatalf("payload = %v", &p)
	}
}

func TestStatusAndTimezoneChangesEmitUserUpdatedAndRoleDoesNot(t *testing.T) {
	// Arrange
	_, pool := newSessions(t)
	users := store.NewUsers(pool)
	ctx := context.Background()
	u, err := users.Upsert(ctx, newUser("a"))
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	// Act
	_, _ = users.SetRole(ctx, u.ID, domain.RoleAdmin)
	_, _ = users.SetGmailAllowed(ctx, u.ID, true)
	_, statusErr := users.SetStatus(ctx, u.ID, domain.StatusDisabled)
	_, zoneErr := users.SetTimezone(ctx, u.ID, "Europe/Berlin")
	events := outboxEvents(t, pool)

	// Assert
	if statusErr != nil || zoneErr != nil {
		t.Fatalf("status %v, timezone %v", statusErr, zoneErr)
	}
	if len(events) != 3 || events[1].GetType() != "user.updated" || events[2].GetType() != "user.updated" {
		t.Fatalf("events = %v, want user.activated then two user.updated", events)
	}
	var last toriiv1.UserUpdated
	if err := events[2].GetPayload().UnmarshalTo(&last); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if last.GetStatus() != "disabled" || last.GetTimezone() != "Europe/Berlin" || last.GetVersion() != 5 {
		t.Fatalf("last payload = %v, want disabled, Europe/Berlin, version 5", &last)
	}
}

func TestAFailedChangeWritesNoEvent(t *testing.T) {
	_, pool := newSessions(t)

	_, err := store.NewUsers(pool).SetStatus(context.Background(), store.NewID(), domain.StatusDisabled)

	if !errors.Is(err, store.ErrNotFound) || len(outboxEvents(t, pool)) != 0 {
		t.Fatalf("err = %v, events = %d, want ErrNotFound and no event", err, len(outboxEvents(t, pool)))
	}
}
