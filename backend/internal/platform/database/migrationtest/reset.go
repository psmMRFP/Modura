package migrationtest

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Reset drops the wheretolive schema, applies every migration in order, and
// returns a cleanup function that releases the database-wide advisory lock
// without dropping the schema, for callers that want to keep the result
// (browser E2E seeding). Prepare wraps it with test cleanup semantics.
func Reset(ctx context.Context, pool *pgxpool.Pool) (func(), error) {
	lockConnection, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire lock connection: %w", err)
	}
	if _, err := lockConnection.Exec(ctx, "SELECT pg_advisory_lock(1297040469)"); err != nil {
		lockConnection.Release()
		return nil, fmt.Errorf("acquire advisory lock: %w", err)
	}
	cleanup := func() {
		_, _ = lockConnection.Exec(context.Background(), "SELECT pg_advisory_unlock(1297040469)")
		lockConnection.Release()
	}
	if _, err := pool.Exec(ctx, "DROP SCHEMA IF EXISTS wheretolive CASCADE"); err != nil {
		cleanup()
		return nil, fmt.Errorf("drop schema: %w", err)
	}
	names, err := migrationNames()
	if err != nil {
		cleanup()
		return nil, err
	}
	if len(names) == 0 {
		cleanup()
		return nil, fmt.Errorf("no migrations found")
	}
	for _, name := range names {
		migration, err := migrationFile(name)
		if err != nil {
			cleanup()
			return nil, err
		}
		if _, err := pool.Exec(ctx, string(migration)); err != nil {
			cleanup()
			return nil, fmt.Errorf("apply %s: %w", name, err)
		}
	}
	return cleanup, nil
}
