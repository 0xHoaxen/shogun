package app

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/services/tsubame/internal/mail"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/store"
)

const statusReauthRequired = "reauth_required"

// Provider returns a mail.Provider for an active account, opened with its
// refresh token. If the provider ever reports that access was revoked, the
// account is marked reauth_required, so nothing retries it until the owner
// connects it again.
func (a *Accounts) Provider(ctx context.Context, owner, id uuid.UUID, factory mail.Factory, log *slog.Logger) (mail.Provider, error) {
	token, err := a.Token(ctx, owner, id)
	if err != nil {
		return nil, err
	}
	return &guarded{inner: factory.Provider(token), accounts: a, owner: owner, id: id, log: log}, nil
}

// guarded marks its account for reauth when a call finds access revoked.
type guarded struct {
	inner    mail.Provider
	accounts *Accounts
	owner    uuid.UUID
	id       uuid.UUID
	log      *slog.Logger
}

func (g *guarded) check(ctx context.Context, err error) error {
	if !errors.Is(err, mail.ErrAuthRevoked) {
		return err
	}
	// The call may have failed because ctx ended; the status must still be
	// recorded, so it is written on a context that outlives the caller's.
	setCtx := context.WithoutCancel(ctx)
	if setErr := store.New(g.accounts.pool).SetAccountStatus(setCtx, g.owner, g.id, statusReauthRequired); setErr != nil {
		g.log.Error("mark account for reauth", slog.String("account_id", g.id.String()), slog.Any("error", setErr))
	} else {
		g.log.Warn("account needs reauth", slog.String("account_id", g.id.String()))
	}
	return err
}

func (g *guarded) Profile(ctx context.Context) (mail.Profile, error) {
	p, err := g.inner.Profile(ctx)
	return p, g.check(ctx, err)
}

func (g *guarded) List(ctx context.Context, q mail.ListQuery) (mail.Page, error) {
	p, err := g.inner.List(ctx, q)
	return p, g.check(ctx, err)
}

func (g *guarded) Get(ctx context.Context, id string) (mail.Message, error) {
	m, err := g.inner.Get(ctx, id)
	return m, g.check(ctx, err)
}

func (g *guarded) History(ctx context.Context, since string) (mail.History, error) {
	h, err := g.inner.History(ctx, since)
	return h, g.check(ctx, err)
}

func (g *guarded) Send(ctx context.Context, m mail.Outgoing) (mail.Sent, error) {
	s, err := g.inner.Send(ctx, m)
	return s, g.check(ctx, err)
}
