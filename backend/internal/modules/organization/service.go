package organization

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/audit"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/identity"
)

// Store is the persistence boundary consumed by organization use cases.
type Store interface {
	ListDepartments(context.Context, identity.TenantID, DataScope) ([]DepartmentView, error)
	ListPositions(context.Context, identity.TenantID) ([]PositionView, error)
	CreateDepartment(context.Context, pgx.Tx, Department, DataScope) error
	UpdateDepartment(context.Context, pgx.Tx, identity.TenantID, DepartmentID, string, string, int, DataScope, time.Time) error
	MoveDepartment(context.Context, pgx.Tx, identity.TenantID, DepartmentID, DepartmentID, DataScope, time.Time) error
	DeleteDepartment(context.Context, pgx.Tx, identity.TenantID, DepartmentID, DataScope) error
	CreatePosition(context.Context, pgx.Tx, Position) error
	UpdatePosition(context.Context, pgx.Tx, identity.TenantID, PositionID, string, string, PositionStatus, time.Time) error
	AssignUser(context.Context, pgx.Tx, identity.TenantID, identity.UserID, DepartmentID, *PositionID, DataScope, time.Time) error
	ProvisionInitialOrganization(context.Context, pgx.Tx, Department, identity.UserID) error
}

// UpdateDepartment changes a visible department's display fields.
func (s *Service) UpdateDepartment(ctx context.Context, write WriteContext, departmentID DepartmentID, name string, sortOrder int) error {
	normalized := NormalizeName(name)
	if !validWrite(write) || departmentID == "" || normalized == "" {
		return fmt.Errorf("invalid department update")
	}
	now := s.now().UTC()
	return s.write(ctx, write, now, "organization.department.updated", "department", string(departmentID), func(tx pgx.Tx) error {
		return s.store.UpdateDepartment(ctx, tx, write.Actor.TenantID, departmentID, strings.TrimSpace(name), normalized, sortOrder, write.Scope, now)
	})
}

// Transactor supplies the application-owned transaction boundary.
type Transactor interface {
	WithinTransaction(context.Context, func(pgx.Tx) error) error
}

// Auditor owns durable audit persistence inside the organization transaction.
type Auditor interface {
	RecordTenantWrite(context.Context, pgx.Tx, audit.Event) error
}

// WriteContext carries verified evidence required for tenant management writes.
type WriteContext struct {
	Actor         identity.Actor
	CorrelationID string
	Scope         DataScope
}

// ListPositions returns the actor tenant's position catalog.
func (s *Service) ListPositions(ctx context.Context, tenantID identity.TenantID) ([]PositionView, error) {
	if tenantID == "" {
		return nil, fmt.Errorf("invalid tenant scope")
	}
	positions, err := s.store.ListPositions(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list positions: %w", err)
	}
	return positions, nil
}

// ListDepartments returns the actor tenant's department projection.
func (s *Service) ListDepartments(ctx context.Context, tenantID identity.TenantID, scope DataScope) ([]DepartmentView, error) {
	if tenantID == "" || !scope.valid() {
		return nil, fmt.Errorf("invalid tenant scope")
	}
	departments, err := s.store.ListDepartments(ctx, tenantID, scope)
	if err != nil {
		return nil, fmt.Errorf("list departments: %w", err)
	}
	return departments, nil
}

// Service implements tenant-scoped organization use cases.
type Service struct {
	store        Store
	transactions Transactor
	auditor      Auditor
	now          func() time.Time
	newID        func(time.Time) (string, error)
}

// NewService constructs an organization service.
func NewService(store Store, transactions Transactor, auditor Auditor, now func() time.Time, newID func(time.Time) (string, error)) (*Service, error) {
	if store == nil || transactions == nil || auditor == nil || now == nil || newID == nil {
		return nil, fmt.Errorf("invalid organization service configuration")
	}
	return &Service{store: store, transactions: transactions, auditor: auditor, now: now, newID: newID}, nil
}

// CreateDepartment creates a root or child department in an explicit tenant.
func (s *Service) CreateDepartment(ctx context.Context, write WriteContext, parentID *DepartmentID, name string, sortOrder int) (DepartmentID, error) {
	normalized := NormalizeName(name)
	if !validWrite(write) || normalized == "" {
		return "", fmt.Errorf("invalid department")
	}
	now := s.now().UTC()
	id, err := s.newID(now)
	if err != nil {
		return "", fmt.Errorf("generate department ID: %w", err)
	}
	department := Department{ID: DepartmentID(id), TenantID: write.Actor.TenantID, ParentID: parentID, Name: strings.TrimSpace(name), NormalizedName: normalized, SortOrder: sortOrder, CreatedAt: now}
	if err := s.write(ctx, write, now, "organization.department.created", "department", string(department.ID), func(tx pgx.Tx) error { return s.store.CreateDepartment(ctx, tx, department, write.Scope) }); err != nil {
		return "", fmt.Errorf("create department: %w", err)
	}
	return department.ID, nil
}

// MoveDepartment moves a non-root department while preventing cycles.
func (s *Service) MoveDepartment(ctx context.Context, write WriteContext, departmentID, newParentID DepartmentID) error {
	if !validWrite(write) || departmentID == "" || newParentID == "" {
		return fmt.Errorf("invalid department move")
	}
	now := s.now().UTC()
	return s.write(ctx, write, now, "organization.department.moved", "department", string(departmentID), func(tx pgx.Tx) error {
		return s.store.MoveDepartment(ctx, tx, write.Actor.TenantID, departmentID, newParentID, write.Scope, now)
	})
}

// DeleteDepartment deletes an unused non-root department.
func (s *Service) DeleteDepartment(ctx context.Context, write WriteContext, departmentID DepartmentID) error {
	if !validWrite(write) || departmentID == "" {
		return fmt.Errorf("invalid department delete")
	}
	now := s.now().UTC()
	return s.write(ctx, write, now, "organization.department.deleted", "department", string(departmentID), func(tx pgx.Tx) error {
		return s.store.DeleteDepartment(ctx, tx, write.Actor.TenantID, departmentID, write.Scope)
	})
}

// CreatePosition creates an active tenant position.
func (s *Service) CreatePosition(ctx context.Context, write WriteContext, name string) (PositionID, error) {
	normalized := NormalizeName(name)
	if !validWrite(write) || normalized == "" {
		return "", fmt.Errorf("invalid position")
	}
	now := s.now().UTC()
	id, err := s.newID(now)
	if err != nil {
		return "", fmt.Errorf("generate position ID: %w", err)
	}
	position := Position{ID: PositionID(id), TenantID: write.Actor.TenantID, Name: strings.TrimSpace(name), NormalizedName: normalized, CreatedAt: now}
	if err := s.write(ctx, write, now, "organization.position.created", "position", string(position.ID), func(tx pgx.Tx) error { return s.store.CreatePosition(ctx, tx, position) }); err != nil {
		return "", fmt.Errorf("create position: %w", err)
	}
	return position.ID, nil
}

// UpdatePosition changes a position's display name and lifecycle status.
func (s *Service) UpdatePosition(ctx context.Context, write WriteContext, positionID PositionID, name string, status PositionStatus) error {
	normalized := NormalizeName(name)
	if !validWrite(write) || positionID == "" || normalized == "" || !status.valid() {
		return fmt.Errorf("invalid position update")
	}
	now := s.now().UTC()
	return s.write(ctx, write, now, "organization.position.updated", "position", string(positionID), func(tx pgx.Tx) error {
		return s.store.UpdatePosition(ctx, tx, write.Actor.TenantID, positionID, strings.TrimSpace(name), normalized, status, now)
	})
}

// AssignUser sets the user's single primary department and optional position.
func (s *Service) AssignUser(ctx context.Context, write WriteContext, userID identity.UserID, departmentID DepartmentID, positionID *PositionID) error {
	if !validWrite(write) || userID == "" || departmentID == "" {
		return fmt.Errorf("invalid user organization assignment")
	}
	now := s.now().UTC()
	return s.write(ctx, write, now, "organization.user-assigned", "user", string(userID), func(tx pgx.Tx) error {
		return s.store.AssignUser(ctx, tx, write.Actor.TenantID, userID, departmentID, positionID, write.Scope, now)
	})
}

func (s *Service) write(ctx context.Context, write WriteContext, now time.Time, action, resource, resourceID string, change func(pgx.Tx) error) error {
	return s.transactions.WithinTransaction(ctx, func(tx pgx.Tx) error {
		if err := change(tx); err != nil {
			return err
		}
		return s.auditor.RecordTenantWrite(ctx, tx, audit.Event{ActorID: write.Actor.UserID, TenantID: write.Actor.TenantID, Action: action, Resource: resource, ResourceID: resourceID, Reason: "authorized tenant management request", CorrelationID: write.CorrelationID, OccurredAt: now})
	})
}

func validWrite(write WriteContext) bool {
	return write.Actor.TenantID != "" && write.Actor.UserID != "" && write.Actor.SessionID != "" && strings.TrimSpace(write.CorrelationID) != "" && write.Scope.valid() && write.Scope.ActorID == write.Actor.UserID
}

// ProvisionInitialOrganization creates the sole root department and assigns
// the first administrator inside the caller-owned provisioning transaction.
func (s *Service) ProvisionInitialOrganization(ctx context.Context, tx pgx.Tx, root Department, administratorID identity.UserID) error {
	if tx == nil || root.TenantID == "" || root.ID == "" || root.ParentID != nil || administratorID == "" {
		return fmt.Errorf("invalid initial organization")
	}
	if err := s.store.ProvisionInitialOrganization(ctx, tx, root, administratorID); err != nil {
		return fmt.Errorf("provision initial organization: %w", err)
	}
	return nil
}
