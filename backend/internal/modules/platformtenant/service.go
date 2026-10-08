// Package platformtenant owns platform-level tenant lifecycle use cases.
// Tenant state is owned by the identity module; this module coordinates
// platform administration through identity's public transaction-capable API
// and records its own audit evidence inside the same transactions.
package platformtenant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/audit"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/identity"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/platformadmin"
)

var (
	// ErrNotFound means the target tenant does not exist.
	ErrNotFound = errors.New("tenant not found")
	// ErrInvalidTransition means the requested lifecycle transition is not allowed.
	ErrInvalidTransition = errors.New("invalid tenant lifecycle transition")
	// ErrConflict means another administrator changed the tenant profile first.
	ErrConflict = errors.New("tenant profile conflict")
)

// Tenant is the platform-visible tenant summary.
type Tenant struct {
	ID          identity.TenantID
	Slug        string
	DisplayName string
	Status      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// TenantStore exposes identity-owned tenant state through the owning module's
// public transaction-capable application API.
type TenantStore interface {
	ListTenantSummaries(context.Context) ([]identity.TenantSummary, error)
	LockTenantProfile(context.Context, pgx.Tx, identity.TenantID) (identity.TenantProfileState, error)
	UpdateTenantProfile(context.Context, pgx.Tx, identity.TenantID, string, time.Time) error
	TransitionTenantStatus(context.Context, pgx.Tx, identity.TenantID, string, string, time.Time) error
}

// Transactor supplies application-owned transaction boundaries.
type Transactor interface {
	WithinTransaction(context.Context, func(pgx.Tx) error) error
}

// Auditor records tenant-scoped platform activity in the same transaction.
type Auditor interface {
	RecordTenantScopedPlatformWrite(context.Context, pgx.Tx, audit.TenantScopedPlatformEvent) error
}

// Service implements platform tenant queries and lifecycle changes.
type Service struct {
	tenants      TenantStore
	transactions Transactor
	auditor      Auditor
	now          func() time.Time
	newID        func(time.Time) (string, error)
}

// NewService constructs a platform tenant service.
func NewService(tenants TenantStore, transactions Transactor, auditor Auditor, now func() time.Time, newID func(time.Time) (string, error)) (*Service, error) {
	if tenants == nil || transactions == nil || auditor == nil || now == nil || newID == nil {
		return nil, fmt.Errorf("invalid platform tenant service configuration")
	}
	return &Service{tenants: tenants, transactions: transactions, auditor: auditor, now: now, newID: newID}, nil
}

// List returns all tenants to an already verified platform actor.
func (s *Service) List(ctx context.Context, actor platformadmin.Actor) ([]Tenant, error) {
	if actor.AdministratorID == "" || actor.SessionID == "" {
		return nil, platformadmin.ErrInvalidToken
	}
	summaries, err := s.tenants.ListTenantSummaries(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tenants: %w", err)
	}
	tenants := make([]Tenant, 0, len(summaries))
	for _, summary := range summaries {
		tenants = append(tenants, Tenant{ID: summary.ID, Slug: summary.Slug, DisplayName: summary.DisplayName, Status: summary.Status, CreatedAt: summary.CreatedAt, UpdatedAt: summary.UpdatedAt})
	}
	return tenants, nil
}

// UpdateProfile changes only mutable tenant presentation data. The slug and
// lifecycle status remain controlled by their dedicated workflows.
func (s *Service) UpdateProfile(ctx context.Context, actor platformadmin.Actor, tenantID identity.TenantID, displayName string, expectedUpdatedAt time.Time, reason, correlationID string) error {
	displayName = strings.TrimSpace(displayName)
	reason = strings.TrimSpace(reason)
	correlationID = strings.TrimSpace(correlationID)
	if actor.AdministratorID == "" || actor.SessionID == "" || tenantID == "" || displayName == "" || len(displayName) > 128 || expectedUpdatedAt.IsZero() || reason == "" || len(reason) > 512 || correlationID == "" {
		return fmt.Errorf("invalid platform tenant profile request")
	}
	now := s.now().UTC()
	err := s.transactions.WithinTransaction(ctx, func(tx pgx.Tx) error {
		state, err := s.tenants.LockTenantProfile(ctx, tx, tenantID)
		if err != nil {
			return err
		}
		if !state.UpdatedAt.Equal(expectedUpdatedAt.UTC()) {
			return ErrConflict
		}
		before, err := json.Marshal(map[string]any{"slug": state.Slug, "displayName": state.DisplayName, "status": state.Status})
		if err != nil {
			return fmt.Errorf("encode previous tenant profile: %w", err)
		}
		after, err := json.Marshal(map[string]any{"slug": state.Slug, "displayName": displayName, "status": state.Status})
		if err != nil {
			return fmt.Errorf("encode updated tenant profile: %w", err)
		}
		if err := s.tenants.UpdateTenantProfile(ctx, tx, tenantID, displayName, now); err != nil {
			return err
		}
		return s.auditor.RecordTenantScopedPlatformWrite(ctx, tx, audit.TenantScopedPlatformEvent{ActorID: string(actor.AdministratorID), TenantID: tenantID, Action: "tenant.profile-updated", Resource: "tenant", ResourceID: string(tenantID), Reason: reason, CorrelationID: correlationID, OccurredAt: now, BeforeState: before, AfterState: after})
	})
	if err != nil {
		if errors.Is(err, identity.ErrTenantNotFound) {
			return ErrNotFound
		}
		return fmt.Errorf("update tenant profile: %w", err)
	}
	return nil
}

// Suspend prevents tenant-local authentication and session validation.
func (s *Service) Suspend(ctx context.Context, actor platformadmin.Actor, tenantID identity.TenantID, reason, correlationID string) error {
	return s.changeStatus(ctx, actor, tenantID, reason, correlationID, "active", "suspended")
}

// Reactivate restores a suspended tenant.
func (s *Service) Reactivate(ctx context.Context, actor platformadmin.Actor, tenantID identity.TenantID, reason, correlationID string) error {
	return s.changeStatus(ctx, actor, tenantID, reason, correlationID, "suspended", "active")
}

func (s *Service) changeStatus(ctx context.Context, actor platformadmin.Actor, tenantID identity.TenantID, reason, correlationID, from, to string) error {
	reason = strings.TrimSpace(reason)
	correlationID = strings.TrimSpace(correlationID)
	if actor.AdministratorID == "" || actor.SessionID == "" || tenantID == "" || reason == "" || correlationID == "" {
		return fmt.Errorf("invalid platform tenant lifecycle request")
	}
	now := s.now().UTC()
	err := s.transactions.WithinTransaction(ctx, func(tx pgx.Tx) error {
		if err := s.tenants.TransitionTenantStatus(ctx, tx, tenantID, from, to, now); err != nil {
			return err
		}
		return s.auditor.RecordTenantScopedPlatformWrite(ctx, tx, audit.TenantScopedPlatformEvent{ActorID: string(actor.AdministratorID), TenantID: tenantID, Action: "tenant." + to, Resource: "tenant", ResourceID: string(tenantID), Reason: reason, CorrelationID: correlationID, OccurredAt: now})
	})
	if err != nil {
		if errors.Is(err, identity.ErrTenantNotFound) {
			return ErrNotFound
		}
		if errors.Is(err, identity.ErrInvalidTenantTransition) {
			return ErrInvalidTransition
		}
		return fmt.Errorf("change tenant status: %w", err)
	}
	return nil
}
