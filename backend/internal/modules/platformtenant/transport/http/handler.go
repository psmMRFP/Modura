// Package http adapts platform tenant lifecycle use cases to HTTP.
package http

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/psmMRFP/WhereToLive/backend/internal/api/generated"
	apihttp "github.com/psmMRFP/WhereToLive/backend/internal/api/transport"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/identity"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/platformadmin"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/platformtenant"
)

// ActorResolver authenticates a global platform request actor.
type ActorResolver interface {
	Actor(*gin.Context) (platformadmin.Actor, bool)
}

// Service is the tenant lifecycle application API consumed by this adapter.
type Service interface {
	List(context.Context, platformadmin.Actor) ([]platformtenant.Tenant, error)
	UpdateProfile(context.Context, platformadmin.Actor, identity.TenantID, string, time.Time, string, string) error
	Suspend(context.Context, platformadmin.Actor, identity.TenantID, string, string) error
	Reactivate(context.Context, platformadmin.Actor, identity.TenantID, string, string) error
}

// UpdatePlatformTenant changes mutable tenant profile data with optimistic locking.
func (h *PlatformTenantHandler) UpdatePlatformTenant(c *gin.Context, tenantID generated.TenantId, params generated.UpdatePlatformTenantParams) {
	if _, ok := h.security.CookieAndCSRF(c, apihttp.PlatformRefreshCookie, apihttp.PlatformCSRFCookie, params.XCSRFToken); !ok {
		return
	}
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	var request generated.UpdatePlatformTenantRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		h.security.Problem(c, http.StatusBadRequest, "invalid request")
		return
	}
	err := h.service.UpdateProfile(c.Request.Context(), actor, identity.TenantID(tenantID.String()), request.DisplayName, request.ExpectedUpdatedAt, request.Reason, c.GetHeader("X-Request-ID"))
	if errors.Is(err, platformtenant.ErrNotFound) {
		h.security.Problem(c, http.StatusNotFound, "not found")
		return
	}
	if errors.Is(err, platformtenant.ErrConflict) {
		h.security.Problem(c, http.StatusConflict, "conflict")
		return
	}
	if err != nil {
		h.security.Problem(c, http.StatusBadRequest, "invalid request")
		return
	}
	c.Status(http.StatusNoContent)
}

// PlatformTenantHandler serves audited global tenant lifecycle operations.
type PlatformTenantHandler struct {
	service  Service
	actors   ActorResolver
	security *apihttp.Security
}

// NewHandler constructs the platform tenant HTTP adapter.
func NewHandler(service Service, actors ActorResolver, security *apihttp.Security) *PlatformTenantHandler {
	return &PlatformTenantHandler{service: service, actors: actors, security: security}
}

// ListPlatformTenants lists tenants for a verified global actor.
func (h *PlatformTenantHandler) ListPlatformTenants(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	tenants, err := h.service.List(c.Request.Context(), actor)
	if err != nil {
		h.security.Problem(c, http.StatusInternalServerError, "internal server error")
		return
	}
	response := make([]generated.PlatformTenant, 0, len(tenants))
	for _, tenant := range tenants {
		id, err := uuid.Parse(string(tenant.ID))
		if err != nil {
			h.security.Problem(c, http.StatusInternalServerError, "internal server error")
			return
		}
		response = append(response, generated.PlatformTenant{Id: id, Slug: tenant.Slug, DisplayName: tenant.DisplayName, Status: generated.PlatformTenantStatus(tenant.Status), CreatedAt: tenant.CreatedAt, UpdatedAt: tenant.UpdatedAt})
	}
	c.JSON(http.StatusOK, response)
}

// SuspendPlatformTenant suspends a tenant with durable audit evidence.
func (h *PlatformTenantHandler) SuspendPlatformTenant(c *gin.Context, tenantID generated.TenantId, params generated.SuspendPlatformTenantParams) {
	h.changeStatus(c, tenantID, params.XCSRFToken, h.service.Suspend)
}

// ReactivatePlatformTenant restores a tenant with durable audit evidence.
func (h *PlatformTenantHandler) ReactivatePlatformTenant(c *gin.Context, tenantID generated.TenantId, params generated.ReactivatePlatformTenantParams) {
	h.changeStatus(c, tenantID, params.XCSRFToken, h.service.Reactivate)
}
func (h *PlatformTenantHandler) changeStatus(c *gin.Context, tenantID generated.TenantId, csrf string, change func(context.Context, platformadmin.Actor, identity.TenantID, string, string) error) {
	if _, ok := h.security.CookieAndCSRF(c, apihttp.PlatformRefreshCookie, apihttp.PlatformCSRFCookie, csrf); !ok {
		return
	}
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	var request generated.TenantLifecycleRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		h.security.Problem(c, http.StatusBadRequest, "invalid request")
		return
	}
	err := change(c.Request.Context(), actor, identity.TenantID(tenantID.String()), request.Reason, c.GetHeader("X-Request-ID"))
	if errors.Is(err, platformtenant.ErrNotFound) {
		h.security.Problem(c, http.StatusNotFound, "not found")
		return
	}
	if errors.Is(err, platformtenant.ErrInvalidTransition) {
		h.security.Problem(c, http.StatusBadRequest, "invalid request")
		return
	}
	if err != nil {
		h.security.Problem(c, http.StatusInternalServerError, "internal server error")
		return
	}
	c.Status(http.StatusNoContent)
}
func (h *PlatformTenantHandler) actor(c *gin.Context) (platformadmin.Actor, bool) {
	if h.service == nil || h.actors == nil {
		h.security.Problem(c, http.StatusServiceUnavailable, "service unavailable")
		return platformadmin.Actor{}, false
	}
	return h.actors.Actor(c)
}
