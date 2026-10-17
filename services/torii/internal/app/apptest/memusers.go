package apptest

import (
	"context"
	"sync"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/services/torii/internal/domain"
	"github.com/0xHoaxen/shogun/services/torii/internal/store"
)

// MemUsers is an in-memory app.UserStore keyed by Google subject.
type MemUsers struct {
	mu    sync.Mutex
	byID  map[uuid.UUID]domain.User
	bySub map[string]uuid.UUID
}

// NewMemUsers returns an empty MemUsers.
func NewMemUsers() *MemUsers {
	return &MemUsers{byID: map[uuid.UUID]domain.User{}, bySub: map[string]uuid.UUID{}}
}

// Upsert implements app.UserStore. A new user is an active member; a known one
// only has its email and display name refreshed.
func (m *MemUsers) Upsert(_ context.Context, user domain.User) (domain.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id, ok := m.bySub[user.GoogleSub]; ok {
		existing := m.byID[id]
		existing.Email = user.Email
		existing.DisplayName = user.DisplayName
		m.byID[id] = existing
		return existing, nil
	}
	user.Role = domain.RoleMember
	user.Status = domain.StatusActive
	user.Timezone = domain.DefaultTimezone
	user.Version = 1
	m.byID[user.ID] = user
	m.bySub[user.GoogleSub] = user.ID
	return user, nil
}

// ByID implements app.UserStore.
func (m *MemUsers) ByID(_ context.Context, id uuid.UUID) (domain.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	user, ok := m.byID[id]
	if !ok {
		return domain.User{}, store.ErrNotFound
	}
	return user, nil
}

// SetRole implements app.UserStore.
func (m *MemUsers) SetRole(_ context.Context, id uuid.UUID, role domain.Role) (domain.User, error) {
	return m.update(id, func(u domain.User) domain.User { u.Role = role; return u })
}

// SetGmailAllowed implements app.UserStore.
func (m *MemUsers) SetGmailAllowed(_ context.Context, id uuid.UUID, allowed bool) (domain.User, error) {
	return m.update(id, func(u domain.User) domain.User { u.GmailAllowed = allowed; return u })
}

// SetStatus changes a user's status, for tests that disable a user.
func (m *MemUsers) SetStatus(_ context.Context, id uuid.UUID, status domain.Status) (domain.User, error) {
	return m.update(id, func(u domain.User) domain.User { u.Status = status; return u })
}

func (m *MemUsers) update(id uuid.UUID, change func(domain.User) domain.User) (domain.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	user, ok := m.byID[id]
	if !ok {
		return domain.User{}, store.ErrNotFound
	}
	user = change(user)
	user.Version++
	m.byID[id] = user
	return user, nil
}
