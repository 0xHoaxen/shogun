// Package mailtest has an in-memory mail.Provider for tests of code that reads
// or sends mail. It records what is sent and is safe for concurrent use.
package mailtest

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/0xHoaxen/shogun/services/tsubame/internal/mail"
)

// Fake is a mail.Provider and mail.Factory over a list of messages.
type Fake struct {
	mu sync.Mutex

	// Address is what Profile returns.
	Address string
	// Messages is the mailbox, oldest first. Its position is the history
	// cursor: the cursor "3" means the first three messages were seen.
	Messages []mail.Message
	// Err, if set, is returned by every call; SendErr only by Send.
	Err, SendErr error
	// GetErrFor makes Get fail for the given message ids.
	GetErrFor map[string]error
	// ExpiredBefore makes History fail with ErrHistoryExpired for cursors
	// older than it.
	ExpiredBefore int

	// Sent is everything Send accepted.
	Sent []mail.Outgoing
	// Tokens is the refresh token of each Provider call on the factory.
	Tokens []string
}

// Provider implements mail.Factory. Every provider shares the fake's mailbox.
func (f *Fake) Provider(refreshToken string) mail.Provider {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Tokens = append(f.Tokens, refreshToken)
	return f
}

// Profile implements mail.Provider.
func (f *Fake) Profile(context.Context) (mail.Profile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return mail.Profile{Address: f.Address, HistoryID: strconv.Itoa(len(f.Messages))}, f.Err
}

// List implements mail.Provider. A page holds Max ids (all when Max is zero);
// the page token is the index to continue from. Query is ignored.
func (f *Fake) List(_ context.Context, q mail.ListQuery) (mail.Page, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return mail.Page{}, f.Err
	}
	start, _ := strconv.Atoi(q.PageToken)
	end := len(f.Messages)
	if q.Max > 0 && start+q.Max < end {
		end = start + q.Max
	}
	page := mail.Page{}
	for _, m := range f.Messages[min(start, len(f.Messages)):end] {
		page.IDs = append(page.IDs, m.ID)
	}
	if end < len(f.Messages) {
		page.NextPageToken = strconv.Itoa(end)
	}
	return page, nil
}

// Get implements mail.Provider.
func (f *Fake) Get(_ context.Context, id string) (mail.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return mail.Message{}, f.Err
	}
	if err := f.GetErrFor[id]; err != nil {
		return mail.Message{}, err
	}
	for _, m := range f.Messages {
		if m.ID == id {
			return m, nil
		}
	}
	return mail.Message{}, mail.ErrNotFound
}

// History implements mail.Provider.
func (f *Fake) History(_ context.Context, since string) (mail.History, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return mail.History{}, f.Err
	}
	n, err := strconv.Atoi(since)
	if err != nil || n < f.ExpiredBefore {
		return mail.History{}, mail.ErrHistoryExpired
	}
	h := mail.History{LatestID: strconv.Itoa(len(f.Messages))}
	for _, m := range f.Messages[min(n, len(f.Messages)):] {
		h.AddedIDs = append(h.AddedIDs, m.ID)
	}
	return h, nil
}

// Send implements mail.Provider. The sent message joins the mailbox as an
// outbound message carrying its draft id, as Gmail's Sent folder would.
func (f *Fake) Send(_ context.Context, m mail.Outgoing) (mail.Sent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return mail.Sent{}, f.Err
	}
	if f.SendErr != nil {
		return mail.Sent{}, f.SendErr
	}
	f.Sent = append(f.Sent, m)
	id := fmt.Sprintf("sent-%d", len(f.Sent))
	f.Messages = append(f.Messages, mail.Message{
		ID: id, ThreadID: id, Direction: mail.Outbound, From: m.From, To: slices.Clone(m.To),
		Subject: m.Subject, ReceivedAt: time.Now(), DraftID: m.DraftID,
	})
	return mail.Sent{ID: id, ThreadID: id}, nil
}

// SentCount returns how many messages Send accepted.
func (f *Fake) SentCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.Sent)
}
