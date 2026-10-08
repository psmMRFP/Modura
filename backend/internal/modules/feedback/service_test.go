package feedback

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/audit"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/places"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/platformadmin"
)

type stubStore struct {
	entry Entry
	calls int
}

func (s *stubStore) Categories(context.Context) ([]Category, error) {
	s.calls++
	return []Category{{"bug", "Bug"}}, nil
}
func (s *stubStore) List(context.Context, Query) ([]Entry, error) {
	s.calls++
	return []Entry{s.entry}, nil
}
func (s *stubStore) Insert(_ context.Context, _ pgx.Tx, e Entry) (Entry, error) {
	s.calls++
	s.entry = e
	return e, nil
}
func (s *stubStore) Lock(context.Context, pgx.Tx, string) (Entry, error) {
	s.calls++
	return s.entry, nil
}
func (s *stubStore) Update(_ context.Context, _ pgx.Tx, e Entry) (Entry, error) {
	s.calls++
	e.Version++
	s.entry = e
	return e, nil
}

type stubTx struct{}

func (stubTx) WithinTransaction(_ context.Context, fn func(pgx.Tx) error) error { return fn(nil) }

type stubAudit struct{ events []audit.PlatformEvent }

func (s *stubAudit) RecordPlatformWrite(_ context.Context, _ pgx.Tx, e audit.PlatformEvent) error {
	s.events = append(s.events, e)
	return nil
}

type stubPlaces struct{}

func (stubPlaces) Get(context.Context, platformadmin.Actor, string) (places.Entry, error) {
	return places.Entry{}, places.ErrNotFound
}
func testService(t *testing.T) (*Service, *stubStore, *stubAudit, WriteContext) {
	t.Helper()
	store := &stubStore{}
	auditor := &stubAudit{}
	s, err := NewService(store, stubTx{}, auditor, stubPlaces{}, time.Now, func(time.Time) (string, error) { return "018bcfe5-6800-7000-8000-000000007001", nil })
	if err != nil {
		t.Fatal(err)
	}
	w := WriteContext{Actor: platformadmin.Actor{AdministratorID: "018bcfe5-6800-7000-8000-000000007002", SessionID: "018bcfe5-6800-7000-8000-000000007003"}, Reason: "manual intake", CorrelationID: "feedback-test"}
	return s, store, auditor, w
}
func TestInputAndAuthorizationFailBeforePersistence(t *testing.T) {
	s, store, _, w := testService(t)
	ctx := context.Background()
	if _, err := s.List(ctx, platformadmin.Actor{}, Query{Limit: 20}); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	if _, err := s.Categories(ctx, platformadmin.Actor{}); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	for _, input := range []Create{{Category: "bug", Title: "", Message: "body"}, {Category: "../bug", Title: "title", Message: "body"}, {Category: "bug", Title: "title", Message: strings.Repeat("x", 5001)}} {
		if _, err := s.Create(ctx, w, input); !errors.Is(err, ErrInvalid) {
			t.Fatal(err)
		}
	}
	unknown := "018bcfe5-6800-7000-8000-000000007004"
	if _, err := s.Create(ctx, w, Create{Category: "bug", Title: "title", Message: "body", PlaceID: &unknown}); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if store.calls != 0 {
		t.Fatalf("invalid or unauthorized input reached store %d", store.calls)
	}
}
func TestReviewOutcomeVersionAndRedactedAudit(t *testing.T) {
	s, _, auditor, w := testService(t)
	ctx := context.Background()
	e, err := s.Create(ctx, w, Create{Category: "bug", Title: "private-title", Message: "private-body"})
	if err != nil {
		t.Fatal(err)
	}
	outcome := "private-outcome"
	if _, err := s.Review(ctx, w, e.ID, Review{Version: 1, Status: "resolved", Outcome: &outcome}); !errors.Is(err, ErrConflict) {
		t.Fatal("resolved without review", err)
	}
	if _, err := s.Review(ctx, w, e.ID, Review{Version: 1, Status: "in_review", Outcome: &outcome}); !errors.Is(err, ErrInvalid) {
		t.Fatal("outcome on nonterminal", err)
	}
	e, err = s.Review(ctx, w, e.ID, Review{Version: 1, Status: "in_review"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Review(ctx, w, e.ID, Review{Version: 1, Status: "dismissed", Outcome: &outcome}); !errors.Is(err, ErrConflict) {
		t.Fatal("stale edit", err)
	}
	if _, err := s.Review(ctx, w, e.ID, Review{Version: e.Version, Status: "resolved"}); !errors.Is(err, ErrInvalid) {
		t.Fatal("no actual result", err)
	}
	e, err = s.Review(ctx, w, e.ID, Review{Version: e.Version, Status: "resolved", Outcome: &outcome})
	if err != nil {
		t.Fatal(err)
	}
	e, err = s.Review(ctx, w, e.ID, Review{Version: e.Version, Status: "in_review"})
	if err != nil || e.Outcome != nil || e.Category != "bug" || e.Message != "private-body" {
		t.Fatalf("reopen: %+v %v", e, err)
	}
	if len(auditor.events) != 4 {
		t.Fatalf("audit count %d", len(auditor.events))
	}
	for _, event := range auditor.events {
		if strings.Contains(string(event.AfterState)+string(event.BeforeState), "private-") {
			t.Fatal("private text copied into audit")
		}
	}
}
