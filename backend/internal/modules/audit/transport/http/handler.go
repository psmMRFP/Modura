// Package http adapts audit queries to the HTTP contract.
package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/psmMRFP/WhereToLive/backend/internal/api/generated"
	apihttp "github.com/psmMRFP/WhereToLive/backend/internal/api/transport"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/audit"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/authorization"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/identity"
)

// ActorResolver authenticates tenant-local actors.
type ActorResolver interface {
	Actor(*gin.Context) (identity.Actor, bool)
}

// Authorizer checks stable audit permissions.
type Authorizer interface {
	Authorize(context.Context, identity.Actor, authorization.Permission) error
}

// Service is the audit query API consumed by HTTP delivery.
type Service interface {
	List(context.Context, identity.TenantID, string, string, int, int) ([]audit.Record, error)
	Get(context.Context, identity.TenantID, string) (audit.Record, error)
}

// AuditHandler serves immutable tenant audit queries.
type AuditHandler struct {
	service    Service
	authorizer Authorizer
	actors     ActorResolver
	security   *apihttp.Security
}

// NewHandler constructs the audit HTTP adapter.
func NewHandler(service Service, authorizer Authorizer, actors ActorResolver, security *apihttp.Security) *AuditHandler {
	return &AuditHandler{service: service, authorizer: authorizer, actors: actors, security: security}
}

// ListAuditEvents returns a bounded redacted page in the authenticated tenant.
func (h *AuditHandler) ListAuditEvents(c *gin.Context, params generated.ListAuditEventsParams) {
	if h.service == nil || h.authorizer == nil || h.actors == nil {
		h.security.Problem(c, http.StatusServiceUnavailable, "service unavailable")
		return
	}
	actor, ok := h.actors.Actor(c)
	if !ok {
		return
	}
	permission := authorization.Permission{Resource: authorization.ResourceAuditEvents, Action: authorization.ActionRead}
	if err := h.authorizer.Authorize(c.Request.Context(), actor, permission); err != nil {
		if errors.Is(err, authorization.ErrDenied) {
			h.security.Problem(c, http.StatusForbidden, "forbidden")
		} else {
			h.security.Problem(c, http.StatusInternalServerError, "internal server error")
		}
		return
	}
	action, resource := "", ""
	limit, offset := 50, 0
	if params.Action != nil {
		action = *params.Action
	}
	if params.Resource != nil {
		resource = *params.Resource
	}
	if params.Limit != nil {
		limit = *params.Limit
	}
	if params.Offset != nil {
		offset = *params.Offset
	}
	records, err := h.service.List(c.Request.Context(), actor.TenantID, action, resource, limit, offset)
	if err != nil {
		h.security.Problem(c, http.StatusBadRequest, "invalid request")
		return
	}
	response := make([]generated.AuditEvent, 0, len(records))
	for _, record := range records {
		item, ok := auditResponse(record)
		if !ok {
			h.security.Problem(c, http.StatusInternalServerError, "internal server error")
			return
		}
		response = append(response, item)
	}
	c.JSON(http.StatusOK, response)
}

// GetAuditEvent returns one redacted event only within the authenticated tenant.
func (h *AuditHandler) GetAuditEvent(c *gin.Context, eventID generated.EventId) {
	if h.service == nil || h.authorizer == nil || h.actors == nil {
		h.security.Problem(c, http.StatusServiceUnavailable, "service unavailable")
		return
	}
	actor, ok := h.actors.Actor(c)
	if !ok {
		return
	}
	permission := authorization.Permission{Resource: authorization.ResourceAuditEvents, Action: authorization.ActionRead}
	if err := h.authorizer.Authorize(c.Request.Context(), actor, permission); err != nil {
		if errors.Is(err, authorization.ErrDenied) {
			h.security.Problem(c, http.StatusForbidden, "forbidden")
		} else {
			h.security.Problem(c, http.StatusInternalServerError, "internal server error")
		}
		return
	}
	record, err := h.service.Get(c.Request.Context(), actor.TenantID, eventID.String())
	if errors.Is(err, audit.ErrNotFound) {
		h.security.Problem(c, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		h.security.Problem(c, http.StatusInternalServerError, "internal server error")
		return
	}
	item, ok := auditResponse(record)
	if !ok {
		h.security.Problem(c, http.StatusInternalServerError, "internal server error")
		return
	}
	c.JSON(http.StatusOK, item)
}

func auditResponse(record audit.Record) (generated.AuditEvent, bool) {
	id, err := uuid.Parse(record.ID)
	if err != nil {
		return generated.AuditEvent{}, false
	}
	actorID, err := uuid.Parse(record.ActorID)
	if err != nil {
		return generated.AuditEvent{}, false
	}
	tenantID, err := uuid.Parse(string(record.TenantID))
	if err != nil {
		return generated.AuditEvent{}, false
	}
	resourceID, err := uuid.Parse(record.ResourceID)
	if err != nil {
		return generated.AuditEvent{}, false
	}
	item := generated.AuditEvent{Id: id, ActorType: generated.AuditEventActorType(record.ActorType), ActorId: actorID, TenantId: tenantID, Action: record.Action, Resource: record.Resource, ResourceId: resourceID, Reason: record.Reason, Result: generated.AuditEventResult(record.Result), CorrelationId: record.CorrelationID, OccurredAt: record.OccurredAt}
	if len(record.BeforeState) > 0 {
		if err := json.Unmarshal(record.BeforeState, &item.BeforeState); err != nil {
			return generated.AuditEvent{}, false
		}
	}
	if len(record.AfterState) > 0 {
		if err := json.Unmarshal(record.AfterState, &item.AfterState); err != nil {
			return generated.AuditEvent{}, false
		}
	}
	return item, true
}
