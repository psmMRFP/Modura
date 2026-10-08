package database

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ValidateURL rejects implicit database selection and PostgreSQL's built-in
// databases/administrator. It never includes the supplied DSN in errors.
func ValidateURL(dsn string) error {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return safeError("parse PostgreSQL connection configuration", err)
	}
	return validateTarget(cfg.ConnConfig)
}

func validateTarget(cfg *pgx.ConnConfig) error {
	name := cfg.Database
	if strings.TrimSpace(name) == "" || !utf8.ValidString(name) || strings.ContainsRune(name, 0) || len(name) > 63 {
		return fmt.Errorf("an explicit PostgreSQL database name of 1–63 bytes is required")
	}
	for _, reserved := range []string{"postgres", "template0", "template1"} {
		if strings.EqualFold(name, reserved) {
			return fmt.Errorf("built-in PostgreSQL databases cannot be used as application databases")
		}
	}
	if strings.EqualFold(cfg.User, "postgres") {
		return fmt.Errorf("the default postgres role cannot be used; configure a dedicated database role")
	}
	return nil
}

// Open connects to the configured application database. When autoCreate is
// enabled, a confirmed missing database is created through template1, using
// the same credentials and TLS settings, before reconnecting to the target.
// Creating an empty database does not apply application migrations.
func Open(ctx context.Context, dsn string, autoCreate bool) (*pgxpool.Pool, bool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, false, safeError("parse PostgreSQL connection configuration", err)
	}
	if err := validateTarget(cfg.ConnConfig); err != nil {
		return nil, false, err
	}
	openPool := func() (*pgxpool.Pool, error) {
		pool, err := pgxpool.NewWithConfig(ctx, cfg.Copy())
		if err != nil {
			return nil, err
		}
		if err := pool.Ping(ctx); err != nil {
			pool.Close()
			return nil, err
		}
		return pool, nil
	}
	var targetPool *pgxpool.Pool
	created, err := ensureTarget(autoCreate, func() error {
		var err error
		targetPool, err = openPool()
		return err
	}, func() (bool, error) {
		maintenance := cfg.ConnConfig.Copy()
		maintenance.Database = "template1"
		connection, err := pgx.ConnectConfig(ctx, maintenance)
		if err != nil {
			return false, safeError("connect template1 for database creation", err)
		}
		defer func() {
			closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_ = connection.Close(closeCtx)
		}()
		return createDatabase(ctx, cfg.ConnConfig.Database, func(ctx context.Context, sql string) error {
			_, err := connection.Exec(ctx, sql)
			return err
		})
	})
	return targetPool, created, err
}

// ensureTarget keeps the failure gate testable without an actual server.
func ensureTarget(autoCreate bool, connect func() error, create func() (bool, error)) (bool, error) {
	if err := connect(); err != nil {
		if !autoCreate || !hasSQLState(err, "3D000") {
			return false, safeError("connect application database", err)
		}
	} else {
		return false, nil
	}
	created, err := create()
	if err != nil {
		return false, safeError("create application database (the dedicated role requires CREATEDB)", err)
	}
	if err := connect(); err != nil {
		return created, safeError("connect created application database", err)
	}
	return created, nil
}

func createDatabase(ctx context.Context, name string, exec func(context.Context, string) error) (bool, error) {
	err := exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()+" TEMPLATE template0")
	if err == nil {
		return true, nil
	}
	// Another instance may win between the failed target connection and CREATE.
	// PostgreSQL can report either duplicate_database or pg_database uniqueness.
	var pgError *pgconn.PgError
	if hasSQLState(err, "42P04") || (errors.As(err, &pgError) && pgError.Code == "23505" && pgError.ConstraintName == "pg_database_datname_index") {
		return false, nil
	}
	return false, err
}

func hasSQLState(err error, state string) bool {
	var serverError *pgconn.PgError
	return errors.As(err, &serverError) && serverError.Code == state
}

// connectionError preserves semantic identity without logging DSNs, server
// details, usernames, passwords, or arbitrary PostgreSQL error messages.
type connectionError struct {
	operation string
	cause     error
}

func (e *connectionError) Error() string {
	var serverError *pgconn.PgError
	if errors.As(e.cause, &serverError) {
		return e.operation + " failed (SQLSTATE " + serverError.Code + ")"
	}
	if errors.Is(e.cause, context.DeadlineExceeded) {
		return e.operation + " timed out"
	}
	if errors.Is(e.cause, context.Canceled) {
		return e.operation + " cancelled"
	}
	return e.operation + " failed"
}
func (e *connectionError) Unwrap() error { return e.cause }
func safeError(operation string, cause error) error {
	return &connectionError{operation: operation, cause: cause}
}
