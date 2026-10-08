package postgres

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/audit"
	auditpostgres "github.com/psmMRFP/WhereToLive/backend/internal/modules/audit/postgres"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/feedback"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/places"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/platformadmin"
	"github.com/psmMRFP/WhereToLive/backend/internal/platform/database"
	"github.com/psmMRFP/WhereToLive/backend/internal/platform/database/migrationtest"
	"github.com/psmMRFP/WhereToLive/backend/internal/platform/identifier"
)

type placeReader struct{}

func (placeReader) Get(context.Context, platformadmin.Actor, string) (places.Entry, error) {
	return places.Entry{}, places.ErrNotFound
}

type unavailableAudit struct{}

func (unavailableAudit) RecordPlatformWrite(context.Context, pgx.Tx, audit.PlatformEvent) error {
	return errors.New("audit unavailable")
}
func TestFeedbackTransactionsFiltersAndConstraints(t *testing.T) {
	url := os.Getenv("WHERETOLIVE_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("WHERETOLIVE_TEST_DATABASE_URL is not set")
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(cfg.ConnConfig.Database, "_test") {
		t.Fatal("dedicated test database required")
	}
	ctx := context.Background()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	migrationtest.Prepare(t, pool)
	newID := func(now time.Time) (string, error) { id, err := identifier.NewUUIDv7(now, nil); return string(id), err }
	auditor, err := audit.NewService(auditpostgres.New(pool), newID)
	if err != nil {
		t.Fatal(err)
	}
	if err := auditor.EnableQueries(auditpostgres.New(pool)); err != nil {
		t.Fatal(err)
	}
	store := New(pool)
	s, err := feedback.NewService(store, database.NewTransactor(pool), auditor, placeReader{}, time.Now, newID)
	if err != nil {
		t.Fatal(err)
	}
	w := feedback.WriteContext{Actor: platformadmin.Actor{AdministratorID: "018bcfe5-6800-7000-8000-000000007101", SessionID: "018bcfe5-6800-7000-8000-000000007102"}, Reason: "feedback integration", CorrelationID: "feedback-integration"}
	input := feedback.Create{Category: "bug", Title: "Private fixture", Message: "private-feedback-body"}
	e, err := s.Create(ctx, w, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(ctx, w, feedback.Create{Category: "account", Title: "Unsupported", Message: "body"}); !errors.Is(err, feedback.ErrInvalid) {
		t.Fatal("unknown category accepted", err)
	}
	broken, _ := feedback.NewService(store, database.NewTransactor(pool), unavailableAudit{}, placeReader{}, time.Now, newID)
	if _, err := broken.Create(ctx, w, input); err == nil {
		t.Fatal("unaudited creation committed")
	}
	page, err := s.List(ctx, w.Actor, feedback.Query{Limit: 1, Status: "open", Category: "bug"})
	if err != nil || len(page.Items) != 1 || page.NextOffset != nil {
		t.Fatalf("rollback or list %+v %v", page, err)
	}
	e, err = s.Review(ctx, w, e.ID, feedback.Review{Version: 1, Status: "in_review"})
	if err != nil {
		t.Fatal(err)
	}
	outcome := "private-outcome"
	if _, err := broken.Review(ctx, w, e.ID, feedback.Review{Version: e.Version, Status: "resolved", Outcome: &outcome}); err == nil {
		t.Fatal("unaudited review committed")
	}
	if _, err := s.Review(ctx, w, e.ID, feedback.Review{Version: 1, Status: "dismissed", Outcome: &outcome}); !errors.Is(err, feedback.ErrConflict) {
		t.Fatal("stale version accepted", err)
	}
	e, err = s.Review(ctx, w, e.ID, feedback.Review{Version: e.Version, Status: "resolved", Outcome: &outcome})
	if err != nil || e.Version != 3 {
		t.Fatalf("resolution %+v %v", e, err)
	}
	e, err = s.Review(ctx, w, e.ID, feedback.Review{Version: e.Version, Status: "in_review"})
	if err != nil || e.Outcome != nil {
		t.Fatal("reopen outcome", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := s.Create(ctx, w, input); err != nil {
			t.Fatal(err)
		}
	}
	page, err = s.List(ctx, w.Actor, feedback.Query{Limit: 1, Category: "bug"})
	if err != nil || page.NextOffset == nil {
		t.Fatal("no next page", err)
	}
	next, err := s.List(ctx, w.Actor, feedback.Query{Limit: 1, Offset: *page.NextOffset, Category: "bug"})
	if err != nil || len(next.Items) != 1 || next.Items[0].ID == page.Items[0].ID {
		t.Fatal("unstable paging", err)
	}
	empty, err := s.List(ctx, w.Actor, feedback.Query{Limit: 20, Status: "resolved"})
	if err != nil || len(empty.Items) != 0 {
		t.Fatal("filters", err)
	}
	events, err := auditor.ListPlatform(ctx, audit.PlatformQuery{Resource: "feedback", Limit: 50})
	if err != nil || len(events) != 6 {
		t.Fatalf("audit %d %v", len(events), err)
	}
	for _, event := range events {
		if strings.Contains(string(event.BeforeState)+string(event.AfterState), "private-") {
			t.Fatal("audit copied raw feedback")
		}
	}
	// Enforce terminal-outcome consistency even for direct writes in the owner.
	if _, err := pool.Exec(ctx, `UPDATE wheretolive.feedback_intake SET status='resolved',outcome=NULL WHERE id=$1`, e.ID); err == nil {
		t.Fatal("database accepted resolution without result")
	}
	if _, err := pool.Exec(ctx, `UPDATE wheretolive.feedback_intake SET place_id='018bcfe5-6800-7000-8000-000000007199' WHERE id=$1`, e.ID); err == nil {
		t.Fatal("database accepted missing linked place")
	}
	if _, err := pool.Exec(ctx, `UPDATE wheretolive.feedback_categories SET active=false WHERE key='bug'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(ctx, w, input); !errors.Is(err, feedback.ErrInvalid) {
		t.Fatal("inactive category accepted", err)
	}
	if err := migrationtest.RollbackAfter(ctx, pool, 14); err != nil {
		t.Fatal(err)
	}

}
