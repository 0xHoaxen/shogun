package app

import (
	"sync"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/services/taiko/internal/store/db"
)

// subscriberBuffer is how many notifications a subscriber may have waiting. A
// subscriber that falls further behind is closed and reconnects from its last
// seen id, so a slow client never holds up the others.
const subscriberBuffer = 32

// subscription is one open stream, fed by the broker.
type subscription struct {
	owner uuid.UUID
	ch    chan db.Notification
}

// broker fans new notifications out to the open streams of their owner.
type broker struct {
	mu   sync.Mutex
	subs map[*subscription]struct{}
}

func newBroker() *broker {
	return &broker{subs: map[*subscription]struct{}{}}
}

// subscribe opens a subscription for owner. The returned function closes it and
// is safe to call more than once.
func (b *broker) subscribe(owner uuid.UUID) (*subscription, func()) {
	sub := &subscription{owner: owner, ch: make(chan db.Notification, subscriberBuffer)}
	b.mu.Lock()
	b.subs[sub] = struct{}{}
	b.mu.Unlock()
	return sub, func() { b.drop(sub) }
}

// drop removes and closes sub if it is still registered.
func (b *broker) drop(sub *subscription) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.subs[sub]; ok {
		delete(b.subs, sub)
		close(sub.ch)
	}
}

// publish hands n to every subscriber of its owner. A subscriber whose buffer
// is full is closed instead of waited for.
func (b *broker) publish(n db.Notification) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for sub := range b.subs {
		if sub.owner != n.OwnerID {
			continue
		}
		select {
		case sub.ch <- n:
		default:
			delete(b.subs, sub)
			close(sub.ch)
		}
	}
}

// reset closes every subscription, so each client reconnects and replays what
// it missed. It is called when the live feed was interrupted.
func (b *broker) reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for sub := range b.subs {
		delete(b.subs, sub)
		close(sub.ch)
	}
}
