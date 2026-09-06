// Package platformtenant owns platform-level tenant lifecycle use cases.
package platformtenant

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/modura-dev/modura/backend/internal/modules/identity"
	"github.com/modura-dev/modura/backend/internal/modules/platformadmin"
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

// LifecycleChange carries mandatory cross-tenant authorization evidence.
type LifecycleChange struct {
	Actor         platformadmin.Actor
	TenantID      identity.TenantID
	Reason        string
	CorrelationID string
	AuditID       string
	OccurredAt    time.Time
}

// ProfileChange carries a validated optimistic tenant profile update.
type ProfileChange struct {
	Actor             platformadmin.Actor
	TenantID          identity.TenantID
	DisplayName       string
	ExpectedUpdatedAt time.Time
	Reason            string
	CorrelationID     string
	AuditID           string
	OccurredAt        time.Time
}

// Store is the persistence boundary consumed by platform tenant use cases.
type Store interface {
	List(context.Context) ([]Tenant, error)
	UpdateProfile(context.Context, ProfileChange) error
	ChangeStatus(context.Context, LifecycleChange, string, string) error
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
	auditID, err := s.newID(now)
	if err != nil {
		return fmt.Errorf("generate audit event ID: %w", err)
	}
	change := ProfileChange{Actor: actor, TenantID: tenantID, DisplayName: displayName, ExpectedUpdatedAt: expectedUpdatedAt.UTC(), Reason: reason, CorrelationID: correlationID, AuditID: auditID, OccurredAt: now}
	if err := s.store.UpdateProfile(ctx, change); err != nil {
		return fmt.Errorf("update tenant profile: %w", err)
	}
	return nil
}

// Service implements platform tenant queries and lifecycle changes.
type Service struct {
	store Store
	now   func() time.Time
	newID func(time.Time) (string, error)
}

// NewService constructs a platform tenant service.
func NewService(store Store, now func() time.Time, newID func(time.Time) (string, error)) (*Service, error) {
	if store == nil || now == nil || newID == nil {
		return nil, fmt.Errorf("invalid platform tenant service configuration")
	}
	return &Service{store: store, now: now, newID: newID}, nil
}

// List returns all tenants to an already verified platform actor.
func (s *Service) List(ctx context.Context, actor platformadmin.Actor) ([]Tenant, error) {
	if actor.AdministratorID == "" || actor.SessionID == "" {
		return nil, platformadmin.ErrInvalidToken
	}
	return s.store.List(ctx)
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
	auditID, err := s.newID(now)
	if err != nil {
		return fmt.Errorf("generate audit event ID: %w", err)
	}
	change := LifecycleChange{Actor: actor, TenantID: tenantID, Reason: reason, CorrelationID: correlationID, AuditID: auditID, OccurredAt: now}
	if err := s.store.ChangeStatus(ctx, change, from, to); err != nil {
		return fmt.Errorf("change tenant status: %w", err)
	}
	return nil
}
