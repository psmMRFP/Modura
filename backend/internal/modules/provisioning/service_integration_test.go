package provisioning

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modura-dev/modura/backend/internal/modules/audit"
	auditpostgres "github.com/modura-dev/modura/backend/internal/modules/audit/postgres"
	"github.com/modura-dev/modura/backend/internal/modules/authorization"
	authorizationpostgres "github.com/modura-dev/modura/backend/internal/modules/authorization/postgres"
	"github.com/modura-dev/modura/backend/internal/modules/identity"
	identitypostgres "github.com/modura-dev/modura/backend/internal/modules/identity/postgres"
	"github.com/modura-dev/modura/backend/internal/modules/organization"
	organizationpostgres "github.com/modura-dev/modura/backend/internal/modules/organization/postgres"
	"github.com/modura-dev/modura/backend/internal/modules/platformadmin"
	"github.com/modura-dev/modura/backend/internal/modules/settings"
	settingspostgres "github.com/modura-dev/modura/backend/internal/modules/settings/postgres"
	"github.com/modura-dev/modura/backend/internal/platform/database"
	"github.com/modura-dev/modura/backend/internal/platform/database/migrationtest"
)

func TestProvisionIsAtomicAndIdempotent(t *testing.T) {
	pool := integrationPool(t)
	now := time.Unix(1_700_000_000, 0).UTC()
	key := []byte(strings.Repeat("k", 32))
	signer, err := identity.NewAccessTokenSigner("modura", "admin", "key", key, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	identityService, err := identity.NewService(identitypostgres.New(pool), signer, identity.NewAccessTokenVerifier("modura", "admin", map[string][]byte{"key": key}, 0), identity.DefaultPasswordParameters(), time.Hour, func() time.Time { return now }, sequentialIDs(), func() (string, error) { return strings.Repeat("i", 43), nil })
	if err != nil {
		t.Fatal(err)
	}
	auditService, err := audit.NewService(auditpostgres.New(), auditIDs())
	if err != nil {
		t.Fatal(err)
	}
	organizationService, err := organization.NewService(organizationpostgres.New(pool), database.NewTransactor(pool), auditService, func() time.Time { return now }, sequentialIDs())
	if err != nil {
		t.Fatal(err)
	}
	authorizationService, err := authorization.NewService(authorizationpostgres.New(pool))
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(pool, identityService, organizationService, authorizationService, auditService, func() time.Time { return now }, sequentialIDs(), func() (string, error) { return strings.Repeat("v", 43), nil }, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	request := Request{IdempotencyKey: "018bcfe5-6800-7000-8000-000000000301", Slug: " Acme ", DisplayName: "Acme", RootDepartmentName: "Acme Root", AdministratorUsername: "Admin", AdministratorEmail: "admin@example.com", Actor: platformadmin.Actor{AdministratorID: "018bcfe5-6800-7000-8000-000000000390", SessionID: "018bcfe5-6800-7000-8000-000000000391"}, Reason: "customer onboarding", CorrelationID: "request-provision-1"}
	first, err := service.Provision(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Created || first.TenantID == "" || first.AdministratorID == "" || first.InvitationToken == "" {
		t.Fatalf("first result = %+v", first)
	}
	second, err := service.Provision(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if second.Created || second.TenantID != first.TenantID || second.InvitationToken != "" {
		t.Fatalf("second result = %+v", second)
	}
	conflict := request
	conflict.DisplayName = "Different"
	if _, err := service.Provision(context.Background(), conflict); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("conflict error = %v", err)
	}
	assertProvisionedGraph(t, pool, first)
	administrator := identity.Actor{TenantID: first.TenantID, UserID: first.AdministratorID, SessionID: "verified-session"}
	if err := authorizationService.Authorize(context.Background(), administrator, authorization.Permission{Resource: authorization.ResourceDepartments, Action: authorization.ActionDelete}); err != nil {
		t.Fatalf("tenant administrator permission denied: %v", err)
	}
	if err := authorizationService.Authorize(context.Background(), administrator, authorization.Permission{Resource: authorization.ResourcePositions, Action: authorization.ActionDelete}); !errors.Is(err, authorization.ErrDenied) {
		t.Fatalf("unregistered destructive permission error = %v", err)
	}
	if err := authorizationService.EnableManagement(authorizationpostgres.New(pool), database.NewTransactor(pool), auditService, identityService, func() time.Time { return now }, managementIDs()); err != nil {
		t.Fatal(err)
	}
	write := authorization.WriteContext{Actor: administrator, CorrelationID: "request-authorization-1"}
	role, err := authorizationService.CreateRole(context.Background(), write, "department-reader", "Department Reader")
	if err != nil {
		t.Fatal(err)
	}
	policy := authorization.Policy{Permission: authorization.Permission{Resource: authorization.ResourceDepartments, Action: authorization.ActionRead}, Scope: authorization.DataScopeDepartmentDescendants}
	version, err := authorizationService.ReplaceRolePolicies(context.Background(), write, role.ID, role.Version, []authorization.Policy{policy})
	if err != nil || version != 2 {
		t.Fatalf("replace policies version=%d err=%v", version, err)
	}
	grants, err := authorizationService.GetUserRoleGrants(context.Background(), administrator, administrator.UserID)
	if err != nil || grants.Version != 1 || len(grants.RoleIDs) != 0 {
		t.Fatalf("initial non-reserved grants=%+v err=%v", grants, err)
	}
	grants, err = authorizationService.ReplaceUserRoleGrants(context.Background(), write, administrator.UserID, grants.Version, []authorization.RoleID{role.ID})
	if err != nil || grants.Version != 2 || len(grants.RoleIDs) != 1 || grants.RoleIDs[0] != role.ID {
		t.Fatalf("replaced grants=%+v err=%v", grants, err)
	}
	if _, err := authorizationService.ReplaceUserRoleGrants(context.Background(), write, administrator.UserID, 1, nil); !errors.Is(err, authorization.ErrConflict) {
		t.Fatalf("stale grant error=%v", err)
	}
	var stateAudits int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM modura.audit_events WHERE tenant_id = $1 AND action LIKE 'authorization.%' AND after_state IS NOT NULL`, first.TenantID).Scan(&stateAudits); err != nil || stateAudits != 3 {
		t.Fatalf("authorization state audits=%d err=%v", stateAudits, err)
	}
	nonAdministrator := identity.Actor{TenantID: first.TenantID, UserID: "018bcfe5-6800-7000-8000-000000000999", SessionID: "verified-session"}
	if err := authorizationService.Authorize(context.Background(), nonAdministrator, authorization.Permission{Resource: authorization.ResourceDepartments, Action: authorization.ActionRead}); !errors.Is(err, authorization.ErrDenied) {
		t.Fatalf("non-administrator permission error = %v", err)
	}
	seedGlobalSettings(t, pool, now)
	settingsService, err := settings.NewService(settingspostgres.New(pool), database.NewTransactor(pool), auditService, func() time.Time { return now }, settingsIDs())
	if err != nil {
		t.Fatal(err)
	}
	platformWrite := settings.PlatformWriteContext{Actor: request.Actor, Reason: "standardize global settings", CorrelationID: "request-platform-settings-1"}
	globalDictionary, err := settingsService.ReplaceGlobalDictionary(context.Background(), platformWrite, "priority", "Priority", 0, []settings.DictionaryItem{{Code: "high", Label: "High", Enabled: true}})
	if err != nil || globalDictionary.Source != "global" || globalDictionary.Version != 1 {
		t.Fatalf("global dictionary=%+v err=%v", globalDictionary, err)
	}
	if _, err := settingsService.ReplaceGlobalDictionary(context.Background(), platformWrite, "priority", "Stale", 2, nil); !errors.Is(err, settings.ErrConflict) {
		t.Fatalf("stale global dictionary error=%v", err)
	}
	globalConfiguration, err := settingsService.PutGlobalConfiguration(context.Background(), platformWrite, "feature.preview", "Preview Features", "boolean", true, 0, []byte("false"))
	if err != nil || globalConfiguration.Source != "global" || globalConfiguration.Version != 1 {
		t.Fatalf("global configuration=%+v err=%v", globalConfiguration, err)
	}
	if _, err := settingsService.PutGlobalConfiguration(context.Background(), platformWrite, "feature.preview", "Preview Features", "boolean", true, 2, []byte("true")); !errors.Is(err, settings.ErrConflict) {
		t.Fatalf("stale global configuration error=%v", err)
	}
	var platformSettingsAudits int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM modura.audit_events WHERE tenant_id IS NULL AND actor_type = 'platform_administrator' AND action LIKE 'settings.global_%'`).Scan(&platformSettingsAudits); err != nil || platformSettingsAudits != 2 {
		t.Fatalf("platform settings audits=%d err=%v", platformSettingsAudits, err)
	}
	dictionaries, err := settingsService.ListDictionaries(context.Background(), administrator)
	if err != nil || len(dictionaries) != 2 || dictionaries[0].Source != "global" || dictionaries[0].Items[0].Code != "enabled" {
		t.Fatalf("global dictionary fallback=%+v err=%v", dictionaries, err)
	}
	settingsWrite := settings.WriteContext{Actor: administrator, CorrelationID: "request-settings-1"}
	dictionary, err := settingsService.ReplaceDictionary(context.Background(), settingsWrite, "account_status", "Tenant Account Status", 0, []settings.DictionaryItem{{Code: "active", Label: "Active", Enabled: true}})
	if err != nil || dictionary.Source != "tenant" || dictionary.Version != 1 {
		t.Fatalf("tenant dictionary=%+v err=%v", dictionary, err)
	}
	if _, err := settingsService.ReplaceDictionary(context.Background(), settingsWrite, "account_status", "Stale", 2, nil); !errors.Is(err, settings.ErrConflict) {
		t.Fatalf("stale dictionary error=%v", err)
	}
	configurations, err := settingsService.ListConfigurations(context.Background(), administrator)
	if err != nil || len(configurations) != 2 || configurations[1].Key != "ui.compact" || configurations[1].Source != "global" || string(configurations[1].Value) != "false" {
		t.Fatalf("global configuration=%+v err=%v", configurations, err)
	}
	configuration, err := settingsService.PutConfiguration(context.Background(), settingsWrite, "ui.compact", 0, []byte("true"))
	if err != nil || configuration.Source != "tenant" || configuration.Version != 1 || string(configuration.Value) != "true" {
		t.Fatalf("tenant configuration=%+v err=%v", configuration, err)
	}
	if _, err := settingsService.PutConfiguration(context.Background(), settingsWrite, "ui.compact", 1, []byte(`"not-boolean"`)); err == nil {
		t.Fatal("configuration type mismatch succeeded")
	}
	var settingsAudits int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM modura.audit_events WHERE tenant_id = $1 AND action LIKE 'settings.%' AND after_state IS NOT NULL`, first.TenantID).Scan(&settingsAudits); err != nil || settingsAudits != 2 {
		t.Fatalf("settings audits=%d err=%v", settingsAudits, err)
	}
	if err := auditService.EnableQueries(auditpostgres.New(pool)); err != nil {
		t.Fatal(err)
	}
	auditRecords, err := auditService.List(context.Background(), first.TenantID, "", "", 100, 0)
	if err != nil || len(auditRecords) < 6 {
		t.Fatalf("tenant audit records=%d err=%v", len(auditRecords), err)
	}
	for _, record := range auditRecords {
		if record.TenantID != first.TenantID {
			t.Fatalf("cross-tenant audit record=%+v", record)
		}
	}

	duplicate := request
	duplicate.IdempotencyKey = "018bcfe5-6800-7000-8000-000000000302"
	if _, err := service.Provision(context.Background(), duplicate); err == nil {
		t.Fatal("duplicate tenant slug provisioning succeeded")
	}
	var tenants, audits int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM modura.tenants`).Scan(&tenants); err != nil {
		t.Fatal(err)
	}
	if tenants != 1 {
		t.Fatalf("tenant count after rollback = %d", tenants)
	}
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM modura.audit_events WHERE tenant_id = $1 AND action = 'tenant.provisioned'`, first.TenantID).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 1 {
		t.Fatalf("provisioning audit count = %d", audits)
	}
}

func assertProvisionedGraph(t *testing.T, pool *pgxpool.Pool, result Result) {
	t.Helper()
	var status string
	var departments, roles, grants, assignments, invitations int
	query := `SELECT t.status,
    (SELECT count(*) FROM modura.departments d WHERE d.tenant_id = t.id),
    (SELECT count(*) FROM modura.roles r WHERE r.tenant_id = t.id AND r.code = 'tenant-admin' AND r.reserved),
    (SELECT count(*) FROM modura.user_roles ur WHERE ur.tenant_id = t.id),
    (SELECT count(*) FROM modura.user_organization uo WHERE uo.tenant_id = t.id),
    (SELECT count(*) FROM modura.auth_one_time_tokens tok WHERE tok.tenant_id = t.id AND tok.purpose = 'invitation')
FROM modura.tenants t WHERE t.id = $1`
	if err := pool.QueryRow(context.Background(), query, result.TenantID).Scan(&status, &departments, &roles, &grants, &assignments, &invitations); err != nil {
		t.Fatal(err)
	}
	if status != "active" || departments != 1 || roles != 1 || grants != 1 || assignments != 1 || invitations != 1 {
		t.Fatalf("status=%s departments=%d roles=%d grants=%d assignments=%d invitations=%d", status, departments, roles, grants, assignments, invitations)
	}
}

func sequentialIDs() func(time.Time) (string, error) {
	sequence := 0
	return func(time.Time) (string, error) {
		sequence++
		return fmt.Sprintf("018bcfe5-6800-7000-8000-%012d", 400+sequence), nil
	}
}

func managementIDs() func(time.Time) (string, error) {
	sequence := 0
	return func(time.Time) (string, error) {
		sequence++
		return fmt.Sprintf("018bcfe5-6800-7000-9000-%012d", 800+sequence), nil
	}
}

func settingsIDs() func(time.Time) (string, error) {
	sequence := 0
	return func(time.Time) (string, error) {
		sequence++
		return fmt.Sprintf("018bcfe5-6800-7000-a000-%012d", 900+sequence), nil
	}
}

func auditIDs() func(time.Time) (string, error) {
	sequence := 0
	return func(time.Time) (string, error) {
		sequence++
		return fmt.Sprintf("018bcfe5-6800-7000-c000-%012d", 1000+sequence), nil
	}
}

func seedGlobalSettings(t *testing.T, pool *pgxpool.Pool, now time.Time) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO modura.global_dictionary_types (id, code, name, version, created_at, updated_at) VALUES ('018bcfe5-6800-7000-b000-000000000001', 'account_status', 'Account Status', 1, $1, $1)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO modura.global_dictionary_items (id, dictionary_type_id, code, label, sort_order, enabled, created_at, updated_at) VALUES ('018bcfe5-6800-7000-b000-000000000002', '018bcfe5-6800-7000-b000-000000000001', 'enabled', 'Enabled', 10, true, $1, $1)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO modura.configuration_definitions (id, key, name, value_type, tenant_overridable, created_at, updated_at) VALUES ('018bcfe5-6800-7000-b000-000000000003', 'ui.compact', 'Compact UI', 'boolean', true, $1, $1)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO modura.global_configuration_values (key, value, version, created_at, updated_at) VALUES ('ui.compact', 'false', 1, $1, $1)`, now); err != nil {
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
