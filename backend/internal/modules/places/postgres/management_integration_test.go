package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/modura-dev/modura/backend/internal/modules/audit"
	auditpostgres "github.com/modura-dev/modura/backend/internal/modules/audit/postgres"
	"github.com/modura-dev/modura/backend/internal/modules/places"
	"github.com/modura-dev/modura/backend/internal/modules/platformadmin"
	"github.com/modura-dev/modura/backend/internal/platform/database"
	"github.com/modura-dev/modura/backend/internal/platform/identifier"
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
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM modura.audit_events WHERE resource='place' AND correlation_id=$1`, write.CorrelationID).Scan(&events); err != nil || events != 5 {
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
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM modura.places WHERE slug='france'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("failed audit persisted draft")
	}
}
