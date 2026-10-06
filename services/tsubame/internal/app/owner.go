package app

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/pkg/authz"
)

// ErrNoOwner means the call carries no identity, or one whose owner is not a
// UUID. Transport maps it to PermissionDenied.
var ErrNoOwner = errors.New("app: call has no valid owner")

// ownerFrom returns the owner the call acts for.
func ownerFrom(ctx context.Context) (uuid.UUID, error) {
	id, ok := authz.FromContext(ctx)
	if !ok {
		return uuid.Nil, ErrNoOwner
	}
	owner, err := uuid.Parse(id.OwnerID)
	if err != nil {
		return uuid.Nil, ErrNoOwner
	}
	return owner, nil
}
