// Package apptest holds in-memory fakes of the app layer's dependencies, for
// tests of the layers above it.
package apptest

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/services/torii/internal/domain"
	"github.com/0xHoaxen/shogun/services/torii/internal/store"
)

// MemSessions is an in-memory app.SessionStore keyed by token hash.
type MemSessions struct {
	mu     sync.Mutex
	byHash map[string]domain.Session
}

// NewMemSessions returns an empty MemSessions.
func NewMemSessions() *MemSessions {
	return &MemSessions{byHash: map[string]domain.Session{}}
}

// Len returns how many sessions are stored.
func (m *MemSessions) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.byHash)
}

// Insert implements app.SessionStore.
func (m *MemSessions) Insert(_ context.Context, s domain.Session, hash []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.byHash[string(hash)] = s
	return nil
}

// ByTokenHash implements app.SessionStore.
func (m *MemSessions) ByTokenHash(_ context.Context, hash []byte) (domain.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.byHash[string(hash)]
	if !ok {
		return domain.Session{}, store.ErrNotFound
	}
	return s, nil
}

// Extend implements app.SessionStore.
func (m *MemSessions) Extend(_ context.Context, id uuid.UUID, expiresAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for hash, s := range m.byHash {
		if s.ID == id {
			s.ExpiresAt = expiresAt
			m.byHash[hash] = s
			return nil
		}
	}
	return store.ErrNotFound
}

// Delete implements app.SessionStore.
func (m *MemSessions) Delete(_ context.Context, hash []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.byHash, string(hash))
	return nil
}
