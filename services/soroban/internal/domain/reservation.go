package domain

// ReservationStatus is where a reservation is in its life.
type ReservationStatus string

// Reservation statuses.
const (
	ReservationOpen      ReservationStatus = "open"
	ReservationCommitted ReservationStatus = "committed"
	ReservationReleased  ReservationStatus = "released"
	ReservationExpired   ReservationStatus = "expired"
)

// CanSettle reports whether a reservation in status s may still be committed,
// released or expired. Only an open reservation may; the others are final.
func (s ReservationStatus) CanSettle() bool { return s == ReservationOpen }

// CanCommit reports whether usage may still be recorded against a reservation
// in status s. An expired reservation can: the call it was held for ran, so
// its spend is real even though the hold was given back.
func (s ReservationStatus) CanCommit() bool {
	return s == ReservationOpen || s == ReservationExpired
}
