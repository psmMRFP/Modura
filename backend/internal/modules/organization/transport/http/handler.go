// Package http adapts organization application use cases to the HTTP contract.
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
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/organization"
)

// ActorResolver authenticates a tenant-local request actor.
type ActorResolver interface {
	Actor(*gin.Context) (identity.Actor, bool)
}

// Authorizer checks canonical organization permissions.
type Authorizer interface {
	Authorize(context.Context, identity.Actor, authorization.Permission) error
	ResolveDataScope(context.Context, identity.Actor, authorization.Permission) (authorization.ResolvedDataScope, error)
}

// Service is the organization application API consumed by this adapter.
type Service interface {
	ListDepartments(context.Context, identity.TenantID, organization.DataScope) ([]organization.DepartmentView, error)
	ListPositions(context.Context, identity.TenantID) ([]organization.PositionView, error)
	CreateDepartment(context.Context, organization.WriteContext, *organization.DepartmentID, string, int) (organization.DepartmentID, error)
	UpdateDepartment(context.Context, organization.WriteContext, organization.DepartmentID, string, int) error
	MoveDepartment(context.Context, organization.WriteContext, organization.DepartmentID, organization.DepartmentID) error
	DeleteDepartment(context.Context, organization.WriteContext, organization.DepartmentID) error
	CreatePosition(context.Context, organization.WriteContext, string) (organization.PositionID, error)
	UpdatePosition(context.Context, organization.WriteContext, organization.PositionID, string, organization.PositionStatus) error
	AssignUser(context.Context, organization.WriteContext, identity.UserID, organization.DepartmentID, *organization.PositionID) error
}

// UpdateDepartment changes a department's display fields.
func (h *OrganizationHandler) UpdateDepartment(c *gin.Context, departmentID generated.DepartmentId, params generated.UpdateDepartmentParams) {
	if !h.csrf(c, params.XCSRFToken) {
		return
	}
	actor, ok := h.authorizedActor(c, authorization.Permission{Resource: authorization.ResourceDepartments, Action: authorization.ActionUpdate})
	if !ok {
		return
	}
	scope, ok := h.resolvedScope(c, actor, authorization.Permission{Resource: authorization.ResourceDepartments, Action: authorization.ActionUpdate})
	if !ok {
		return
	}
	var request generated.UpdateDepartmentRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		h.security.Problem(c, http.StatusBadRequest, "invalid request")
		return
	}
	err := h.service.UpdateDepartment(c.Request.Context(), writeContext(c, actor, scope), organization.DepartmentID(departmentID.String()), request.Name, request.SortOrder)
	if errors.Is(err, organization.ErrNotFound) {
		h.security.Problem(c, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		h.security.Problem(c, http.StatusBadRequest, "invalid request")
		return
	}
	c.Status(http.StatusNoContent)
}

// OrganizationHandler serves tenant-scoped organization operations.
type OrganizationHandler struct {
	service    Service
	authorizer Authorizer
	actors     ActorResolver
	security   *apihttp.Security
}

// NewHandler constructs the organization HTTP adapter.
func NewHandler(service Service, authorizer Authorizer, actors ActorResolver, security *apihttp.Security) *OrganizationHandler {
	return &OrganizationHandler{service: service, authorizer: authorizer, actors: actors, security: security}
}

// ListDepartments lists departments in the authenticated tenant.
func (h *OrganizationHandler) ListDepartments(c *gin.Context) {
	actor, ok := h.authorizedActor(c, authorization.Permission{Resource: authorization.ResourceDepartments, Action: authorization.ActionRead})
	if !ok {
		return
	}
	resolved, err := h.authorizer.ResolveDataScope(c.Request.Context(), actor, authorization.Permission{Resource: authorization.ResourceDepartments, Action: authorization.ActionRead})
	if errors.Is(err, authorization.ErrDenied) {
		h.security.Problem(c, http.StatusForbidden, "forbidden")
		return
	}
	if err != nil {
		h.security.Problem(c, http.StatusInternalServerError, "internal server error")
		return
	}
	custom := make([]organization.DepartmentID, 0, len(resolved.CustomDepartmentIDs))
	for _, id := range resolved.CustomDepartmentIDs {
		custom = append(custom, organization.DepartmentID(id))
	}
	scope := organization.DataScope{ActorID: actor.UserID, All: resolved.All, Self: resolved.Self, Department: resolved.Department, DepartmentAndDescendants: resolved.DepartmentAndDescendants, CustomDepartmentIDs: custom}
	departments, err := h.service.ListDepartments(c.Request.Context(), actor.TenantID, scope)
	if err != nil {
		h.security.Problem(c, http.StatusInternalServerError, "internal server error")
		return
	}
	response := make([]generated.Department, 0, len(departments))
	for _, department := range departments {
		id, err := uuid.Parse(string(department.ID))
		if err != nil {
			h.security.Problem(c, http.StatusInternalServerError, "internal server error")
			return
		}
		item := generated.Department{Id: id, Name: department.Name, SortOrder: department.SortOrder}
		if department.ParentID != nil {
			parentID, err := uuid.Parse(string(*department.ParentID))
			if err != nil {
				h.security.Problem(c, http.StatusInternalServerError, "internal server error")
				return
			}
			item.ParentId = &parentID
		}
		response = append(response, item)
	}
	c.JSON(http.StatusOK, response)
}

// CreateDepartment creates a department in the authenticated tenant.
func (h *OrganizationHandler) CreateDepartment(c *gin.Context, params generated.CreateDepartmentParams) {
	if !h.csrf(c, params.XCSRFToken) {
		return
	}
	actor, ok := h.authorizedActor(c, authorization.Permission{Resource: authorization.ResourceDepartments, Action: authorization.ActionCreate})
	if !ok {
		return
	}
	scope, ok := h.resolvedScope(c, actor, authorization.Permission{Resource: authorization.ResourceDepartments, Action: authorization.ActionCreate})
	if !ok {
		return
	}
	var request generated.CreateDepartmentRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		h.security.Problem(c, http.StatusBadRequest, "invalid request")
		return
	}
	parentID := organization.DepartmentID(request.ParentId.String())
	id, err := h.service.CreateDepartment(c.Request.Context(), writeContext(c, actor, scope), &parentID, request.Name, request.SortOrder)
	if err != nil {
		h.security.Problem(c, http.StatusBadRequest, "invalid request")
		return
	}
	responseID, err := uuid.Parse(string(id))
	if err != nil {
		h.security.Problem(c, http.StatusInternalServerError, "internal server error")
		return
	}
	c.JSON(http.StatusCreated, generated.IdentifierResponse{Id: responseID})
}

// MoveDepartment changes a department parent within the authenticated tenant.
func (h *OrganizationHandler) MoveDepartment(c *gin.Context, departmentID generated.DepartmentId, params generated.MoveDepartmentParams) {
	if !h.csrf(c, params.XCSRFToken) {
		return
	}
	actor, ok := h.authorizedActor(c, authorization.Permission{Resource: authorization.ResourceDepartments, Action: authorization.ActionUpdate})
	if !ok {
		return
	}
	scope, ok := h.resolvedScope(c, actor, authorization.Permission{Resource: authorization.ResourceDepartments, Action: authorization.ActionUpdate})
	if !ok {
		return
	}
	var request generated.MoveDepartmentRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		h.security.Problem(c, http.StatusBadRequest, "invalid request")
		return
	}
	err := h.service.MoveDepartment(c.Request.Context(), writeContext(c, actor, scope), organization.DepartmentID(departmentID.String()), organization.DepartmentID(request.ParentId.String()))
	if errors.Is(err, organization.ErrNotFound) {
		h.security.Problem(c, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		h.security.Problem(c, http.StatusBadRequest, "invalid request")
		return
	}
	c.Status(http.StatusNoContent)
}

// DeleteDepartment removes an unused department in the authenticated tenant.
func (h *OrganizationHandler) DeleteDepartment(c *gin.Context, departmentID generated.DepartmentId, params generated.DeleteDepartmentParams) {
	if !h.csrf(c, params.XCSRFToken) {
		return
	}
	actor, ok := h.authorizedActor(c, authorization.Permission{Resource: authorization.ResourceDepartments, Action: authorization.ActionDelete})
	if !ok {
		return
	}
	scope, ok := h.resolvedScope(c, actor, authorization.Permission{Resource: authorization.ResourceDepartments, Action: authorization.ActionDelete})
	if !ok {
		return
	}
	err := h.service.DeleteDepartment(c.Request.Context(), writeContext(c, actor, scope), organization.DepartmentID(departmentID.String()))
	if errors.Is(err, organization.ErrNotFound) {
		h.security.Problem(c, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		h.security.Problem(c, http.StatusBadRequest, "invalid request")
		return
	}
	c.Status(http.StatusNoContent)
}

// ListPositions lists positions in the authenticated tenant.
func (h *OrganizationHandler) ListPositions(c *gin.Context) {
	actor, ok := h.authorizedActor(c, authorization.Permission{Resource: authorization.ResourcePositions, Action: authorization.ActionRead})
	if !ok {
		return
	}
	scope, ok := h.resolvedScope(c, actor, authorization.Permission{Resource: authorization.ResourcePositions, Action: authorization.ActionRead})
	if !ok || !scope.All {
		h.security.Problem(c, http.StatusForbidden, "forbidden")
		return
	}
	positions, err := h.service.ListPositions(c.Request.Context(), actor.TenantID)
	if err != nil {
		h.security.Problem(c, http.StatusInternalServerError, "internal server error")
		return
	}
	response := make([]generated.Position, 0, len(positions))
	for _, position := range positions {
		id, err := uuid.Parse(string(position.ID))
		if err != nil {
			h.security.Problem(c, http.StatusInternalServerError, "internal server error")
			return
		}
		response = append(response, generated.Position{Id: id, Name: position.Name, Status: generated.PositionStatus(position.Status)})
	}
	c.JSON(http.StatusOK, response)
}

// CreatePosition creates a position in the authenticated tenant.
func (h *OrganizationHandler) CreatePosition(c *gin.Context, params generated.CreatePositionParams) {
	if !h.csrf(c, params.XCSRFToken) {
		return
	}
	actor, ok := h.authorizedActor(c, authorization.Permission{Resource: authorization.ResourcePositions, Action: authorization.ActionCreate})
	if !ok {
		return
	}
	scope, ok := h.resolvedScope(c, actor, authorization.Permission{Resource: authorization.ResourcePositions, Action: authorization.ActionCreate})
	if !ok || !scope.All {
		h.security.Problem(c, http.StatusForbidden, "forbidden")
		return
	}
	var request generated.CreatePositionRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		h.security.Problem(c, http.StatusBadRequest, "invalid request")
		return
	}
	id, err := h.service.CreatePosition(c.Request.Context(), writeContext(c, actor, scope), request.Name)
	if err != nil {
		h.security.Problem(c, http.StatusBadRequest, "invalid request")
		return
	}
	responseID, err := uuid.Parse(string(id))
	if err != nil {
		h.security.Problem(c, http.StatusInternalServerError, "internal server error")
		return
	}
	c.JSON(http.StatusCreated, generated.IdentifierResponse{Id: responseID})
}

// UpdatePosition changes a tenant position's display fields and status.
func (h *OrganizationHandler) UpdatePosition(c *gin.Context, positionID generated.PositionId, params generated.UpdatePositionParams) {
	if !h.csrf(c, params.XCSRFToken) {
		return
	}
	actor, ok := h.authorizedActor(c, authorization.Permission{Resource: authorization.ResourcePositions, Action: authorization.ActionUpdate})
	if !ok {
		return
	}
	scope, ok := h.resolvedScope(c, actor, authorization.Permission{Resource: authorization.ResourcePositions, Action: authorization.ActionUpdate})
	if !ok || !scope.All {
		h.security.Problem(c, http.StatusForbidden, "forbidden")
		return
	}
	var request generated.UpdatePositionRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		h.security.Problem(c, http.StatusBadRequest, "invalid request")
		return
	}
	err := h.service.UpdatePosition(c.Request.Context(), writeContext(c, actor, scope), organization.PositionID(positionID.String()), request.Name, organization.PositionStatus(request.Status))
	if errors.Is(err, organization.ErrNotFound) {
		h.security.Problem(c, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		h.security.Problem(c, http.StatusBadRequest, "invalid request")
		return
	}
	c.Status(http.StatusNoContent)
}

// AssignUserOrganization replaces a user's primary organization assignment.
func (h *OrganizationHandler) AssignUserOrganization(c *gin.Context, userID generated.UserId, params generated.AssignUserOrganizationParams) {
	if !h.csrf(c, params.XCSRFToken) {
		return
	}
	actor, ok := h.authorizedActor(c, authorization.Permission{Resource: authorization.ResourceUserOrganization, Action: authorization.ActionUpdate})
	if !ok {
		return
	}
	scope, ok := h.resolvedScope(c, actor, authorization.Permission{Resource: authorization.ResourceUserOrganization, Action: authorization.ActionUpdate})
	if !ok {
		return
	}
	var request generated.AssignUserOrganizationRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		h.security.Problem(c, http.StatusBadRequest, "invalid request")
		return
	}
	departmentID := organization.DepartmentID(request.DepartmentId.String())
	var positionID *organization.PositionID
	if request.PositionId != nil {
		value := organization.PositionID(request.PositionId.String())
		positionID = &value
	}
	if err := h.service.AssignUser(c.Request.Context(), writeContext(c, actor, scope), identity.UserID(userID.String()), departmentID, positionID); err != nil {
		h.security.Problem(c, http.StatusBadRequest, "invalid request")
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *OrganizationHandler) resolvedScope(c *gin.Context, actor identity.Actor, permission authorization.Permission) (organization.DataScope, bool) {
	resolved, err := h.authorizer.ResolveDataScope(c.Request.Context(), actor, permission)
	if err != nil {
		if errors.Is(err, authorization.ErrDenied) {
			h.security.Problem(c, http.StatusForbidden, "forbidden")
		} else {
			h.security.Problem(c, http.StatusInternalServerError, "internal server error")
		}
		return organization.DataScope{}, false
	}
	custom := make([]organization.DepartmentID, 0, len(resolved.CustomDepartmentIDs))
	for _, id := range resolved.CustomDepartmentIDs {
		custom = append(custom, organization.DepartmentID(id))
	}
	return organization.DataScope{ActorID: actor.UserID, All: resolved.All, Self: resolved.Self, Department: resolved.Department, DepartmentAndDescendants: resolved.DepartmentAndDescendants, CustomDepartmentIDs: custom}, true
}

func (h *OrganizationHandler) authorizedActor(c *gin.Context, permission authorization.Permission) (identity.Actor, bool) {
	if h.service == nil || h.authorizer == nil || h.actors == nil {
		h.security.Problem(c, http.StatusServiceUnavailable, "service unavailable")
		return identity.Actor{}, false
	}
	actor, ok := h.actors.Actor(c)
	if !ok {
		return identity.Actor{}, false
	}
	if err := h.authorizer.Authorize(c.Request.Context(), actor, permission); err != nil {
		if errors.Is(err, authorization.ErrDenied) {
			h.security.Problem(c, http.StatusForbidden, "forbidden")
			return identity.Actor{}, false
		}
		h.security.Problem(c, http.StatusInternalServerError, "internal server error")
		return identity.Actor{}, false
	}
	return actor, true
}
func (h *OrganizationHandler) csrf(c *gin.Context, header string) bool {
	_, ok := h.security.CookieAndCSRF(c, apihttp.TenantRefreshCookie, apihttp.TenantCSRFCookie, header)
	return ok
}

func writeContext(c *gin.Context, actor identity.Actor, scope organization.DataScope) organization.WriteContext {
	return organization.WriteContext{Actor: actor, CorrelationID: c.GetHeader("X-Request-ID"), Scope: scope}
}
