package places

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/audit"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/platformadmin"
)

type managementStoreStub struct {
	entry Entry
	calls int
	err   error
}

func (s *managementStoreStub) ListManaged(context.Context, CatalogueQuery) ([]Entry, error) {
	s.calls++
	return []Entry{s.entry}, s.err
}
func (s *managementStoreStub) GetManaged(context.Context, string) (Entry, error) {
	s.calls++
	return s.entry, s.err
}
func (s *managementStoreStub) CreateManaged(_ context.Context, _ pgx.Tx, e Entry) (Entry, error) {
	s.calls++
	e.Version = 1
	s.entry = e
	return e, s.err
}
func (s *managementStoreStub) UpdateManaged(_ context.Context, _ pgx.Tx, _ string, v int64, d Details, now time.Time) (Entry, Entry, error) {
	s.calls++
	if s.err != nil {
		return Entry{}, Entry{}, s.err
	}
	if s.entry.Version != v {
		return Entry{}, Entry{}, ErrConflict
	}
	before := s.entry
	s.entry.Details = d
	s.entry.Version++
	s.entry.UpdatedAt = now
	return before, s.entry, nil
}
func (s *managementStoreStub) PublishManaged(_ context.Context, _ pgx.Tx, _ string, v int64, p bool, now time.Time) (Entry, Entry, error) {
	s.calls++
	if s.err != nil {
		return Entry{}, Entry{}, s.err
	}
	if s.entry.Version != v {
		return Entry{}, Entry{}, ErrConflict
	}
	before := s.entry
	if p {
		s.entry.PublishedAt = &now
	} else {
		s.entry.PublishedAt = nil
	}
	s.entry.Version++
	s.entry.UpdatedAt = now
	return before, s.entry, nil
}

type transactionStub struct{ commits int }

func (s *transactionStub) WithinTransaction(_ context.Context, work func(pgx.Tx) error) error {
	if err := work(nil); err != nil {
		return err
	}
	s.commits++
	return nil
}

type platformAuditStub struct {
	events []audit.PlatformEvent
	err    error
}

func (s *platformAuditStub) RecordPlatformWrite(_ context.Context, _ pgx.Tx, e audit.PlatformEvent) error {
	s.events = append(s.events, e)
	return s.err
}
func managementFixture(t *testing.T) (*Management, *managementStoreStub, *transactionStub, *platformAuditStub, WriteContext) {
	t.Helper()
	store := &managementStoreStub{}
	tx := &transactionStub{}
	a := &platformAuditStub{}
	s, err := NewManagement(store, tx, a, func() time.Time { return time.Unix(1700000000, 0).UTC() }, func(time.Time) (string, error) { return "018bcfe5-6800-7000-8000-000000001501", nil })
	if err != nil {
		t.Fatal(err)
	}
	w := WriteContext{Actor: platformadmin.Actor{AdministratorID: "018bcfe5-6800-7000-8000-000000001502", SessionID: "018bcfe5-6800-7000-8000-000000001503"}, Reason: "initial catalogue", CorrelationID: "request-places"}
	return s, store, tx, a, w
}
func TestManagementCreatesDraftAndAuditsChanges(t *testing.T) {
	s, store, tx, a, w := managementFixture(t)
	ctx := context.Background()
	input := Create{Slug: "germany", Type: "country", CountryCode: "DE", Details: Details{Name: " Germany ", Aliases: []Alias{{Locale: "zh-cn", Name: "德国", Preferred: true}}}}
	e, err := s.Create(ctx, w, input)
	if err != nil || e.PublishedAt != nil || e.Version != 1 || e.Name != "Germany" || e.Aliases[0].Locale != "zh-CN" || tx.commits != 1 || len(a.events) != 1 {
		t.Fatalf("create=%+v commits=%d err=%v", e, tx.commits, err)
	}
	e, err = s.Publish(ctx, w, e.ID, e.Version, true)
	if err != nil || e.PublishedAt == nil || e.Version != 2 {
		t.Fatalf("publish=%+v %v", e, err)
	}
	if _, err = s.Update(ctx, w, e.ID, 1, Details{Name: "Deutschland"}); !errors.Is(err, ErrConflict) {
		t.Fatal("stale write accepted")
	}
	if len(a.events) != 2 {
		t.Fatal("failed write produced successful audit")
	}
	if _, err = s.Update(ctx, w, e.ID, 2, Details{Name: "Deutschland"}); err != nil {
		t.Fatal(err)
	}
	if store.entry.Slug != "germany" || a.events[2].ResourceID != e.ID || a.events[2].ActorID != string(w.Actor.AdministratorID) || a.events[2].CorrelationID != "request-places" || len(a.events[2].BeforeState) == 0 {
		t.Fatal("stable identity or audit context lost")
	}
}
func TestManagementFailsClosedWithoutActorOrAudit(t *testing.T) {
	s, store, tx, a, w := managementFixture(t)
	if _, err := s.List(context.Background(), platformadmin.Actor{}, CatalogueQuery{Query: Query{Limit: 20}}); !errors.Is(err, ErrDenied) || store.calls != 0 {
		t.Fatal("missing actor reached store")
	}
	w.Reason = ""
	if _, err := s.Create(context.Background(), w, Create{}); !errors.Is(err, ErrInvalidPlace) || store.calls != 0 {
		t.Fatal("missing reason reached store")
	}
	w.Reason = "create"
	a.err = errors.New("audit unavailable")
	if _, err := s.Create(context.Background(), w, Create{Slug: "germany", Type: "country", CountryCode: "DE", Details: Details{Name: "Germany"}}); !errors.Is(err, a.err) || tx.commits != 0 {
		t.Fatal("audit failure committed")
	}
}
func TestInvalidPlaceMetadata(t *testing.T) {
	zone := "Local"
	badCurrency := "zz!"
	lat := 91.0
	lon := 0.0
	for _, details := range []Details{{Name: ""}, {Name: "City", Timezone: &zone}, {Name: "City", Currency: &badCurrency}, {Name: "City", Latitude: &lat, Longitude: &lon}, {Name: "City", Latitude: &lon}, {Name: "City", Languages: []string{"not_a_locale"}}, {Name: "City", Aliases: []Alias{{Locale: "en", Name: "City", Preferred: true}, {Locale: "en", Name: "Town", Preferred: true}}}, {Name: "City", Aliases: []Alias{{Locale: "en", Name: " ＣＩＴＹ "}, {Locale: "en", Name: "city"}}}} {
		if _, err := normalizeDetails(details); !errors.Is(err, ErrInvalidPlace) {
			t.Fatalf("accepted invalid details: %+v", details)
		}
	}
	s, store, _, _, w := managementFixture(t)
	for _, input := range []Create{{Slug: "germany", Type: "country", CountryCode: "ZZ", Details: Details{Name: "Germany"}}, {Slug: "munich", Type: "city", CountryCode: "DE", Details: Details{Name: "Munich"}}} {
		if _, err := s.Create(context.Background(), w, input); !errors.Is(err, ErrInvalidPlace) || store.calls != 0 {
			t.Fatalf("accepted invalid geography: %+v", input)
		}
	}
}

func TestCatalogueFiltersRejectInvalidInputBeforePersistence(t *testing.T) {
	s, store, _, _, write := managementFixture(t)
	low, high := -1, 4
	for _, query := range []CatalogueQuery{
		{Query: Query{Limit: 20}, CountryCode: "de"},
		{Query: Query{Limit: 20}, CountryCode: "ZZ"},
		{Query: Query{Limit: 20}, CoverageLevel: &low},
		{Query: Query{Limit: 20}, CoverageLevel: &high},
		{Query: Query{Limit: 20}, Publication: "visible"},
	} {
		if _, err := s.List(context.Background(), write.Actor, query); !errors.Is(err, ErrInvalidQuery) {
			t.Fatalf("accepted invalid filter: %+v: %v", query, err)
		}
	}
	if store.calls != 0 {
		t.Fatal("invalid filters reached persistence")
	}
	zero := 0
	if _, err := s.List(context.Background(), write.Actor, CatalogueQuery{Query: Query{Limit: 20}, CountryCode: "DE", CoverageLevel: &zero, Publication: "draft"}); err != nil {
		t.Fatal(err)
	}
}
