package owners

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// PurgeSQL returns a Purge that runs each statement in order with the owner id
// as $1. Order the statements children first, so foreign keys hold. Statements
// come from the service's own code, never from input.
func PurgeSQL(statements ...string) Purge {
	return func(ctx context.Context, tx pgx.Tx, owner uuid.UUID) error {
		for _, stmt := range statements {
			if _, err := tx.Exec(ctx, stmt, owner); err != nil {
				return fmt.Errorf("%q: %w", stmt, err)
			}
		}
		return nil
	}
}
