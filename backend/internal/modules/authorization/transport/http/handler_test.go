package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	apihttp "github.com/psmMRFP/WhereToLive/backend/internal/api/transport"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/authorization"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/identity"
)

type actorResolverStub struct{ actor identity.Actor }

func (s actorResolverStub) Actor(*gin.Context) (identity.Actor, bool) { return s.actor, true }

type deniedService struct{}

func (deniedService) Authorize(context.Context, identity.Actor, authorization.Permission) error {
	return authorization.ErrDenied
}
func (deniedService) EffectivePermissions(context.Context, identity.Actor) ([]authorization.Permission, error) {
	return nil, authorization.ErrDenied
}
func (deniedService) ListRoles(context.Context, identity.Actor) ([]authorization.RoleView, error) {
	panic("authorization denial must stop delivery")
}
func (deniedService) CreateRole(context.Context, authorization.WriteContext, string, string) (authorization.RoleView, error) {
	panic("authorization denial must stop delivery")
}
func (deniedService) GetRolePolicySet(context.Context, identity.Actor, authorization.RoleID) (authorization.RolePolicySet, error) {
	panic("authorization denial must stop delivery")
}
func (deniedService) ReplaceRolePolicies(context.Context, authorization.WriteContext, authorization.RoleID, int64, []authorization.Policy) (int64, error) {
	panic("authorization denial must stop delivery")
}
func (deniedService) GetUserRoleGrants(context.Context, identity.Actor, identity.UserID) (authorization.UserRoleGrantSet, error) {
	panic("authorization denial must stop delivery")
}
func (deniedService) ReplaceUserRoleGrants(context.Context, authorization.WriteContext, identity.UserID, int64, []authorization.RoleID) (authorization.UserRoleGrantSet, error) {
	panic("authorization denial must stop delivery")
}

func TestListRolesRejectsMissingServerAuthority(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/authorization/roles", nil)
	handler := NewHandler(deniedService{}, actorResolverStub{actor: identity.Actor{TenantID: "tenant", UserID: "user", SessionID: "session"}}, apihttp.NewSecurity(true, func() (string, error) { return "csrf", nil }))
	handler.ListRoles(c)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
}
