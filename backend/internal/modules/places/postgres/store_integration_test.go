package postgres

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/places"
	"github.com/psmMRFP/WhereToLive/backend/internal/platform/database/migrationtest"
)

func integrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("WHERETOLIVE_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("WHERETOLIVE_TEST_DATABASE_URL is not set")
	}
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(config.ConnConfig.Database, "_test") {
		t.Fatal("refusing integration setup outside a dedicated _test database")
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	migrationtest.Prepare(t, pool)
	return pool
}

func TestPublicSearchAndHierarchy(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	now := time.Now().UTC().Add(-time.Hour)
	// Fixture geography only: it is never inserted into a production migration.
	insert := func(id, slug, name, kind, code string, parent *string, published *time.Time) error {
		_, err := pool.Exec(ctx, `INSERT INTO wheretolive.places (id,slug,name,normalized_name,type,country_code,parent_id,coverage_level,published_at,created_at,updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,1,$8,$9,$9)`, id, slug, name, places.NormalizeName(name), kind, code, parent, published, now)
		return err
	}
	germany := "018bcfe5-6800-7000-8000-000000001001"
	munich := "018bcfe5-6800-7000-8000-000000001002"
	unpublished := "018bcfe5-6800-7000-8000-000000001003"
	child := "018bcfe5-6800-7000-8000-000000001004"
	if err := insert(germany, "germany", "Germany", "country", "DE", nil, &now); err != nil {
		t.Fatal(err)
	}
	if err := insert(munich, "munich", "Munich", "city", "DE", &germany, &now); err != nil {
		t.Fatal(err)
	}
	if err := insert(unpublished, "private-region", "Private Region", "region", "DE", &germany, nil); err != nil {
		t.Fatal(err)
	}
	if err := insert(child, "hidden-city", "Hidden City", "city", "DE", &unpublished, &now); err != nil {
		t.Fatal(err)
	}
	for _, alias := range []struct{ locale, name string }{{"de", "München"}, {"zh-CN", "慕尼黑"}} {
		if _, err := pool.Exec(ctx, `INSERT INTO wheretolive.place_aliases (place_id,locale,name,normalized_name,preferred) VALUES ($1,$2,$3,$4,true)`, munich, alias.locale, alias.name, places.NormalizeName(alias.name)); err != nil {
			t.Fatal(err)
		}
	}
	service, _ := places.NewService(New(pool))
	for _, name := range []string{"Munich", "München", "慕尼黑", "Muni"} {
		page, err := service.Search(ctx, places.Query{Search: name, Locale: "zh-CN", Limit: 20})
		if err != nil || len(page.Items) != 1 || page.Items[0].Slug != "munich" || page.Items[0].DisplayName != "慕尼黑" {
			t.Fatalf("search %q: page=%+v err=%v", name, page, err)
		}
	}
	for _, slug := range []string{"private-region", "hidden-city", "missing"} {
		if _, err := service.Get(ctx, slug, "en"); !errors.Is(err, places.ErrNotFound) {
			t.Fatalf("draft/missing %s err=%v", slug, err)
		}
	}
	for _, q := range []string{"private", "hidden", "%", "_", "' OR true --"} {
		page, err := service.Search(ctx, places.Query{Search: q, Locale: "en", Limit: 20})
		if err != nil || len(page.Items) != 0 {
			t.Fatalf("private/wildcard search %q: %+v %v", q, page, err)
		}
	}
	fallback, err := service.Get(ctx, "munich", "fr")
	if err != nil || fallback.DisplayName != "Munich" || fallback.Timezone != nil || fallback.Currency != nil {
		t.Fatalf("fallback=%+v err=%v", fallback, err)
	}
	// Country mismatch and invalid parent types fail at the database boundary.
	if err := insert("018bcfe5-6800-7000-8000-000000001010", "wrong-country", "Wrong", "city", "FR", &germany, &now); err == nil {
		t.Fatal("cross-country parent accepted")
	}
	if err := insert("018bcfe5-6800-7000-8000-000000001011", "wrong-parent", "Wrong", "region", "DE", &munich, &now); err == nil {
		t.Fatal("region under city accepted")
	}
	if _, err := pool.Exec(ctx, `UPDATE wheretolive.places SET type='district',parent_id=$1 WHERE id=$2`, munich, germany); err == nil {
		t.Fatal("cycle accepted")
	}
	if _, err := pool.Exec(ctx, `UPDATE wheretolive.places SET type='island' WHERE id=$1`, munich); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE wheretolive.places SET type='district' WHERE id=$1`, munich); err == nil {
		t.Fatal("invalid parent type update accepted")
	}
	future := time.Now().UTC().Add(time.Hour)
	if _, err := pool.Exec(ctx, `UPDATE wheretolive.places SET published_at=$1 WHERE id=$2`, future, munich); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Get(ctx, "munich", "en"); !errors.Is(err, places.ErrNotFound) {
		t.Fatal("future publication exposed")
	}
	// Roll back later dependents before testing this owner's down migration.
	if err := migrationtest.RollbackAfter(ctx, pool, 11); err != nil {
		t.Fatal(err)
	}

}
