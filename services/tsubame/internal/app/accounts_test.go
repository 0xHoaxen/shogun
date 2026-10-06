package app_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/pkg/postgres/postgrestest"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/app"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/envelope"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/mail"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/mail/mailtest"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/store"
	"github.com/0xHoaxen/shogun/services/tsubame/migrations"
)

func TestMain(m *testing.M) { os.Exit(postgrestest.Run(m)) }

const refresh = "1//0g-refresh-token-value"

func newAccounts(t *testing.T) (*app.Accounts, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgrestest.NewDatabase(t), "tsubame")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ring, err := envelope.NewKeyring("k1", map[string][]byte{"k1": bytes.Repeat([]byte{7}, envelope.KeySize)})
	if err != nil {
		t.Fatal(err)
	}
	return app.NewAccounts(pool, ring), pool
}

func TestConnectStoresTheTokenSealedAndTokenReturnsIt(t *testing.T) {
	accounts, pool := newAccounts(t)
	ctx := context.Background()
	owner := uuid.New()

	acc, err := accounts.Connect(ctx, owner, "gmail", " Me@Example.com ", refresh)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	got, tokenErr := accounts.Token(ctx, owner, acc.ID)

	if acc.Address != "me@example.com" || acc.Status != "active" || acc.TokenKeyID != "k1" {
		t.Fatalf("got %+v", acc)
	}
	if bytes.Contains(acc.TokenCiphertext, []byte(refresh)) {
		t.Fatal("the stored token is not encrypted")
	}
	var inDB []byte
	if err := pool.QueryRow(ctx, `SELECT token_ciphertext FROM accounts WHERE id = $1`, acc.ID).Scan(&inDB); err != nil || bytes.Contains(inDB, []byte("refresh-token")) {
		t.Fatalf("db value leaks the token or cannot be read: %v", err)
	}
	if tokenErr != nil || got != refresh {
		t.Fatalf("token %q, %v", got, tokenErr)
	}
}

func TestConnectingAgainKeepsTheAccountAndReplacesTheToken(t *testing.T) {
	accounts, pool := newAccounts(t)
	ctx := context.Background()
	owner := uuid.New()
	first, _ := accounts.Connect(ctx, owner, "gmail", "me@example.com", refresh)
	if _, err := pool.Exec(ctx, `UPDATE accounts SET status = 'reauth_required', history_id = '42' WHERE id = $1`, first.ID); err != nil {
		t.Fatal(err)
	}

	again, err := accounts.Connect(ctx, owner, "gmail", "ME@example.com", "1//new-token")
	got, tokenErr := accounts.Token(ctx, owner, first.ID)

	if err != nil || again.ID != first.ID || again.Status != "active" || again.HistoryID == nil || *again.HistoryID != "42" {
		t.Fatalf("got %+v, %v; want the same account, active, cursor kept", again, err)
	}
	if tokenErr != nil || got != "1//new-token" {
		t.Fatalf("token %q, %v", got, tokenErr)
	}
	if list, _ := accounts.List(ctx, owner); len(list) != 1 {
		t.Fatalf("got %d accounts, want 1", len(list))
	}
}

func TestTokenRefusesWhatItShouldNotOpen(t *testing.T) {
	accounts, pool := newAccounts(t)
	ctx := context.Background()
	owner := uuid.New()
	acc, _ := accounts.Connect(ctx, owner, "gmail", "me@example.com", refresh)
	other, _ := accounts.Connect(ctx, owner, "gmail", "other@example.com", "1//other")

	if _, err := pool.Exec(ctx, `UPDATE accounts SET token_ciphertext = (SELECT token_ciphertext FROM accounts WHERE id = $1) WHERE id = $2`, acc.ID, other.ID); err != nil {
		t.Fatal(err)
	}
	_, moved := accounts.Token(ctx, owner, other.ID)
	_, stranger := accounts.Token(ctx, uuid.New(), acc.ID)
	_, missing := accounts.Token(ctx, owner, uuid.New())
	if _, err := pool.Exec(ctx, `UPDATE accounts SET status = 'disabled' WHERE id = $1`, acc.ID); err != nil {
		t.Fatal(err)
	}
	_, disabled := accounts.Token(ctx, owner, acc.ID)

	if !errors.Is(moved, envelope.ErrCorrupt) {
		t.Errorf("a token copied to another account opened: %v", moved)
	}
	if !errors.Is(stranger, store.ErrNotFound) || !errors.Is(missing, store.ErrNotFound) {
		t.Errorf("another owner or a missing id: %v, %v", stranger, missing)
	}
	if !errors.Is(disabled, app.ErrAccountNotUsable) {
		t.Errorf("a disabled account: %v", disabled)
	}
	if moved != nil && strings.Contains(moved.Error(), refresh) {
		t.Error("the error leaks the token")
	}
}

func TestConnectRejectsIncompleteInput(t *testing.T) {
	accounts, _ := newAccounts(t)
	tests := []struct{ name, provider, address, token string }{
		{"unknown provider", "outlook", "me@example.com", refresh},
		{"no address", "gmail", " ", refresh},
		{"no token", "gmail", "me@example.com", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := accounts.Connect(context.Background(), uuid.New(), tt.provider, tt.address, tt.token); err == nil {
				t.Fatal("want an error")
			}
		})
	}
}

func TestAProviderThatFindsAccessRevokedMarksTheAccountForReauth(t *testing.T) {
	accounts, pool := newAccounts(t)
	ctx := context.Background()
	owner := uuid.New()
	acc, _ := accounts.Connect(ctx, owner, "gmail", "me@example.com", refresh)
	fake := &mailtest.Fake{Err: mail.ErrAuthRevoked}
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, nil))
	p, err := accounts.Provider(ctx, owner, acc.ID, fake, log)
	if err != nil {
		t.Fatalf("provider: %v", err)
	}

	_, callErr := p.List(ctx, mail.ListQuery{})
	var status string
	_ = pool.QueryRow(ctx, `SELECT status FROM accounts WHERE id = $1`, acc.ID).Scan(&status)
	_, again := accounts.Provider(ctx, owner, acc.ID, fake, log)

	if !errors.Is(callErr, mail.ErrAuthRevoked) || status != "reauth_required" {
		t.Fatalf("call %v, status %q", callErr, status)
	}
	if !errors.Is(again, app.ErrAccountNotUsable) {
		t.Fatalf("an account needing reauth was opened again: %v", again)
	}
	if len(fake.Tokens) != 1 || fake.Tokens[0] != refresh {
		t.Fatalf("factory got tokens %v", fake.Tokens)
	}
	if strings.Contains(logs.String(), refresh) || !strings.Contains(logs.String(), acc.ID.String()) {
		t.Fatalf("log must name the account and never the token:\n%s", logs.String())
	}
}

func TestOtherProviderErrorsLeaveTheAccountActive(t *testing.T) {
	accounts, pool := newAccounts(t)
	ctx := context.Background()
	owner := uuid.New()
	acc, _ := accounts.Connect(ctx, owner, "gmail", "me@example.com", refresh)
	fake := &mailtest.Fake{Err: &mail.APIError{Status: 429, Message: "RESOURCE_EXHAUSTED"}}
	p, _ := accounts.Provider(ctx, owner, acc.ID, fake, slog.New(slog.DiscardHandler))

	_, err := p.Get(ctx, "m1")
	var status string
	_ = pool.QueryRow(ctx, `SELECT status FROM accounts WHERE id = $1`, acc.ID).Scan(&status)

	var api *mail.APIError
	if !errors.As(err, &api) || status != "active" {
		t.Fatalf("err %v, status %q", err, status)
	}
}

func TestAGuardedProviderPassesCallsThrough(t *testing.T) {
	accounts, _ := newAccounts(t)
	ctx := context.Background()
	owner := uuid.New()
	acc, _ := accounts.Connect(ctx, owner, "gmail", "me@example.com", refresh)
	fake := &mailtest.Fake{Address: "me@example.com", Messages: []mail.Message{{ID: "m1", Subject: "hi"}}}
	p, _ := accounts.Provider(ctx, owner, acc.ID, fake, slog.New(slog.DiscardHandler))

	profile, pErr := p.Profile(ctx)
	page, lErr := p.List(ctx, mail.ListQuery{})
	msg, gErr := p.Get(ctx, "m1")
	hist, hErr := p.History(ctx, "0")
	sent, sErr := p.Send(ctx, mail.Outgoing{To: []string{"a@b.example"}, DraftID: "d1"})

	if pErr != nil || lErr != nil || gErr != nil || hErr != nil || sErr != nil {
		t.Fatalf("errors: %v %v %v %v %v", pErr, lErr, gErr, hErr, sErr)
	}
	if profile.Address != "me@example.com" || len(page.IDs) != 1 || msg.Subject != "hi" || len(hist.AddedIDs) != 1 || sent.ID == "" || fake.SentCount() != 1 {
		t.Fatalf("got %+v %+v %+v %+v %+v", profile, page, msg, hist, sent)
	}
}
