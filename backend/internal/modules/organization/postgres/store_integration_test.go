package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modura-dev/modura/backend/internal/modules/audit"
	auditpostgres "github.com/modura-dev/modura/backend/internal/modules/audit/postgres"
	"github.com/modura-dev/modura/backend/internal/modules/identity"
	"github.com/modura-dev/modura/backend/internal/modules/organization"
	"github.com/modura-dev/modura/backend/internal/platform/database"
	"github.com/modura-dev/modura/backend/internal/platform/database/migrationtest"
)

func TestOrganizationTenantAndTreeInvariants(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	now := time.Unix(1_700_000_000, 0).UTC()
	seedIdentity(t, pool, now)
	store := New(pool)
	alpha := identity.TenantID("018bcfe5-6800-7000-8000-000000000101")
	beta := identity.TenantID("018bcfe5-6800-7000-8000-000000000102")
	alphaScope := organization.DataScope{ActorID: "018bcfe5-6800-7000-8000-000000000131", All: true}
	betaScope := organization.DataScope{ActorID: "018bcfe5-6800-7000-8000-000000000132", All: true}
	alphaRoot := department("018bcfe5-6800-7000-8000-000000000111", alpha, nil, "Alpha Root", now)
	betaRoot := department("018bcfe5-6800-7000-8000-000000000112", beta, nil, "Beta Root", now)
	if err := within(ctx, pool, func(tx pgx.Tx) error { return store.CreateDepartment(ctx, tx, alphaRoot, alphaScope) }); err != nil {
		t.Fatal(err)
	}
	if err := within(ctx, pool, func(tx pgx.Tx) error { return store.CreateDepartment(ctx, tx, betaRoot, betaScope) }); err != nil {
		t.Fatal(err)
	}
	if err := within(ctx, pool, func(tx pgx.Tx) error {
		return store.CreateDepartment(ctx, tx, department("018bcfe5-6800-7000-8000-000000000113", alpha, nil, "Another Root", now), alphaScope)
	}); err == nil {
		t.Fatal("second tenant root succeeded")
	}
	childID := organization.DepartmentID("018bcfe5-6800-7000-8000-000000000114")
	grandchildID := organization.DepartmentID("018bcfe5-6800-7000-8000-000000000115")
	if err := within(ctx, pool, func(tx pgx.Tx) error {
		return store.CreateDepartment(ctx, tx, department(string(childID), alpha, &alphaRoot.ID, "Engineering", now), alphaScope)
	}); err != nil {
		t.Fatal(err)
	}
	if err := within(ctx, pool, func(tx pgx.Tx) error {
		return store.CreateDepartment(ctx, tx, department(string(grandchildID), alpha, &childID, "Platform", now), alphaScope)
	}); err != nil {
		t.Fatal(err)
	}
	if err := within(ctx, pool, func(tx pgx.Tx) error {
		return store.CreateDepartment(ctx, tx, department("018bcfe5-6800-7000-8000-000000000116", alpha, &alphaRoot.ID, " engineering ", now), alphaScope)
	}); err == nil {
		t.Fatal("duplicate normalized sibling succeeded")
	}
	if err := within(ctx, pool, func(tx pgx.Tx) error {
		return store.CreateDepartment(ctx, tx, department("018bcfe5-6800-7000-8000-000000000117", alpha, &betaRoot.ID, "Cross Tenant", now), alphaScope)
	}); err == nil {
		t.Fatal("cross-tenant parent succeeded")
	}
	if err := within(ctx, pool, func(tx pgx.Tx) error {
		return store.MoveDepartment(ctx, tx, alpha, childID, grandchildID, alphaScope, now)
	}); !errors.Is(err, organization.ErrCycle) {
		t.Fatalf("cycle move error = %v", err)
	}
	if err := within(ctx, pool, func(tx pgx.Tx) error {
		return store.MoveDepartment(ctx, tx, alpha, childID, betaRoot.ID, alphaScope, now)
	}); !errors.Is(err, organization.ErrNotFound) {
		t.Fatalf("cross-tenant move error = %v", err)
	}
	if err := within(ctx, pool, func(tx pgx.Tx) error { return store.DeleteDepartment(ctx, tx, alpha, alphaRoot.ID, alphaScope) }); !errors.Is(err, organization.ErrRootDepartment) {
		t.Fatalf("root delete error = %v", err)
	}
	if err := within(ctx, pool, func(tx pgx.Tx) error { return store.DeleteDepartment(ctx, tx, alpha, childID, alphaScope) }); !errors.Is(err, organization.ErrInUse) {
		t.Fatalf("parent delete error = %v", err)
	}

	position := organization.Position{ID: "018bcfe5-6800-7000-8000-000000000121", TenantID: alpha, Name: "Engineer", NormalizedName: "engineer", CreatedAt: now}
	if err := within(ctx, pool, func(tx pgx.Tx) error { return store.CreatePosition(ctx, tx, position) }); err != nil {
		t.Fatal(err)
	}
	betaPosition := organization.Position{ID: "018bcfe5-6800-7000-8000-000000000122", TenantID: beta, Name: "Engineer", NormalizedName: "engineer", CreatedAt: now}
	if err := within(ctx, pool, func(tx pgx.Tx) error { return store.CreatePosition(ctx, tx, betaPosition) }); err != nil {
		t.Fatal(err)
	}
	if err := within(ctx, pool, func(tx pgx.Tx) error {
		return store.AssignUser(ctx, tx, alpha, "018bcfe5-6800-7000-8000-000000000131", childID, &position.ID, alphaScope, now)
	}); err != nil {
		t.Fatal(err)
	}
	if err := within(ctx, pool, func(tx pgx.Tx) error {
		return store.AssignUser(ctx, tx, alpha, "018bcfe5-6800-7000-8000-000000000131", childID, &betaPosition.ID, alphaScope, now)
	}); err == nil {
		t.Fatal("cross-tenant position assignment succeeded")
	}
	if err := within(ctx, pool, func(tx pgx.Tx) error {
		return store.AssignUser(ctx, tx, beta, "018bcfe5-6800-7000-8000-000000000132", childID, nil, betaScope, now)
	}); err == nil {
		t.Fatal("cross-tenant department assignment succeeded")
	}
	departmentOnly, err := store.ListDepartments(ctx, alpha, organization.DataScope{ActorID: "018bcfe5-6800-7000-8000-000000000131", Department: true})
	if err != nil || len(departmentOnly) != 1 || departmentOnly[0].ID != childID {
		t.Fatalf("department scope=%+v err=%v", departmentOnly, err)
	}
	selfOnly, err := store.ListDepartments(ctx, alpha, organization.DataScope{ActorID: "018bcfe5-6800-7000-8000-000000000131", Self: true})
	if err != nil || len(selfOnly) != 1 || selfOnly[0].ID != childID {
		t.Fatalf("self scope=%+v err=%v", selfOnly, err)
	}
	descendants, err := store.ListDepartments(ctx, alpha, organization.DataScope{ActorID: "018bcfe5-6800-7000-8000-000000000131", DepartmentAndDescendants: true})
	if err != nil || len(descendants) != 2 {
		t.Fatalf("descendant scope=%+v err=%v", descendants, err)
	}
	custom, err := store.ListDepartments(ctx, alpha, organization.DataScope{ActorID: "018bcfe5-6800-7000-8000-000000000131", CustomDepartmentIDs: []organization.DepartmentID{alphaRoot.ID, betaRoot.ID}})
	if err != nil || len(custom) != 1 || custom[0].ID != alphaRoot.ID {
		t.Fatalf("custom tenant scope=%+v err=%v", custom, err)
	}
	departmentScope := organization.DataScope{ActorID: "018bcfe5-6800-7000-8000-000000000131", Department: true}
	if err := within(ctx, pool, func(tx pgx.Tx) error {
		return store.CreateDepartment(ctx, tx, department("018bcfe5-6800-7000-8000-000000000118", alpha, &alphaRoot.ID, "Out of Scope", now), departmentScope)
	}); !errors.Is(err, organization.ErrNotFound) {
		t.Fatalf("out-of-scope create error=%v", err)
	}
	if err := within(ctx, pool, func(tx pgx.Tx) error { return store.DeleteDepartment(ctx, tx, alpha, grandchildID, alphaScope) }); err != nil {
		t.Fatal(err)
	}
	departments, err := store.ListDepartments(ctx, alpha, organization.DataScope{ActorID: "018bcfe5-6800-7000-8000-000000000131", All: true})
	if err != nil || len(departments) != 2 {
		t.Fatalf("alpha departments=%+v err=%v", departments, err)
	}
	positions, err := store.ListPositions(ctx, alpha)
	if err != nil || len(positions) != 1 || positions[0].ID != position.ID {
		t.Fatalf("alpha positions=%+v err=%v", positions, err)
	}
}

func TestOrganizationWritesAndAuditAreAtomic(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	now := time.Unix(1_700_000_000, 0).UTC()
	seedIdentity(t, pool, now)
	store := New(pool)
	tenantID := identity.TenantID("018bcfe5-6800-7000-8000-000000000101")
	allScope := organization.DataScope{ActorID: "018bcfe5-6800-7000-8000-000000000131", All: true}
	root := department("018bcfe5-6800-7000-8000-000000000111", tenantID, nil, "Alpha Root", now)
	if err := within(ctx, pool, func(tx pgx.Tx) error { return store.CreateDepartment(ctx, tx, root, allScope) }); err != nil {
		t.Fatal(err)
	}
	auditor, err := audit.NewService(auditpostgres.New(), sequenceIDs())
	if err != nil {
		t.Fatal(err)
	}
	service, err := organization.NewService(store, database.NewTransactor(pool), auditor, func() time.Time { return now }, sequenceIDs())
	if err != nil {
		t.Fatal(err)
	}
	write := organization.WriteContext{Actor: identity.Actor{TenantID: tenantID, UserID: "018bcfe5-6800-7000-8000-000000000131", SessionID: "018bcfe5-6800-7000-8000-000000000141"}, CorrelationID: "organization-request-1", Scope: allScope}
	createdID, err := service.CreateDepartment(ctx, write, &root.ID, "Engineering", 10)
	if err != nil {
		t.Fatal(err)
	}
	var action, actorID, correlationID string
	if err := pool.QueryRow(ctx, `SELECT action, actor_id, correlation_id FROM modura.audit_events WHERE tenant_id = $1 AND resource_id = $2`, tenantID, createdID).Scan(&action, &actorID, &correlationID); err != nil {
		t.Fatal(err)
	}
	if action != "organization.department.created" || actorID != string(write.Actor.UserID) || correlationID != write.CorrelationID {
		t.Fatalf("audit action=%q actor=%q correlation=%q", action, actorID, correlationID)
	}
	failing, err := organization.NewService(store, database.NewTransactor(pool), failingAuditor{}, func() time.Time { return now }, sequenceIDs())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := failing.CreatePosition(ctx, write, "Must Roll Back"); err == nil {
		t.Fatal("organization write succeeded without audit")
	}
	var positions int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM modura.positions WHERE tenant_id = $1`, tenantID).Scan(&positions); err != nil {
		t.Fatal(err)
	}
	if positions != 0 {
		t.Fatalf("positions after audit failure = %d", positions)
	}
}

type failingAuditor struct{}

func (failingAuditor) RecordTenantWrite(context.Context, pgx.Tx, audit.Event) error {
	return errors.New("audit unavailable")
}

func sequenceIDs() func(time.Time) (string, error) {
	sequence := 500
	return func(time.Time) (string, error) {
		sequence++
		return fmt.Sprintf("018bcfe5-6800-7000-8000-%012d", sequence), nil
	}
}

func within(ctx context.Context, pool *pgxpool.Pool, work func(pgx.Tx) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := work(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func department(id string, tenantID identity.TenantID, parentID *organization.DepartmentID, name string, now time.Time) organization.Department {
	return organization.Department{ID: organization.DepartmentID(id), TenantID: tenantID, ParentID: parentID, Name: strings.TrimSpace(name), NormalizedName: organization.NormalizeName(name), CreatedAt: now}
}

func seedIdentity(t *testing.T, pool *pgxpool.Pool, now time.Time) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
INSERT INTO modura.tenants (id, slug, display_name, status, created_at, updated_at) VALUES
('018bcfe5-6800-7000-8000-000000000101', 'org-alpha', 'Alpha', 'active', $1, $1),
('018bcfe5-6800-7000-8000-000000000102', 'org-beta', 'Beta', 'active', $1, $1)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `
INSERT INTO modura.users (id, tenant_id, username, normalized_username, password_hash, status, created_at, updated_at) VALUES
('018bcfe5-6800-7000-8000-000000000131', '018bcfe5-6800-7000-8000-000000000101', 'alpha-user', 'alpha-user', 'hash', 'active', $1, $1),
('018bcfe5-6800-7000-8000-000000000132', '018bcfe5-6800-7000-8000-000000000102', 'beta-user', 'beta-user', 'hash', 'active', $1, $1)`, now); err != nil {
		t.Fatal(err)
	}
}

func integrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("MODURA_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("MODURA_TEST_DATABASE_URL is not set")
	}
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(config.ConnConfig.Database, "_test") {
		t.Fatalf("refusing destructive integration setup for database %q: name must end in _test", config.ConnConfig.Database)
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	migrationtest.Prepare(t, pool)
	return pool
}
