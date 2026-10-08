package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/audit"
	auditpostgres "github.com/psmMRFP/WhereToLive/backend/internal/modules/audit/postgres"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/places"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/platformadmin"
	"github.com/psmMRFP/WhereToLive/backend/internal/platform/database"
	"github.com/psmMRFP/WhereToLive/backend/internal/platform/identifier"
)

type unavailableAudit struct{}

func (unavailableAudit) RecordPlatformWrite(context.Context, pgx.Tx, audit.PlatformEvent) error {
	return errors.New("audit unavailable")
}
func TestManagedPlacePublicationAndAuditAreAtomic(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	newID := func(now time.Time) (string, error) { id, err := identifier.NewUUIDv7(now, nil); return string(id), err }
	auditStore := auditpostgres.New(pool)
	auditor, err := audit.NewService(auditStore, newID)
	if err != nil {
		t.Fatal(err)
	}
	store := New(pool)
	management, err := places.NewManagement(store, database.NewTransactor(pool), auditor, time.Now, newID)
	if err != nil {
		t.Fatal(err)
	}
	write := places.WriteContext{Actor: platformadmin.Actor{AdministratorID: "018bcfe5-6800-7000-8000-000000001701", SessionID: "018bcfe5-6800-7000-8000-000000001702"}, Reason: "test catalogue", CorrelationID: "request-managed-places"}
	country, err := management.Create(ctx, write, places.Create{Slug: "germany", Type: "country", CountryCode: "DE", Details: places.Details{Name: "Germany"}})
	if err != nil {
		t.Fatal(err)
	}
	city, err := management.Create(ctx, write, places.Create{Slug: "munich", Type: "city", CountryCode: "DE", ParentID: &country.ID, Details: places.Details{Name: "Munich", Aliases: []places.Alias{{Locale: "de", Name: "München", Preferred: true}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := management.Publish(ctx, write, city.ID, 1, true); !errors.Is(err, places.ErrConflict) {
		t.Fatal("published child before parent")
	}
	country, err = management.Publish(ctx, write, country.ID, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	city, err = management.Publish(ctx, write, city.ID, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	public, _ := places.NewService(store)
	found, err := public.Get(ctx, "munich", "de")
	if err != nil || found.DisplayName != "München" {
		t.Fatalf("public place=%+v %v", found, err)
	}
	if _, err := management.Update(ctx, write, city.ID, 1, places.Details{Name: "stale"}); !errors.Is(err, places.ErrConflict) {
		t.Fatal("lost update accepted")
	}
	if _, err := management.Publish(ctx, write, country.ID, country.Version, false); err != nil {
		t.Fatal(err)
	}
	if _, err := public.Get(ctx, "munich", "de"); !errors.Is(err, places.ErrNotFound) {
		t.Fatal("child visible after parent withdrawal")
	}
	var events int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM wheretolive.audit_events WHERE resource='place' AND correlation_id=$1`, write.CorrelationID).Scan(&events); err != nil || events != 5 {
		t.Fatalf("audit events=%d err=%v", events, err)
	}
	rejecting, err := places.NewManagement(store, database.NewTransactor(pool), unavailableAudit{}, time.Now, newID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rejecting.Update(ctx, write, city.ID, city.Version, places.Details{Name: "must roll back"}); err == nil {
		t.Fatal("audit failure accepted")
	}
	unchanged, err := management.Get(ctx, write.Actor, city.ID)
	if err != nil || unchanged.Name != "Munich" || unchanged.Version != city.Version || len(unchanged.Aliases) != 1 {
		t.Fatalf("failed audit persisted metadata: %+v %v", unchanged, err)
	}
	if _, err := rejecting.Create(ctx, write, places.Create{Slug: "france", Type: "country", CountryCode: "FR", Details: places.Details{Name: "France"}}); err == nil {
		t.Fatal("audit failure created a place")
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM wheretolive.places WHERE slug='france'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("failed audit persisted draft")
	}
}

func TestCandidatePoolFiltersComposeAndPreservePublicationSemantics(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	now := time.Now().UTC().Add(-time.Hour)
	country := "018bcfe5-6800-7000-8000-000000002001"
	city := "018bcfe5-6800-7000-8000-000000002002"
	france := "018bcfe5-6800-7000-8000-000000002003"
	insert := func(id, slug, name, kind, code string, parent *string, coverage int, published *time.Time) {
		t.Helper()
		_, err := pool.Exec(ctx, `INSERT INTO wheretolive.places(id,slug,name,normalized_name,type,country_code,parent_id,coverage_level,published_at,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$10)`, id, slug, name, places.NormalizeName(name), kind, code, parent, coverage, published, now)
		if err != nil {
			t.Fatal(err)
		}
	}
	insert(country, "germany", "Germany", "country", "DE", nil, 0, nil)
	insert(city, "munich", "Munich", "city", "DE", &country, 1, &now)
	insert(france, "france", "France", "country", "FR", nil, 0, nil)
	if _, err := pool.Exec(ctx, `INSERT INTO wheretolive.place_aliases(place_id,locale,name,normalized_name,preferred) VALUES($1,'de','München','münchen',true)`, city); err != nil {
		t.Fatal(err)
	}
	store := New(pool)
	zero, one := 0, 1
	cases := []struct {
		query places.CatalogueQuery
		ids   []string
	}{
		{places.CatalogueQuery{}, []string{france, country, city}},
		{places.CatalogueQuery{CountryCode: "DE", CoverageLevel: &zero, Publication: "draft"}, []string{country}},
		{places.CatalogueQuery{CountryCode: "FR", CoverageLevel: &zero}, []string{france}},
		{places.CatalogueQuery{CountryCode: "FR", Query: places.Query{Search: "mun"}}, nil},
		{places.CatalogueQuery{CountryCode: "DE", CoverageLevel: &one, Publication: "published", Query: places.Query{Search: "mün"}}, []string{city}},
		{places.CatalogueQuery{Query: places.Query{Search: "%"}}, nil},
		{places.CatalogueQuery{Query: places.Query{Search: "_"}}, nil},
		{places.CatalogueQuery{Query: places.Query{Limit: 1, Offset: 1}}, []string{country}},
	}
	for _, tc := range cases {
		q := tc.query
		if q.Limit == 0 {
			q.Limit = 20
		}
		rows, err := store.ListManaged(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != len(tc.ids) {
			t.Fatalf("filter %+v: got %d want %d", q, len(rows), len(tc.ids))
		}
		for i, id := range tc.ids {
			if rows[i].ID != id {
				t.Fatalf("filter %+v: got %s want %s", q, rows[i].ID, id)
			}
		}
	}
	public, _ := places.NewService(store)
	if _, err := public.Get(ctx, "munich", "en"); !errors.Is(err, places.ErrNotFound) {
		t.Fatal("publication filter bypassed private ancestor")
	}
}
