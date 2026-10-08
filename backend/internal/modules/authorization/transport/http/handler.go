// Package http adapts authorization use cases to the HTTP contract.
package http

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/psmMRFP/WhereToLive/backend/internal/api/generated"
	apihttp "github.com/psmMRFP/WhereToLive/backend/internal/api/transport"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/authorization"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/identity"
)

// ActorResolver authenticates tenant-local actors.
type ActorResolver interface {
	Actor(*gin.Context) (identity.Actor, bool)
}

// Service is the authorization application API consumed by HTTP delivery.
type Service interface {
	Authorize(context.Context, identity.Actor, authorization.Permission) error
	EffectivePermissions(context.Context, identity.Actor) ([]authorization.Permission, error)
	ListRoles(context.Context, identity.Actor) ([]authorization.RoleView, error)
	CreateRole(context.Context, authorization.WriteContext, string, string) (authorization.RoleView, error)
	GetRolePolicySet(context.Context, identity.Actor, authorization.RoleID) (authorization.RolePolicySet, error)
	ReplaceRolePolicies(context.Context, authorization.WriteContext, authorization.RoleID, int64, []authorization.Policy) (int64, error)
	GetUserRoleGrants(context.Context, identity.Actor, identity.UserID) (authorization.UserRoleGrantSet, error)
	ReplaceUserRoleGrants(context.Context, authorization.WriteContext, identity.UserID, int64, []authorization.RoleID) (authorization.UserRoleGrantSet, error)
}

// ListEffectivePermissions projects canonical permissions for frontend navigation.
func (h *AuthorizationHandler) ListEffectivePermissions(c *gin.Context) {
	if h.service == nil || h.actors == nil {
		h.security.Problem(c, http.StatusServiceUnavailable, "service unavailable")
		return
	}
	actor, ok := h.actors.Actor(c)
	if !ok {
		return
	}
	permissions, err := h.service.EffectivePermissions(c.Request.Context(), actor)
	if err != nil {
		h.security.Problem(c, http.StatusInternalServerError, "internal server error")
		return
	}
	response := make([]generated.EffectivePermission, 0, len(permissions))
	for _, permission := range permissions {
		response = append(response, generated.EffectivePermission{Resource: string(permission.Resource), Action: string(permission.Action)})
	}
	c.JSON(http.StatusOK, response)
}

// GetRolePolicySet returns one tenant role's current policy desired state.
func (h *AuthorizationHandler) GetRolePolicySet(c *gin.Context, roleID generated.RoleId) {
	actor, ok := h.authorized(c, authorization.Permission{Resource: authorization.ResourcePolicies, Action: authorization.ActionRead})
	if !ok {
		return
	}
	state, err := h.service.GetRolePolicySet(c.Request.Context(), actor, authorization.RoleID(roleID.String()))
	if h.writeError(c, err) {
		return
	}
	response, ok := policySetResponse(state)
	if !ok {
		h.security.Problem(c, http.StatusInternalServerError, "internal server error")
		return
	}
	c.JSON(http.StatusOK, response)
}

// AuthorizationHandler serves tenant role and policy operations.
type AuthorizationHandler struct {
	service  Service
	actors   ActorResolver
	security *apihttp.Security
}

// NewHandler constructs the authorization HTTP adapter.
func NewHandler(service Service, actors ActorResolver, security *apihttp.Security) *AuthorizationHandler {
	return &AuthorizationHandler{service: service, actors: actors, security: security}
}

// ListRoles lists roles in the authenticated actor's tenant.
func (h *AuthorizationHandler) ListRoles(c *gin.Context) {
	actor, ok := h.authorized(c, authorization.Permission{Resource: authorization.ResourceRoles, Action: authorization.ActionRead})
	if !ok {
		return
	}
	roles, err := h.service.ListRoles(c.Request.Context(), actor)
	if err != nil {
		h.security.Problem(c, http.StatusInternalServerError, "internal server error")
		return
	}
	response := make([]generated.Role, 0, len(roles))
	for _, role := range roles {
		item, ok := roleResponse(role)
		if !ok {
			h.security.Problem(c, http.StatusInternalServerError, "internal server error")
			return
		}
		response = append(response, item)
	}
	c.JSON(http.StatusOK, response)
}

// CreateRole creates a non-reserved tenant role.
func (h *AuthorizationHandler) CreateRole(c *gin.Context, params generated.CreateRoleParams) {
	if !h.csrf(c, params.XCSRFToken) {
		return
	}
	actor, ok := h.authorized(c, authorization.Permission{Resource: authorization.ResourceRoles, Action: authorization.ActionCreate})
	if !ok {
		return
	}
	var request generated.CreateRoleRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		h.security.Problem(c, http.StatusBadRequest, "invalid request")
		return
	}
	role, err := h.service.CreateRole(c.Request.Context(), writeContext(c, actor), request.Code, request.Name)
	if err != nil {
		h.security.Problem(c, http.StatusBadRequest, "invalid request")
		return
	}
	response, ok := roleResponse(role)
	if !ok {
		h.security.Problem(c, http.StatusInternalServerError, "internal server error")
		return
	}
	c.JSON(http.StatusCreated, response)
}

// ReplaceRolePolicies replaces a role's versioned policy desired state.
func (h *AuthorizationHandler) ReplaceRolePolicies(c *gin.Context, roleID generated.RoleId, params generated.ReplaceRolePoliciesParams) {
	if !h.csrf(c, params.XCSRFToken) {
		return
	}
	actor, ok := h.authorized(c, authorization.Permission{Resource: authorization.ResourcePolicies, Action: authorization.ActionUpdate})
	if !ok {
		return
	}
	var request generated.ReplaceRolePoliciesRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		h.security.Problem(c, http.StatusBadRequest, "invalid request")
		return
	}
	policies := make([]authorization.Policy, 0, len(request.Policies))
	for _, item := range request.Policies {
		policy := authorization.Policy{Permission: authorization.Permission{Resource: authorization.Resource(item.Resource), Action: authorization.Action(item.Action)}, Scope: authorization.DataScopeKind(item.DataScope)}
		if item.DepartmentIds != nil {
			for _, id := range *item.DepartmentIds {
				policy.DepartmentIDs = append(policy.DepartmentIDs, id.String())
			}
		}
		policies = append(policies, policy)
	}
	version, err := h.service.ReplaceRolePolicies(c.Request.Context(), writeContext(c, actor), authorization.RoleID(roleID.String()), request.ExpectedVersion, policies)
	if h.writeError(c, err) {
		return
	}
	c.JSON(http.StatusOK, generated.VersionResponse{Version: version})
}

// GetUserRoleGrants returns versioned non-reserved grants for a tenant user.
func (h *AuthorizationHandler) GetUserRoleGrants(c *gin.Context, userID generated.UserId) {
	actor, ok := h.authorized(c, authorization.Permission{Resource: authorization.ResourceUserRoles, Action: authorization.ActionRead})
	if !ok {
		return
	}
	state, err := h.service.GetUserRoleGrants(c.Request.Context(), actor, identity.UserID(userID.String()))
	if h.writeError(c, err) {
		return
	}
	response, ok := grantResponse(state)
	if !ok {
		h.security.Problem(c, http.StatusInternalServerError, "internal server error")
		return
	}
	c.JSON(http.StatusOK, response)
}

// ReplaceUserRoleGrants replaces non-reserved user grants with optimistic locking.
func (h *AuthorizationHandler) ReplaceUserRoleGrants(c *gin.Context, userID generated.UserId, params generated.ReplaceUserRoleGrantsParams) {
	if !h.csrf(c, params.XCSRFToken) {
		return
	}
	actor, ok := h.authorized(c, authorization.Permission{Resource: authorization.ResourceUserRoles, Action: authorization.ActionUpdate})
	if !ok {
		return
	}
	var request generated.ReplaceUserRoleGrantsRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		h.security.Problem(c, http.StatusBadRequest, "invalid request")
		return
	}
	roleIDs := make([]authorization.RoleID, 0, len(request.RoleIds))
	for _, id := range request.RoleIds {
		roleIDs = append(roleIDs, authorization.RoleID(id.String()))
	}
	state, err := h.service.ReplaceUserRoleGrants(c.Request.Context(), writeContext(c, actor), identity.UserID(userID.String()), request.ExpectedVersion, roleIDs)
	if h.writeError(c, err) {
		return
	}
	response, ok := grantResponse(state)
	if !ok {
		h.security.Problem(c, http.StatusInternalServerError, "internal server error")
		return
	}
	c.JSON(http.StatusOK, response)
}

func (h *AuthorizationHandler) authorized(c *gin.Context, permission authorization.Permission) (identity.Actor, bool) {
	if h.service == nil || h.actors == nil {
		h.security.Problem(c, http.StatusServiceUnavailable, "service unavailable")
		return identity.Actor{}, false
	}
	actor, ok := h.actors.Actor(c)
	if !ok {
		return identity.Actor{}, false
	}
	if err := h.service.Authorize(c.Request.Context(), actor, permission); err != nil {
		if errors.Is(err, authorization.ErrDenied) {
			h.security.Problem(c, http.StatusForbidden, "forbidden")
		} else {
			h.security.Problem(c, http.StatusInternalServerError, "internal server error")
		}
		return identity.Actor{}, false
	}
	return actor, true
}

func (h *AuthorizationHandler) csrf(c *gin.Context, header string) bool {
	_, ok := h.security.CookieAndCSRF(c, apihttp.TenantRefreshCookie, apihttp.TenantCSRFCookie, header)
	return ok
}

func (h *AuthorizationHandler) writeError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, authorization.ErrDenied), errors.Is(err, authorization.ErrReserved):
		h.security.Problem(c, http.StatusForbidden, "forbidden")
	case errors.Is(err, authorization.ErrNotFound):
		h.security.Problem(c, http.StatusNotFound, "not found")
	case errors.Is(err, authorization.ErrConflict):
		h.security.Problem(c, http.StatusConflict, "version conflict")
	default:
		h.security.Problem(c, http.StatusBadRequest, "invalid request")
	}
	return true
}

func writeContext(c *gin.Context, actor identity.Actor) authorization.WriteContext {
	return authorization.WriteContext{Actor: actor, CorrelationID: c.GetHeader("X-Request-ID")}
}

func roleResponse(role authorization.RoleView) (generated.Role, bool) {
	id, err := uuid.Parse(string(role.ID))
	return generated.Role{Id: id, Code: role.Code, Name: role.Name, Reserved: role.Reserved, Version: role.Version}, err == nil
}

func grantResponse(state authorization.UserRoleGrantSet) (generated.UserRoleGrantSet, bool) {
	response := generated.UserRoleGrantSet{Version: state.Version, RoleIds: make([]uuid.UUID, 0, len(state.RoleIDs))}
	for _, roleID := range state.RoleIDs {
		id, err := uuid.Parse(string(roleID))
		if err != nil {
			return generated.UserRoleGrantSet{}, false
		}
		response.RoleIds = append(response.RoleIds, id)
	}
	return response, true
}

func policySetResponse(state authorization.RolePolicySet) (generated.RolePolicySet, bool) {
	response := generated.RolePolicySet{Version: state.Version, Reserved: state.Reserved, Policies: make([]generated.RolePolicy, 0, len(state.Policies))}
	for _, policy := range state.Policies {
		item := generated.RolePolicy{Resource: generated.RolePolicyResource(policy.Resource), Action: generated.RolePolicyAction(policy.Action), DataScope: generated.DataScopeKind(policy.Scope)}
		if len(policy.DepartmentIDs) > 0 {
			ids := make([]uuid.UUID, 0, len(policy.DepartmentIDs))
			for _, departmentID := range policy.DepartmentIDs {
				id, err := uuid.Parse(departmentID)
				if err != nil {
					return generated.RolePolicySet{}, false
				}
				ids = append(ids, id)
			}
			item.DepartmentIds = &ids
		}
		response.Policies = append(response.Policies, item)
	}
	return response, true
}
