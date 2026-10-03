package domain

import "slices"

// transitions maps a status to the statuses it may move to. A status with no
// entry, or an empty list, has no way out.
type transitions[S comparable] map[S][]S

func (t transitions[S]) allows(from, to S) bool {
	return slices.Contains(t[from], to)
}
