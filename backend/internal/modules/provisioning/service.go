// Package provisioning coordinates atomic tenant creation across module boundaries.
package provisioning

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modura-dev/modura/backend/internal/modules/audit"
	"github.com/modura-dev/modura/backend/internal/modules/authorization"
	"github.com/modura-dev/modura/backend/internal/modules/identity"
	"github.com/modura-dev/modura/backend/internal/modules/organization"
	"github.com/modura-dev/modura/backend/internal/modules/platformadmin"
)

var (
	// ErrIdempotencyConflict means a key was reused for different input.
	ErrIdempotencyConflict = errors.New("idempotency key reused with different request")
)

// Identity owns identity records created by provisioning.
type Identity interface {
	ProvisionTenant(context.Context, pgx.Tx, identity.TenantProvisioning) error
	ActivateTenant(context.Context, pgx.Tx, identity.TenantID, time.Time) error
}

// Organization owns the root department and first user assignment.
type Organization interface {
	ProvisionInitialOrganization(context.Context, pgx.Tx, organization.Department, identity.UserID) error
}

// Authorization owns the tenant-administrator role and grant.
type Authorization interface {
	ProvisionTenantAdministrator(context.Context, pgx.Tx, authorization.Role, identity.UserID) error
}

// Auditor records provisioning activity in the same transaction.
type Auditor interface {
	RecordTenantScopedPlatformWrite(context.Context, pgx.Tx, audit.TenantScopedPlatformEvent) error
}

// Request contains canonical tenant provisioning input.
type Request struct {
	IdempotencyKey        string
	Slug                  string
	DisplayName           string
	RootDepartmentName    string
	AdministratorUsername string
	AdministratorEmail    string
	Actor                 platformadmin.Actor
	Reason                string
	CorrelationID         string
}

// Result identifies provisioned resources. InvitationToken is present only
// for the transaction that first creates the tenant.
type Result struct {
	TenantID        identity.TenantID
	AdministratorID identity.UserID
	InvitationToken string
	Created         bool
}

// Service coordinates the provisioning transaction without owning module data.
type Service struct {
	pool               *pgxpool.Pool
	identity           Identity
	organization       Organization
	authorization      Authorization
	auditor            Auditor
	now                func() time.Time
	newID              func(time.Time) (string, error)
	newSecret          func() (string, error)
	invitationLifetime time.Duration
}

// NewService constructs a tenant provisioning coordinator.
func NewService(pool *pgxpool.Pool, identityService Identity, organizationService Organization, authorizationService Authorization, auditor Auditor, now func() time.Time, newID func(time.Time) (string, error), newSecret func() (string, error), invitationLifetime time.Duration) (*Service, error) {
	if pool == nil || identityService == nil || organizationService == nil || authorizationService == nil || auditor == nil || now == nil || newID == nil || newSecret == nil || invitationLifetime <= 0 {
		return nil, fmt.Errorf("invalid provisioning service configuration")
	}
	return &Service{pool: pool, identity: identityService, organization: organizationService, authorization: authorizationService, auditor: auditor, now: now, newID: newID, newSecret: newSecret, invitationLifetime: invitationLifetime}, nil
}

// Provision creates a complete active tenant or returns the prior idempotent result.
func (s *Service) Provision(ctx context.Context, request Request) (Result, error) {
	canonical, digest, err := canonicalRequest(request)
	if err != nil {
		return Result{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Result{}, fmt.Errorf("begin tenant provisioning: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, canonical.IdempotencyKey); err != nil {
		return Result{}, fmt.Errorf("lock tenant provisioning key: %w", err)
	}
	var existingDigest []byte
	var existingTenant identity.TenantID
	err = tx.QueryRow(ctx, `SELECT request_digest, tenant_id FROM modura.tenant_provisioning_requests WHERE idempotency_key = $1`, canonical.IdempotencyKey).Scan(&existingDigest, &existingTenant)
	if err == nil {
		if !hmac.Equal(existingDigest, digest[:]) {
			return Result{}, ErrIdempotencyConflict
		}
		return Result{TenantID: existingTenant, Created: false}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Result{}, fmt.Errorf("read tenant provisioning request: %w", err)
	}
	now := s.now().UTC()
	ids := make([]string, 6)
	for index := range ids {
		ids[index], err = s.newID(now)
		if err != nil {
			return Result{}, fmt.Errorf("generate provisioning ID: %w", err)
		}
	}
	invitation, err := s.newSecret()
	if err != nil {
		return Result{}, fmt.Errorf("generate administrator invitation: %w", err)
	}
	tenantID := identity.TenantID(ids[0])
	administratorID := identity.UserID(ids[1])
	identityInput := identity.TenantProvisioning{
		TenantID: tenantID, Slug: canonical.Slug, DisplayName: canonical.DisplayName,
		AdministratorID: administratorID, Username: canonical.AdministratorUsername,
		NormalizedUsername: identity.NormalizeLogin(canonical.AdministratorUsername), Email: canonical.AdministratorEmail,
		NormalizedEmail: identity.NormalizeLogin(canonical.AdministratorEmail), InvitationID: ids[4],
		InvitationHash: identity.HashOpaqueToken(invitation), CreatedAt: now, InvitationExpires: now.Add(s.invitationLifetime),
	}
	if err := s.identity.ProvisionTenant(ctx, tx, identityInput); err != nil {
		return Result{}, err
	}
	root := organization.Department{ID: organization.DepartmentID(ids[2]), TenantID: tenantID, Name: canonical.RootDepartmentName, NormalizedName: organization.NormalizeName(canonical.RootDepartmentName), CreatedAt: now}
	if err := s.organization.ProvisionInitialOrganization(ctx, tx, root, administratorID); err != nil {
		return Result{}, err
	}
	role := authorization.Role{ID: authorization.RoleID(ids[3]), TenantID: tenantID, Code: "tenant-admin", Name: "Tenant Administrator", Reserved: true, CreatedAt: now}
	if err := s.authorization.ProvisionTenantAdministrator(ctx, tx, role, administratorID); err != nil {
		return Result{}, err
	}
	if err := s.identity.ActivateTenant(ctx, tx, tenantID, now); err != nil {
		return Result{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO modura.tenant_provisioning_requests (idempotency_key, request_digest, tenant_id, created_at, completed_at) VALUES ($1, $2, $3, $4, $4)`, canonical.IdempotencyKey, digest[:], tenantID, now); err != nil {
		return Result{}, fmt.Errorf("record tenant provisioning request: %w", err)
	}
	if err := s.auditor.RecordTenantScopedPlatformWrite(ctx, tx, audit.TenantScopedPlatformEvent{ActorID: string(canonical.Actor.AdministratorID), TenantID: tenantID, Action: "tenant.provisioned", Resource: "tenant", ResourceID: string(tenantID), Reason: canonical.Reason, CorrelationID: canonical.CorrelationID, OccurredAt: now}); err != nil {
		return Result{}, fmt.Errorf("record tenant provisioning audit: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Result{}, fmt.Errorf("commit tenant provisioning: %w", err)
	}
	return Result{TenantID: tenantID, AdministratorID: administratorID, InvitationToken: invitation, Created: true}, nil
}

func canonicalRequest(request Request) (Request, [32]byte, error) {
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	request.Slug = identity.NormalizeLogin(request.Slug)
	request.DisplayName = strings.TrimSpace(request.DisplayName)
	request.RootDepartmentName = strings.TrimSpace(request.RootDepartmentName)
	request.AdministratorUsername = strings.TrimSpace(request.AdministratorUsername)
	request.AdministratorEmail = strings.TrimSpace(request.AdministratorEmail)
	request.Reason = strings.TrimSpace(request.Reason)
	request.CorrelationID = strings.TrimSpace(request.CorrelationID)
	if request.IdempotencyKey == "" || (request.Slug == "" || request.Slug == "community") || request.DisplayName == "" || request.RootDepartmentName == "" || request.AdministratorUsername == "" || request.Actor.AdministratorID == "" || request.Actor.SessionID == "" || request.Reason == "" || request.CorrelationID == "" {
		return Request{}, [32]byte{}, fmt.Errorf("invalid tenant provisioning request")
	}
	digestInput := struct {
		Slug                  string
		DisplayName           string
		RootDepartmentName    string
		AdministratorUsername string
		AdministratorEmail    string
	}{request.Slug, request.DisplayName, request.RootDepartmentName, request.AdministratorUsername, request.AdministratorEmail}
	encoded, err := json.Marshal(digestInput)
	if err != nil {
		return Request{}, [32]byte{}, fmt.Errorf("encode tenant provisioning request: %w", err)
	}
	return request, sha256.Sum256(encoded), nil
}
