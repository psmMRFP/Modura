package database

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestValidateURLRequiresDedicatedDatabaseAndRole(t *testing.T) {
	t.Setenv("PGDATABASE", "")
	t.Setenv("PGUSER", "application")
	for _, dsn := range []string{"postgres://application@localhost/postgres", "postgres://application@localhost/template1", "postgres://postgres@localhost/wheretolive_test", "postgres://application@localhost/", "host=localhost user=application dbname=template0", "postgres://application@localhost/" + strings.Repeat("a", 64), "postgres://application:private-secret@localhost/%zz"} {
		if err := ValidateURL(dsn); err == nil || strings.Contains(err.Error(), "private-secret") {
			t.Fatalf("invalid DSN was accepted or disclosed: %v", err)
		}
	}
	for _, dsn := range []string{"postgres://application@localhost/wheretolive_test", "host=localhost user=application dbname=atlas sslmode=require", "postgres://application@localhost/a%22b"} {
		if err := ValidateURL(dsn); err != nil {
			t.Fatal(err)
		}
	}
}
func TestEnsureTargetOnlyCreatesAfterMissingDatabase(t *testing.T) {
	missing := fmt.Errorf("wrapped: %w", &pgconn.PgError{Code: "3D000"})
	cases := []struct {
		name       string
		err        error
		enabled    bool
		wantCreate bool
	}{
		{"existing", nil, true, false}, {"missing", missing, true, true}, {"disabled", missing, false, false},
		{"auth", &pgconn.PgError{Code: "28P01"}, true, false},
		{"permission", &pgconn.PgError{Code: "42501"}, true, false},
		{"network", errors.New("private network error"), true, false},
		{"timeout", context.DeadlineExceeded, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			connections, creations := 0, 0
			created, err := ensureTarget(tc.enabled, func() error {
				connections++
				if connections == 1 {
					return tc.err
				}
				return nil
			}, func() (bool, error) { creations++; return true, nil })
			if (creations == 1) != tc.wantCreate || created != tc.wantCreate {
				t.Fatalf("created=%v creates=%d err=%v", created, creations, err)
			}
			if tc.wantCreate && (connections != 2 || err != nil) {
				t.Fatalf("missing target did not reconnect: %d %v", connections, err)
			}
			if !tc.wantCreate && tc.err != nil && !errors.Is(err, tc.err) {
				t.Fatal("connection error identity lost")
			}
		})
	}
}
func TestDatabaseCreationQuotesNameAndHandlesRaces(t *testing.T) {
	ctx := context.Background()
	created, err := createDatabase(ctx, `a"; DROP DATABASE other;--`, func(_ context.Context, sql string) error {
		if sql != `CREATE DATABASE "a""; DROP DATABASE other;--" TEMPLATE template0` {
			t.Fatalf("unsafe identifier: %s", sql)
		}
		return nil
	})
	if !created || err != nil {
		t.Fatal(err)
	}
	for _, serverErr := range []*pgconn.PgError{{Code: "42P04"}, {Code: "23505", ConstraintName: "pg_database_datname_index"}} {
		created, err := createDatabase(ctx, "wheretolive_test", func(context.Context, string) error { return serverErr })
		if created || err != nil {
			t.Fatalf("concurrent create: %v %v", created, err)
		}
	}
	denied := &pgconn.PgError{Code: "42501", Message: "private server detail"}
	_, err = createDatabase(ctx, "wheretolive_test", func(context.Context, string) error { return denied })
	if !errors.Is(err, denied) {
		t.Fatal("permission error was ignored")
	}
}
func TestCreationFailureAndReconnectFailureAreSafe(t *testing.T) {
	privateErr := errors.New("postgres://user:private-secret@server/database")
	for _, reconnectFailure := range []bool{false, true} {
		calls := 0
		_, err := ensureTarget(true, func() error {
			calls++
			if calls == 1 {
				return &pgconn.PgError{Code: "3D000"}
			}
			return privateErr
		}, func() (bool, error) {
			if reconnectFailure {
				return true, nil
			}
			return false, privateErr
		})
		if !errors.Is(err, privateErr) || strings.Contains(err.Error(), "private-secret") {
			t.Fatalf("error lost or leaked: %v", err)
		}
	}
}

func TestConcurrentCreationStillReconnects(t *testing.T) {
	calls := 0
	created, err := ensureTarget(true, func() error {
		calls++
		if calls == 1 {
			return &pgconn.PgError{Code: "3D000"}
		}
		return nil
	}, func() (bool, error) { return false, nil })
	if err != nil || created || calls != 2 {
		t.Fatalf("race result: created=%v calls=%d err=%v", created, calls, err)
	}
}
