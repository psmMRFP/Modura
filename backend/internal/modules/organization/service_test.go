package organization

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/audit"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/identity"
)

type storeStub struct {
	department     Department
	position       Position
	departmentName string
	departmentSort int
	positionName   string
	positionStatus PositionStatus
}

func (*storeStub) ListDepartments(context.Context, identity.TenantID, DataScope) ([]DepartmentView, error) {
	return nil, nil
}
func (*storeStub) ListPositions(context.Context, identity.TenantID) ([]PositionView, error) {
	return nil, nil
}

func (s *storeStub) CreateDepartment(_ context.Context, _ pgx.Tx, department Department, _ DataScope) error {
	s.department = department
	return nil
}
func (s *storeStub) UpdateDepartment(_ context.Context, _ pgx.Tx, _ identity.TenantID, _ DepartmentID, name, _ string, sortOrder int, _ DataScope, _ time.Time) error {
	s.departmentName = name
	s.departmentSort = sortOrder
	return nil
}
func (*storeStub) MoveDepartment(context.Context, pgx.Tx, identity.TenantID, DepartmentID, DepartmentID, DataScope, time.Time) error {
	return nil
}
func (*storeStub) DeleteDepartment(context.Context, pgx.Tx, identity.TenantID, DepartmentID, DataScope) error {
	return nil
}
func (s *storeStub) CreatePosition(_ context.Context, _ pgx.Tx, position Position) error {
	s.position = position
	return nil
}
func (s *storeStub) UpdatePosition(_ context.Context, _ pgx.Tx, _ identity.TenantID, _ PositionID, name, _ string, status PositionStatus, _ time.Time) error {
	s.positionName = name
	s.positionStatus = status
	return nil
}
func (*storeStub) AssignUser(context.Context, pgx.Tx, identity.TenantID, identity.UserID, DepartmentID, *PositionID, DataScope, time.Time) error {
	return nil
}

func TestServiceUpdatesOrganizationCatalogAndAudits(t *testing.T) {
	store := &storeStub{}
	auditor := &auditorStub{}
	now := time.Unix(1_700_000_000, 0)
	service, err := NewService(store, transactorStub{}, auditor, func() time.Time { return now }, func(time.Time) (string, error) { return "unused", nil })
	if err != nil {
		t.Fatal(err)
	}
	write := WriteContext{Actor: identity.Actor{TenantID: "tenant", UserID: "user", SessionID: "session"}, CorrelationID: "request-2", Scope: DataScope{ActorID: "user", All: true}}
	if err := service.UpdateDepartment(context.Background(), write, "department", " 产品研发 ", 20); err != nil {
		t.Fatal(err)
	}
	if store.departmentName != "产品研发" || store.departmentSort != 20 {
		t.Fatalf("department update = %q, %d", store.departmentName, store.departmentSort)
	}
	if err := service.UpdatePosition(context.Background(), write, "position", " Staff Engineer ", PositionStatusDisabled); err != nil {
		t.Fatal(err)
	}
	if store.positionName != "Staff Engineer" || store.positionStatus != PositionStatusDisabled {
		t.Fatalf("position update = %q, %q", store.positionName, store.positionStatus)
	}
	if len(auditor.events) != 2 || auditor.events[0].Action != "organization.department.updated" || auditor.events[1].Action != "organization.position.updated" {
		t.Fatalf("audit events = %+v", auditor.events)
	}
	if err := service.UpdatePosition(context.Background(), write, "position", "name", "unknown"); err == nil {
		t.Fatal("expected invalid position status to fail")
	}
}

type transactorStub struct{}

func (transactorStub) WithinTransaction(_ context.Context, work func(pgx.Tx) error) error {
	return work(nil)
}

type auditorStub struct{ events []audit.Event }

func (s *auditorStub) RecordTenantWrite(_ context.Context, _ pgx.Tx, event audit.Event) error {
	s.events = append(s.events, event)
	return nil
}
func (*storeStub) ProvisionInitialOrganization(context.Context, pgx.Tx, Department, identity.UserID) error {
	return nil
}

func TestServiceNormalizesOrganizationNames(t *testing.T) {
	store := &storeStub{}
	auditor := &auditorStub{}
	now := time.Unix(1_700_000_000, 0)
	service, err := NewService(store, transactorStub{}, auditor, func() time.Time { return now }, func(time.Time) (string, error) { return "018bcfe5-6800-7000-8000-000000000201", nil })
	if err != nil {
		t.Fatal(err)
	}
	write := WriteContext{Actor: identity.Actor{TenantID: "tenant", UserID: "user", SessionID: "session"}, CorrelationID: "request-1", Scope: DataScope{ActorID: "user", All: true}}
	if _, err := service.CreateDepartment(context.Background(), write, nil, " 研发中心 ", 10); err != nil {
		t.Fatal(err)
	}
	if store.department.Name != "研发中心" || store.department.NormalizedName != "研发中心" || !store.department.CreatedAt.Equal(now) {
		t.Fatalf("department = %+v", store.department)
	}
	if _, err := service.CreatePosition(context.Background(), write, " Platform ENGINEER "); err != nil {
		t.Fatal(err)
	}
	if store.position.Name != "Platform ENGINEER" || store.position.NormalizedName != "platform engineer" {
		t.Fatalf("position = %+v", store.position)
	}
	if len(auditor.events) != 2 || auditor.events[0].ActorID != "user" || auditor.events[0].CorrelationID != "request-1" {
		t.Fatalf("audit events = %+v", auditor.events)
	}
}
