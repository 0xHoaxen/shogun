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
