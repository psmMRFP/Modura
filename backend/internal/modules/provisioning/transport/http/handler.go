// Package http adapts tenant provisioning to the platform HTTP contract.
package http

import (
	"context"
	"errors"
	"net/http"
	"net/mail"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/psmMRFP/WhereToLive/backend/internal/api/generated"
	apihttp "github.com/psmMRFP/WhereToLive/backend/internal/api/transport"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/platformadmin"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/provisioning"
)

var tenantSlugPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

// ActorResolver authenticates a global platform request actor.
type ActorResolver interface {
	Actor(*gin.Context) (platformadmin.Actor, bool)
}

// Service is the provisioning application API consumed by this adapter.
type Service interface {
	Provision(context.Context, provisioning.Request) (provisioning.Result, error)
}

// ProvisioningHandler serves atomic platform tenant provisioning.
type ProvisioningHandler struct {
	service  Service
	actors   ActorResolver
	security *apihttp.Security
}

// NewHandler constructs the tenant provisioning HTTP adapter.
func NewHandler(service Service, actors ActorResolver, security *apihttp.Security) *ProvisioningHandler {
	return &ProvisioningHandler{service: service, actors: actors, security: security}
}

// ProvisionPlatformTenant creates a complete tenant graph idempotently.
func (h *ProvisioningHandler) ProvisionPlatformTenant(c *gin.Context, params generated.ProvisionPlatformTenantParams) {
	if h.service == nil || h.actors == nil {
		h.security.Problem(c, http.StatusServiceUnavailable, "service unavailable")
		return
	}
	if _, ok := h.security.CookieAndCSRF(c, apihttp.PlatformRefreshCookie, apihttp.PlatformCSRFCookie, params.XCSRFToken); !ok {
		return
	}
	actor, ok := h.actors.Actor(c)
	if !ok {
		return
	}
	var request generated.ProvisionTenantRequest
	if err := c.ShouldBindJSON(&request); err != nil || !validRequest(request) {
		h.security.Problem(c, http.StatusBadRequest, "invalid request")
		return
	}
	administratorEmail := ""
	if request.AdministratorEmail != nil {
		administratorEmail = string(*request.AdministratorEmail)
	}
	result, err := h.service.Provision(c.Request.Context(), provisioning.Request{
		IdempotencyKey:        params.IdempotencyKey.String(),
		Slug:                  request.Slug,
		DisplayName:           request.DisplayName,
		RootDepartmentName:    request.RootDepartmentName,
		AdministratorUsername: request.AdministratorUsername,
		AdministratorEmail:    administratorEmail,
		Actor:                 actor,
		Reason:                request.Reason,
		CorrelationID:         c.GetHeader("X-Request-ID"),
	})
	if errors.Is(err, provisioning.ErrIdempotencyConflict) {
		h.security.Problem(c, http.StatusConflict, "idempotency conflict")
		return
	}
	if err != nil {
		h.security.Problem(c, http.StatusInternalServerError, "internal server error")
		return
	}
	tenantID, err := uuid.Parse(string(result.TenantID))
	if err != nil {
		h.security.Problem(c, http.StatusInternalServerError, "internal server error")
		return
	}
	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}
	c.JSON(status, generated.ProvisionTenantResponse{TenantId: tenantID, Created: result.Created})
}

func validRequest(request generated.ProvisionTenantRequest) bool {
	if !tenantSlugPattern.MatchString(request.Slug) || !within(request.DisplayName, 128) || !within(request.RootDepartmentName, 128) || !within(request.AdministratorUsername, 128) || !within(request.Reason, 500) {
		return false
	}
	if request.AdministratorEmail == nil {
		return true
	}
	email := string(*request.AdministratorEmail)
	address, err := mail.ParseAddress(email)
	return err == nil && address.Address == email && utf8.RuneCountInString(email) <= 254
}

func within(value string, maximum int) bool {
	trimmed := strings.TrimSpace(value)
	return trimmed != "" && utf8.RuneCountInString(trimmed) <= maximum
}
