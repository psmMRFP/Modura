// Command modura-e2e-seed prepares a *_test database for the browser E2E
// suite: it resets the schema, applies every migration, provisions a tenant
// through the real runtime workflow, and prints the credentials it created.
// The passwords are supplied by the caller through environment variables, so
// no credential material lives in source. It is test tooling and must never
// run against a shared or production database.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
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
	platformadminpostgres "github.com/modura-dev/modura/backend/internal/modules/platformadmin/postgres"
	"github.com/modura-dev/modura/backend/internal/modules/provisioning"
	"github.com/modura-dev/modura/backend/internal/platform/database"
	"github.com/modura-dev/modura/backend/internal/platform/database/migrationtest"
	"github.com/modura-dev/modura/backend/internal/platform/identifier"
)

const (
	platformUsername = "e2e-operator"
	tenantUsername   = "e2e-admin"
	tenantSlug       = "e2e-tenant"
)

type credentials struct {
	PlatformUsername string `json:"platformUsername"`
	PlatformPassword string `json:"platformPassword"`
	TenantUsername   string `json:"tenantUsername"`
	TenantPassword   string `json:"tenantPassword"`
	TenantSlug       string `json:"tenantSlug"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "modura-e2e-seed: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	url := os.Getenv("MODURA_TEST_DATABASE_URL")
	if url == "" {
		return fmt.Errorf("MODURA_TEST_DATABASE_URL is required")
	}
	if !strings.Contains(url, "modura_test") {
		return fmt.Errorf("refusing destructive seed: database name must contain modura_test")
	}
	platformPassword := os.Getenv("MODURA_E2E_PLATFORM_PASSWORD")
	tenantPassword := os.Getenv("MODURA_E2E_TENANT_PASSWORD")
	if platformPassword == "" || tenantPassword == "" {
		return fmt.Errorf("MODURA_E2E_PLATFORM_PASSWORD and MODURA_E2E_TENANT_PASSWORD are required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}
	cleanup, err := migrationtest.Reset(ctx, pool)
	if err != nil {
		return fmt.Errorf("reset schema: %w", err)
	}
	cleanup()

	signingKey := []byte(strings.Repeat("k", 32))
	now := time.Now
	newID := func(now time.Time) (string, error) {
		id, idErr := identifier.NewUUIDv7(now, nil)
		return string(id), idErr
	}
	newSecret := func() (string, error) { return identity.NewOpaqueToken(32) }
	signer, err := identity.NewAccessTokenSigner("modura", "modura-admin", "primary", signingKey, 5*time.Minute)
	if err != nil {
		return fmt.Errorf("configure signer: %w", err)
	}
	verifier := identity.NewAccessTokenVerifier("modura", "modura-admin", map[string][]byte{"primary": signingKey}, 5*time.Second)
	platformSigner, err := identity.NewAccessTokenSigner("modura", "modura-platform", "primary", signingKey, 5*time.Minute)
	if err != nil {
		return fmt.Errorf("configure platform signer: %w", err)
	}
	platformVerifier := identity.NewAccessTokenVerifier("modura", "modura-platform", map[string][]byte{"primary": signingKey}, 5*time.Second)
	auditService, err := audit.NewService(auditpostgres.New(pool), newID)
	if err != nil {
		return fmt.Errorf("configure audit: %w", err)
	}

	identityService, err := identity.NewService(identitypostgres.New(pool), signer, verifier, identity.DefaultPasswordParameters(), 24*time.Hour, now, newID, newSecret)
	if err != nil {
		return fmt.Errorf("configure identity: %w", err)
	}
	if err := identityService.EnableUserManagement(database.NewTransactor(pool), auditAdapter{auditService}); err != nil {
		return fmt.Errorf("enable identity user management: %w", err)
	}
	authorizationService, err := authorization.NewService(authorizationpostgres.New(pool))
	if err != nil {
		return fmt.Errorf("configure authorization: %w", err)
	}
	if err := auditService.EnableQueries(auditpostgres.New(pool)); err != nil {
		return fmt.Errorf("enable audit queries: %w", err)
	}
	if err := authorizationService.EnableManagement(authorizationpostgres.New(pool), database.NewTransactor(pool), auditService, identityService, now, newID); err != nil {
		return fmt.Errorf("enable authorization management: %w", err)
	}
	organizationService, err := organization.NewService(organizationpostgres.New(pool), database.NewTransactor(pool), auditService, now, newID)
	if err != nil {
		return fmt.Errorf("configure organization: %w", err)
	}
	platformAdminService, err := platformadmin.NewService(platformadminpostgres.New(pool), platformSigner, platformVerifier, identity.DefaultPasswordParameters(), 24*time.Hour, now, newID, newSecret)
	if err != nil {
		return fmt.Errorf("configure platform admin: %w", err)
	}
	provisioningService, err := provisioning.NewService(pool, identityService, organizationService, authorizationService, auditService, now, newID, newSecret, 24*time.Hour)
	if err != nil {
		return fmt.Errorf("configure provisioning: %w", err)
	}

	if _, err := platformAdminService.Bootstrap(ctx, platformUsername, platformPassword); err != nil {
		return fmt.Errorf("bootstrap platform administrator: %w", err)
	}
	actor := platformadmin.Actor{AdministratorID: "018e0000-0000-7000-8000-000000000001", SessionID: "018e0000-0000-7000-8000-000000000002"}
	request := provisioning.Request{
		IdempotencyKey:        "018e0000-0000-7000-8000-000000000003",
		Slug:                  tenantSlug,
		DisplayName:           "E2E Tenant",
		RootDepartmentName:    "E2E Root",
		AdministratorUsername: tenantUsername,
		Actor:                 actor,
		Reason:                "browser e2e suite",
		CorrelationID:         "e2e-seed",
	}
	result, err := provisioningService.Provision(ctx, request)
	if err != nil {
		return fmt.Errorf("provision tenant: %w", err)
	}
	if err := identityService.ConsumeOneTimeToken(ctx, result.InvitationToken, identity.PurposeInvitation, tenantPassword, "e2e-seed-invitation"); err != nil {
		return fmt.Errorf("accept invitation: %w", err)
	}
	encoded, err := json.Marshal(credentials{PlatformUsername: platformUsername, PlatformPassword: platformPassword, TenantUsername: tenantUsername, TenantPassword: tenantPassword, TenantSlug: tenantSlug})
	if err != nil {
		return fmt.Errorf("encode credentials: %w", err)
	}
	fmt.Println(string(encoded))
	return nil
}

type auditAdapter struct {
	service *audit.Service
}

func (a auditAdapter) RecordAccountEvent(ctx context.Context, tx pgx.Tx, event identity.AccountAuditEvent) error {
	return a.service.RecordTenantWrite(ctx, tx, audit.Event{ActorID: event.Actor.UserID, TenantID: event.Actor.TenantID, Action: event.Action, Resource: event.Resource, ResourceID: event.ResourceID, Reason: event.Reason, CorrelationID: event.CorrelationID, OccurredAt: event.OccurredAt, BeforeState: event.BeforeState, AfterState: event.AfterState})
}
