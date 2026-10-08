package http

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/psmMRFP/WhereToLive/backend/internal/api/generated"
	apihttp "github.com/psmMRFP/WhereToLive/backend/internal/api/transport"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/audit"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/identity"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/platformadmin"
)

// PlatformActorResolver authenticates a global platform request actor.
type PlatformActorResolver interface {
	Actor(*gin.Context) (platformadmin.Actor, bool)
}

// PlatformReader is the global audit query API consumed by platform delivery.
type PlatformReader interface {
	ListPlatform(context.Context, audit.PlatformQuery) ([]audit.Record, error)
}

// PlatformAuditHandler serves global audit queries for platform administrators.
type PlatformAuditHandler struct {
	service  PlatformReader
	actors   PlatformActorResolver
	security *apihttp.Security
}

// NewPlatformHandler constructs the platform audit HTTP adapter.
func NewPlatformHandler(service PlatformReader, actors PlatformActorResolver, security *apihttp.Security) *PlatformAuditHandler {
	return &PlatformAuditHandler{service: service, actors: actors, security: security}
}

// ListPlatformAuditEvents returns a bounded redacted page across tenants.
func (h *PlatformAuditHandler) ListPlatformAuditEvents(c *gin.Context, params generated.ListPlatformAuditEventsParams) {
	if h.service == nil || h.actors == nil {
		h.security.Problem(c, http.StatusServiceUnavailable, "service unavailable")
		return
	}
	if _, ok := h.actors.Actor(c); !ok {
		return
	}
	query := audit.PlatformQuery{Limit: 50}
	if params.TenantId != nil {
		query.TenantID = identity.TenantID(params.TenantId.String())
	}
	if params.Action != nil {
		query.Action = *params.Action
	}
	if params.Resource != nil {
		query.Resource = *params.Resource
	}
	if params.Limit != nil {
		query.Limit = *params.Limit
	}
	if params.Offset != nil {
		query.Offset = *params.Offset
	}
	records, err := h.service.ListPlatform(c.Request.Context(), query)
	if err != nil {
		h.security.Problem(c, http.StatusBadRequest, "invalid request")
		return
	}
	response := make([]generated.PlatformAuditEvent, 0, len(records))
	for _, record := range records {
		item, ok := platformAuditResponse(record)
		if !ok {
			h.security.Problem(c, http.StatusInternalServerError, "internal server error")
			return
		}
		response = append(response, item)
	}
	c.JSON(http.StatusOK, response)
}

func platformAuditResponse(record audit.Record) (generated.PlatformAuditEvent, bool) {
	id, err := uuid.Parse(record.ID)
	if err != nil {
		return generated.PlatformAuditEvent{}, false
	}
	actorID, err := uuid.Parse(record.ActorID)
	if err != nil {
		return generated.PlatformAuditEvent{}, false
	}
	resourceID, err := uuid.Parse(record.ResourceID)
	if err != nil {
		return generated.PlatformAuditEvent{}, false
	}
	item := generated.PlatformAuditEvent{Id: id, ActorType: generated.PlatformAuditEventActorType(record.ActorType), ActorId: actorID, Action: record.Action, Resource: record.Resource, ResourceId: resourceID, Reason: record.Reason, Result: generated.PlatformAuditEventResult(record.Result), CorrelationId: record.CorrelationID, OccurredAt: record.OccurredAt}
	if record.TenantID != "" {
		tenantID, err := uuid.Parse(string(record.TenantID))
		if err != nil {
			return generated.PlatformAuditEvent{}, false
		}
		item.TenantId = &tenantID
	}
	if len(record.BeforeState) > 0 {
		if err := json.Unmarshal(record.BeforeState, &item.BeforeState); err != nil {
			return generated.PlatformAuditEvent{}, false
		}
	}
	if len(record.AfterState) > 0 {
		if err := json.Unmarshal(record.AfterState, &item.AfterState); err != nil {
			return generated.PlatformAuditEvent{}, false
		}
	}
	return item, true
}
