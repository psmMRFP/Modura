package authorization

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/identity"
)

type policyStoreStub struct {
	roles []RolePolicies
}

func (s policyStoreStub) CreateReservedRole(context.Context, pgx.Tx, Role) error { return nil }
func (s policyStoreStub) AssignRole(context.Context, pgx.Tx, identity.TenantID, identity.UserID, RoleID, time.Time) error {
	return nil
}
func (s policyStoreStub) LoadActorPolicies(context.Context, identity.Actor) ([]RolePolicies, error) {
	return s.roles, nil
}

func TestAuthorizeUsesTenantDomainRBAC(t *testing.T) {
	permission := Permission{Resource: ResourceDepartments, Action: ActionRead}
	service, err := NewService(policyStoreStub{roles: []RolePolicies{{
		RoleID:   "role-a",
		Policies: []Policy{{Permission: permission, Scope: DataScopeDepartment}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	actor := identity.Actor{TenantID: "tenant-a", UserID: "user-a", SessionID: "session-a"}
	if err := service.Authorize(context.Background(), actor, permission); err != nil {
		t.Fatalf("authorize granted policy: %v", err)
	}
	if err := service.Authorize(context.Background(), actor, Permission{Resource: ResourceDepartments, Action: ActionDelete}); !errors.Is(err, ErrDenied) {
		t.Fatalf("missing policy error = %v", err)
	}
}

func TestResolveDataScopeUnionsRoles(t *testing.T) {
	permission := Permission{Resource: ResourceDepartments, Action: ActionRead}
	service, err := NewService(policyStoreStub{roles: []RolePolicies{
		{RoleID: "role-a", Policies: []Policy{{Permission: permission, Scope: DataScopeSelf}, {Permission: permission, Scope: DataScopeCustom, DepartmentIDs: []string{"department-b", "department-a"}}}},
		{RoleID: "role-b", Policies: []Policy{{Permission: permission, Scope: DataScopeDepartmentDescendants}, {Permission: permission, Scope: DataScopeCustom, DepartmentIDs: []string{"department-a"}}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	actor := identity.Actor{TenantID: "tenant-a", UserID: "user-a", SessionID: "session-a"}
	got, err := service.ResolveDataScope(context.Background(), actor, permission)
	if err != nil {
		t.Fatal(err)
	}
	want := ResolvedDataScope{Self: true, DepartmentAndDescendants: true, CustomDepartmentIDs: []string{"department-a", "department-b"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scope = %#v, want %#v", got, want)
	}
}
