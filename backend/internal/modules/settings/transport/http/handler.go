// Package http adapts settings use cases to the HTTP contract.
package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/psmMRFP/WhereToLive/backend/internal/api/generated"
	apihttp "github.com/psmMRFP/WhereToLive/backend/internal/api/transport"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/authorization"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/identity"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/settings"
)

// ActorResolver authenticates tenant-local actors.
type ActorResolver interface {
	Actor(*gin.Context) (identity.Actor, bool)
}

// Authorizer checks stable settings permissions.
type Authorizer interface {
	Authorize(context.Context, identity.Actor, authorization.Permission) error
}

// Service is the settings application API consumed by HTTP delivery.
type Service interface {
	ListDictionaries(context.Context, identity.Actor) ([]settings.Dictionary, error)
	ReplaceDictionary(context.Context, settings.WriteContext, string, string, int64, []settings.DictionaryItem) (settings.Dictionary, error)
	DeleteDictionary(context.Context, settings.WriteContext, string, int64) error
	ListConfigurations(context.Context, identity.Actor) ([]settings.Configuration, error)
	PutConfiguration(context.Context, settings.WriteContext, string, int64, json.RawMessage) (settings.Configuration, error)
}

// SettingsHandler serves tenant settings operations.
type SettingsHandler struct {
	service    Service
	authorizer Authorizer
	actors     ActorResolver
	security   *apihttp.Security
}

// NewHandler constructs the settings HTTP adapter.
func NewHandler(service Service, authorizer Authorizer, actors ActorResolver, security *apihttp.Security) *SettingsHandler {
	return &SettingsHandler{service: service, authorizer: authorizer, actors: actors, security: security}
}

// ListDictionaries returns effective dictionaries for the authenticated tenant.
func (h *SettingsHandler) ListDictionaries(c *gin.Context) {
	actor, ok := h.authorized(c, authorization.Permission{Resource: authorization.ResourceDictionaries, Action: authorization.ActionRead})
	if !ok {
		return
	}
	dictionaries, err := h.service.ListDictionaries(c.Request.Context(), actor)
	if err != nil {
		h.security.Problem(c, http.StatusInternalServerError, "internal server error")
		return
	}
	response := make([]generated.Dictionary, 0, len(dictionaries))
	for _, dictionary := range dictionaries {
		response = append(response, dictionaryResponse(dictionary))
	}
	c.JSON(http.StatusOK, response)
}

// ReplaceDictionary stores a complete tenant dictionary desired state.
func (h *SettingsHandler) ReplaceDictionary(c *gin.Context, code generated.DictionaryCode, params generated.ReplaceDictionaryParams) {
	if !h.csrf(c, params.XCSRFToken) {
		return
	}
	actor, ok := h.authorized(c, authorization.Permission{Resource: authorization.ResourceDictionaries, Action: authorization.ActionUpdate})
	if !ok {
		return
	}
	var request generated.ReplaceDictionaryRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		h.security.Problem(c, http.StatusBadRequest, "invalid request")
		return
	}
	items := make([]settings.DictionaryItem, 0, len(request.Items))
	for _, item := range request.Items {
		items = append(items, settings.DictionaryItem{Code: item.Code, Label: item.Label, SortOrder: item.SortOrder, Enabled: item.Enabled})
	}
	dictionary, err := h.service.ReplaceDictionary(c.Request.Context(), writeContext(c, actor), code, request.Name, request.ExpectedVersion, items)
	if h.writeError(c, err) {
		return
	}
	c.JSON(http.StatusOK, dictionaryResponse(dictionary))
}

// DeleteDictionary removes only a tenant-owned dictionary.
func (h *SettingsHandler) DeleteDictionary(c *gin.Context, code generated.DictionaryCode, params generated.DeleteDictionaryParams) {
	if !h.csrf(c, params.XCSRFToken) {
		return
	}
	actor, ok := h.authorized(c, authorization.Permission{Resource: authorization.ResourceDictionaries, Action: authorization.ActionDelete})
	if !ok {
		return
	}
	if h.writeError(c, h.service.DeleteDictionary(c.Request.Context(), writeContext(c, actor), code, params.ExpectedVersion)) {
		return
	}
	c.Status(http.StatusNoContent)
}

// ListConfigurations returns effective non-secret tenant configuration.
func (h *SettingsHandler) ListConfigurations(c *gin.Context) {
	actor, ok := h.authorized(c, authorization.Permission{Resource: authorization.ResourceConfigurations, Action: authorization.ActionRead})
	if !ok {
		return
	}
	configurations, err := h.service.ListConfigurations(c.Request.Context(), actor)
	if err != nil {
		h.security.Problem(c, http.StatusInternalServerError, "internal server error")
		return
	}
	response := make([]generated.Configuration, 0, len(configurations))
	for _, configuration := range configurations {
		item, ok := configurationResponse(configuration)
		if !ok {
			h.security.Problem(c, http.StatusInternalServerError, "internal server error")
			return
		}
		response = append(response, item)
	}
	c.JSON(http.StatusOK, response)
}

// PutConfiguration stores one eligible tenant configuration override.
func (h *SettingsHandler) PutConfiguration(c *gin.Context, key generated.ConfigurationKey, params generated.PutConfigurationParams) {
	if !h.csrf(c, params.XCSRFToken) {
		return
	}
	actor, ok := h.authorized(c, authorization.Permission{Resource: authorization.ResourceConfigurations, Action: authorization.ActionUpdate})
	if !ok {
		return
	}
	var request generated.PutConfigurationRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		h.security.Problem(c, http.StatusBadRequest, "invalid request")
		return
	}
	value, err := json.Marshal(request.Value)
	if err != nil {
		h.security.Problem(c, http.StatusBadRequest, "invalid request")
		return
	}
	configuration, err := h.service.PutConfiguration(c.Request.Context(), writeContext(c, actor), key, request.ExpectedVersion, value)
	if h.writeError(c, err) {
		return
	}
	response, ok := configurationResponse(configuration)
	if !ok {
		h.security.Problem(c, http.StatusInternalServerError, "internal server error")
		return
	}
	c.JSON(http.StatusOK, response)
}

func (h *SettingsHandler) authorized(c *gin.Context, permission authorization.Permission) (identity.Actor, bool) {
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
		} else {
			h.security.Problem(c, http.StatusInternalServerError, "internal server error")
		}
		return identity.Actor{}, false
	}
	return actor, true
}

func (h *SettingsHandler) csrf(c *gin.Context, header string) bool {
	_, ok := h.security.CookieAndCSRF(c, apihttp.TenantRefreshCookie, apihttp.TenantCSRFCookie, header)
	return ok
}

func (h *SettingsHandler) writeError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, settings.ErrNotFound):
		h.security.Problem(c, http.StatusNotFound, "not found")
	case errors.Is(err, settings.ErrConflict):
		h.security.Problem(c, http.StatusConflict, "version conflict")
	case errors.Is(err, settings.ErrNotOverridable):
		h.security.Problem(c, http.StatusForbidden, "forbidden")
	default:
		h.security.Problem(c, http.StatusBadRequest, "invalid request")
	}
	return true
}

func writeContext(c *gin.Context, actor identity.Actor) settings.WriteContext {
	return settings.WriteContext{Actor: actor, CorrelationID: c.GetHeader("X-Request-ID")}
}

func dictionaryResponse(dictionary settings.Dictionary) generated.Dictionary {
	items := make([]generated.DictionaryItem, 0, len(dictionary.Items))
	for _, item := range dictionary.Items {
		items = append(items, generated.DictionaryItem{Code: item.Code, Label: item.Label, SortOrder: item.SortOrder, Enabled: item.Enabled})
	}
	return generated.Dictionary{Code: dictionary.Code, Name: dictionary.Name, Source: generated.SettingSource(dictionary.Source), Version: dictionary.Version, Items: items}
}

func configurationResponse(configuration settings.Configuration) (generated.Configuration, bool) {
	var value any
	if err := json.Unmarshal(configuration.Value, &value); err != nil {
		return generated.Configuration{}, false
	}
	return generated.Configuration{Key: configuration.Key, Name: configuration.Name, ValueType: generated.ConfigurationValueType(configuration.ValueType), TenantOverridable: configuration.TenantOverridable, Source: generated.SettingSource(configuration.Source), Version: configuration.Version, Value: value}, true
}
