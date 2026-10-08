package migrationtest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// migrationsDir resolves the repository migration directory relative to this
// file so every calling package observes the same migration set.
func migrationsDir() string {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "migrations")
}

func migrationNames() ([]string, error) {
	entries, err := os.ReadDir(migrationsDir())
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".up.sql") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

func migrationFile(name string) ([]byte, error) {
	return os.ReadFile(filepath.Join(migrationsDir(), name))
}

// RollbackAfter reverses a freshly prepared full schema down to the requested
// version. Dependent migrations must be undone first; tests must not drop an
// older owning table while later foreign keys still reference it.
func RollbackAfter(ctx context.Context, pool *pgxpool.Pool, version int) error {
	if version < 0 || !strings.HasSuffix(pool.Config().ConnConfig.Database, "_test") {
		return fmt.Errorf("invalid test rollback target")
	}
	names, err := migrationNames()
	if err != nil {
		return err
	}
	for i := len(names) - 1; i >= 0; i-- {
		name := names[i]
		ordinal, err := strconv.Atoi(strings.SplitN(name, "_", 2)[0])
		if err != nil {
			return fmt.Errorf("invalid migration filename")
		}
		if ordinal <= version {
			break
		}
		downName := strings.TrimSuffix(name, ".up.sql") + ".down.sql"
		down, err := migrationFile(downName)
		if err != nil {
			return err
		}
		if _, err := pool.Exec(ctx, string(down)); err != nil {
			return fmt.Errorf("rollback %s: %w", downName, err)
		}
	}
	return nil
}
