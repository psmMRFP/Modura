// Package postgres persists only feedback-owned data through sqlc.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/feedback"
	feedbackdb "github.com/psmMRFP/WhereToLive/backend/internal/modules/feedback/postgres/db"
)

// Store uses a caller-supplied query handle for reads and transactions for writes.
type Store struct{ queries *feedbackdb.Queries }

// New binds the owner-local query repository.
func New(db feedbackdb.DBTX) Store { return Store{feedbackdb.New(db)} }

// Categories reads active intake categories.
func (s Store) Categories(ctx context.Context) ([]feedback.Category, error) {
	rows, err := s.queries.ListFeedbackCategories(ctx)
	if err != nil {
		return nil, fmt.Errorf("feedback categories: %w", err)
	}
	result := make([]feedback.Category, 0, len(rows))
	for _, row := range rows {
		result = append(result, feedback.Category{Key: row.Key, Label: row.Label})
	}
	return result, nil
}

// List returns stable paginated private intake.
func (s Store) List(ctx context.Context, q feedback.Query) ([]feedback.Entry, error) {
	rows, err := s.queries.ListFeedbackIntake(ctx, feedbackdb.ListFeedbackIntakeParams{Status: q.Status, CategoryKey: q.Category, PageLimit: int32(q.Limit), PageOffset: int32(q.Offset)})
	if err != nil {
		return nil, fmt.Errorf("feedback list: %w", err)
	}
	result := make([]feedback.Entry, 0, len(rows))
	for _, row := range rows {
		result = append(result, entry(row))
	}
	return result, nil
}

// Insert requires an active category at the actual write, not just a UI lookup.
func (Store) Insert(ctx context.Context, tx pgx.Tx, e feedback.Entry) (feedback.Entry, error) {
	var place pgtype.UUID
	if e.PlaceID != nil {
		place = pgtype.UUID{Bytes: uuid.MustParse(*e.PlaceID), Valid: true}
	}
	row, err := feedbackdb.New(tx).InsertFeedbackIntake(ctx, feedbackdb.InsertFeedbackIntakeParams{ID: e.ID, Key: e.Category, PlaceID: place, Title: e.Title, Message: e.Message, CreatedAt: e.CreatedAt})
	if errors.Is(err, pgx.ErrNoRows) {
		return feedback.Entry{}, feedback.ErrInvalid
	}
	return entry(row), mapError(err)
}

// Lock supplies the latest version within the caller's write transaction.
func (Store) Lock(ctx context.Context, tx pgx.Tx, id string) (feedback.Entry, error) {
	row, err := feedbackdb.New(tx).LockFeedbackIntake(ctx, id)
	return entry(row), mapError(err)
}

// Update advances the aggregate version with the caller's expected version.
func (Store) Update(ctx context.Context, tx pgx.Tx, e feedback.Entry) (feedback.Entry, error) {
	var outcome pgtype.Text
	if e.Outcome != nil {
		outcome = pgtype.Text{String: *e.Outcome, Valid: true}
	}
	row, err := feedbackdb.New(tx).ReviewFeedbackIntake(ctx, feedbackdb.ReviewFeedbackIntakeParams{ID: e.ID, Status: e.Status, Outcome: outcome, UpdatedAt: e.UpdatedAt, Version: e.Version})
	if errors.Is(err, pgx.ErrNoRows) {
		return feedback.Entry{}, feedback.ErrConflict
	}
	return entry(row), mapError(err)
}
func entry(row feedbackdb.WheretoliveFeedbackIntake) feedback.Entry {
	e := feedback.Entry{ID: row.ID, Category: row.CategoryKey, Title: row.Title, Message: row.Message, Status: row.Status, Version: row.Version, CreatedAt: row.CreatedAt.UTC(), UpdatedAt: row.UpdatedAt.UTC()}
	if row.PlaceID.Valid {
		id := uuid.UUID(row.PlaceID.Bytes).String()
		e.PlaceID = &id
	}
	if row.Outcome.Valid {
		outcome := row.Outcome.String
		e.Outcome = &outcome
	}
	return e
}
func mapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return feedback.ErrNotFound
	}
	var pgerr *pgconn.PgError
	if errors.As(err, &pgerr) {
		switch pgerr.Code {
		case "23503", "23514":
			return feedback.ErrInvalid
		case "23505":
			return feedback.ErrConflict
		}
	}
	return fmt.Errorf("feedback persistence: %w", err)
}
