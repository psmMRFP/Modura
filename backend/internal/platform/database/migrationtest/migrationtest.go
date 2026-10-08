// Package migrationtest resets a dedicated test database schema and applies
// the full migration set, so integration tests always exercise the current
// migrations from an empty database.
package migrationtest

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Prepare drops the wheretolive schema, applies every migration in order, and
// registers cleanup that drops the schema again. A database-wide advisory
// lock serializes concurrent test packages against the same database.
func Prepare(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	cleanup, err := Reset(context.Background(), pool)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DROP SCHEMA IF EXISTS wheretolive CASCADE")
		cleanup()
	})
}
