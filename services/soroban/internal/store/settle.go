package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/services/soroban/internal/store/db"
)

// LockReservation returns the owner's reservation locked for update until the
// transaction ends, or ErrNotFound.
func (r *Repo) LockReservation(ctx context.Context, owner, id uuid.UUID) (db.Reservation, error) {
	res, err := r.q.LockReservation(ctx, db.LockReservationParams{ID: id, OwnerID: owner})
	return res, mapErr(err)
}

// LockReservationByID is LockReservation for the sweeper, which acts for no
// owner.
func (r *Repo) LockReservationByID(ctx context.Context, id uuid.UUID) (db.Reservation, error) {
	res, err := r.q.LockReservationByID(ctx, id)
	return res, mapErr(err)
}

// SetReservationStatus moves a reservation to status.
func (r *Repo) SetReservationStatus(ctx context.Context, id uuid.UUID, status string) error {
	return r.q.SetReservationStatus(ctx, db.SetReservationStatusParams{ID: id, Status: status})
}

// ExpiredReservationIDs returns up to limit open reservations whose expiry has
// passed at now, oldest first.
func (r *Repo) ExpiredReservationIDs(ctx context.Context, now time.Time, limit int32) ([]uuid.UUID, error) {
	return r.q.ListExpiredReservationIDs(ctx, db.ListExpiredReservationIDsParams{Now: now, BatchSize: limit})
}

// LockPeriods returns the periods with the given ids locked for update, in
// budget id order.
func (r *Repo) LockPeriods(ctx context.Context, ids []uuid.UUID) ([]db.BudgetPeriod, error) {
	return r.q.LockPeriodsByID(ctx, ids)
}

// SettlePeriod gives release back from a period's reserved total and adds spend
// to its spent total.
func (r *Repo) SettlePeriod(ctx context.Context, periodID uuid.UUID, release, spend int64) error {
	return r.q.SettlePeriod(ctx, db.SettlePeriodParams{ID: periodID, ReleaseMicros: release, SpendMicros: spend})
}

// BudgetsByID returns the budgets with the given ids, by id.
func (r *Repo) BudgetsByID(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]db.Budget, error) {
	rows, err := r.q.ListBudgetsByID(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list budgets by id: %w", err)
	}
	byID := make(map[uuid.UUID]db.Budget, len(rows))
	for _, b := range rows {
		byID[b.ID] = b
	}
	return byID, nil
}

// InsertLedgerEntry appends a ledger row.
func (r *Repo) InsertLedgerEntry(ctx context.Context, p db.InsertLedgerEntryParams) (db.Ledger, error) {
	return r.q.InsertLedgerEntry(ctx, p)
}

// LedgerEntryByReservation returns the ledger row a reservation was committed
// into, or ErrNotFound.
func (r *Repo) LedgerEntryByReservation(ctx context.Context, reservationID uuid.UUID) (db.Ledger, error) {
	e, err := r.q.GetLedgerEntryByReservation(ctx, reservationID)
	return e, mapErr(err)
}
