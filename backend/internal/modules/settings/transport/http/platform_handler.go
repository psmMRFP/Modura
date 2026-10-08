package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/psmMRFP/WhereToLive/backend/internal/api/generated"
	apihttp "github.com/psmMRFP/WhereToLive/backend/internal/api/transport"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/platformadmin"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/settings"
)

// PlatformActorResolver authenticates global platform requests.
type PlatformActorResolver interface {
	Actor(*gin.Context) (platformadmin.Actor, bool)
}

// PlatformService is the global settings API consumed by HTTP delivery.
type PlatformService interface {
	ListGlobalDictionaries(context.Context, platformadmin.Actor) ([]settings.Dictionary, error)
	ReplaceGlobalDictionary(context.Context, settings.PlatformWriteContext, string, string, int64, []settings.DictionaryItem) (settings.Dictionary, error)
	ListGlobalConfigurations(context.Context, platformadmin.Actor) ([]settings.Configuration, error)
	PutGlobalConfiguration(context.Context, settings.PlatformWriteContext, string, string, string, bool, int64, json.RawMessage) (settings.Configuration, error)
}

// PlatformSettingsHandler serves global settings operations.
type PlatformSettingsHandler struct {
	service  PlatformService
	actors   PlatformActorResolver
	security *apihttp.Security
}

// NewPlatformHandler constructs the global settings adapter.
func NewPlatformHandler(service PlatformService, actors PlatformActorResolver, security *apihttp.Security) *PlatformSettingsHandler {
	return &PlatformSettingsHandler{service: service, actors: actors, security: security}
}

// ListPlatformDictionaries returns all platform-owned dictionaries.
func (h *PlatformSettingsHandler) ListPlatformDictionaries(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	items, err := h.service.ListGlobalDictionaries(c.Request.Context(), actor)
	if err != nil {
		h.security.Problem(c, http.StatusInternalServerError, "internal server error")
		return
	}
	response := make([]generated.Dictionary, 0, len(items))
	for _, item := range items {
		response = append(response, dictionaryResponse(item))
	}
	c.JSON(http.StatusOK, response)
}

// ReplacePlatformDictionary stores a complete global dictionary desired state.
func (h *PlatformSettingsHandler) ReplacePlatformDictionary(c *gin.Context, code generated.DictionaryCode, params generated.ReplacePlatformDictionaryParams) {
	if !h.csrf(c, params.XCSRFToken) {
		return
	}
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	var request generated.ReplacePlatformDictionaryRequest
	if c.ShouldBindJSON(&request) != nil {
		h.security.Problem(c, http.StatusBadRequest, "invalid request")
		return
	}
	items := make([]settings.DictionaryItem, 0, len(request.Items))
	for _, item := range request.Items {
		items = append(items, settings.DictionaryItem{Code: item.Code, Label: item.Label, SortOrder: item.SortOrder, Enabled: item.Enabled})
	}
	result, err := h.service.ReplaceGlobalDictionary(c.Request.Context(), platformWriteContext(c, actor, request.Reason), code, request.Name, request.ExpectedVersion, items)
	if h.writeError(c, err) {
		return
	}
	c.JSON(http.StatusOK, dictionaryResponse(result))
}

// ListPlatformConfigurations returns global definitions and defaults.
func (h *PlatformSettingsHandler) ListPlatformConfigurations(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	items, err := h.service.ListGlobalConfigurations(c.Request.Context(), actor)
	if err != nil {
		h.security.Problem(c, http.StatusInternalServerError, "internal server error")
		return
	}
	response := make([]generated.Configuration, 0, len(items))
	for _, item := range items {
		converted, valid := configurationResponse(item)
		if !valid {
			h.security.Problem(c, http.StatusInternalServerError, "internal server error")
			return
		}
		response = append(response, converted)
	}
	c.JSON(http.StatusOK, response)
}

// PutPlatformConfiguration stores one global definition and default value.
func (h *PlatformSettingsHandler) PutPlatformConfiguration(c *gin.Context, key generated.ConfigurationKey, params generated.PutPlatformConfigurationParams) {
	if !h.csrf(c, params.XCSRFToken) {
		return
	}
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	var request generated.PutPlatformConfigurationRequest
	if c.ShouldBindJSON(&request) != nil {
		h.security.Problem(c, http.StatusBadRequest, "invalid request")
		return
	}
	value, err := json.Marshal(request.Value)
	if err != nil {
		h.security.Problem(c, http.StatusBadRequest, "invalid request")
		return
	}
	result, err := h.service.PutGlobalConfiguration(c.Request.Context(), platformWriteContext(c, actor, request.Reason), key, request.Name, string(request.ValueType), request.TenantOverridable, request.ExpectedVersion, value)
	if h.writeError(c, err) {
		return
	}
	response, valid := configurationResponse(result)
	if !valid {
		h.security.Problem(c, http.StatusInternalServerError, "internal server error")
		return
	}
	c.JSON(http.StatusOK, response)
}

func (h *PlatformSettingsHandler) actor(c *gin.Context) (platformadmin.Actor, bool) {
	if h.service == nil || h.actors == nil {
		h.security.Problem(c, http.StatusServiceUnavailable, "service unavailable")
		return platformadmin.Actor{}, false
	}
	return h.actors.Actor(c)
}

func (h *PlatformSettingsHandler) csrf(c *gin.Context, token string) bool {
	_, ok := h.security.CookieAndCSRF(c, apihttp.PlatformRefreshCookie, apihttp.PlatformCSRFCookie, token)
	return ok
}

func (h *PlatformSettingsHandler) writeError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, settings.ErrNotFound) {
		h.security.Problem(c, http.StatusNotFound, "not found")
	} else if errors.Is(err, settings.ErrConflict) {
		h.security.Problem(c, http.StatusConflict, "version conflict")
	} else {
		h.security.Problem(c, http.StatusBadRequest, "invalid request")
	}
	return true
}

func platformWriteContext(c *gin.Context, actor platformadmin.Actor, reason string) settings.PlatformWriteContext {
	return settings.PlatformWriteContext{Actor: actor, Reason: reason, CorrelationID: c.GetHeader("X-Request-ID")}
}
