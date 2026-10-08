package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/modura-dev/modura/backend/internal/api/generated"
	apihttp "github.com/modura-dev/modura/backend/internal/api/transport"
	"github.com/modura-dev/modura/backend/internal/modules/places"
	"github.com/modura-dev/modura/backend/internal/modules/platformadmin"
)

// PlatformService is the platform-only catalogue capability.
type PlatformService interface {
	List(context.Context, platformadmin.Actor, places.Query) (places.ManagedPage, error)
	Get(context.Context, platformadmin.Actor, string) (places.Entry, error)
	Create(context.Context, places.WriteContext, places.Create) (places.Entry, error)
	Update(context.Context, places.WriteContext, string, int64, places.Details) (places.Entry, error)
	Publish(context.Context, places.WriteContext, string, int64, bool) (places.Entry, error)
}

// PlatformActorResolver resolves the distinct platform session audience.
type PlatformActorResolver interface {
	Actor(*gin.Context) (platformadmin.Actor, bool)
}

// PlatformPlacesHandler handles private catalogue operations.
type PlatformPlacesHandler struct {
	service  PlatformService
	actors   PlatformActorResolver
	security *apihttp.Security
}

// NewPlatformHandler constructs a platform place adapter.
func NewPlatformHandler(service PlatformService, actors PlatformActorResolver, security *apihttp.Security) *PlatformPlacesHandler {
	return &PlatformPlacesHandler{service, actors, security}
}
func (h *PlatformPlacesHandler) actor(c *gin.Context) (platformadmin.Actor, bool) {
	if h.service == nil || h.actors == nil {
		h.security.Problem(c, 503, "service unavailable")
		return platformadmin.Actor{}, false
	}
	return h.actors.Actor(c)
}
func (h *PlatformPlacesHandler) writeActor(c *gin.Context, csrf string) (platformadmin.Actor, bool) {
	a, ok := h.actor(c)
	if !ok {
		return a, false
	}
	_, ok = h.security.CookieAndCSRF(c, apihttp.PlatformRefreshCookie, apihttp.PlatformCSRFCookie, csrf)
	return a, ok
}

// ListPlatformPlaces returns private draft and published metadata.
func (h *PlatformPlacesHandler) ListPlatformPlaces(c *gin.Context, p generated.ListPlatformPlacesParams) {
	a, ok := h.actor(c)
	if !ok {
		return
	}
	q := places.Query{Limit: 20}
	if p.Q != nil {
		q.Search = *p.Q
	}
	if p.Limit != nil {
		q.Limit = *p.Limit
	}
	if p.Offset != nil {
		q.Offset = *p.Offset
	}
	page, err := h.service.List(c.Request.Context(), a, q)
	if h.writeError(c, err) {
		return
	}
	r := generated.ManagedPlacePage{Items: []generated.ManagedPlace{}, NextOffset: page.NextOffset}
	for _, e := range page.Items {
		r.Items = append(r.Items, managedResponse(e))
	}
	c.JSON(200, r)
}

// GetPlatformPlace returns the private entry and edit version.
func (h *PlatformPlacesHandler) GetPlatformPlace(c *gin.Context, id uuid.UUID) {
	a, ok := h.actor(c)
	if !ok {
		return
	}
	e, err := h.service.Get(c.Request.Context(), a, id.String())
	if h.writeError(c, err) {
		return
	}
	c.JSON(200, managedResponse(e))
}

// CreatePlatformPlace inserts a draft; publication is separate.
func (h *PlatformPlacesHandler) CreatePlatformPlace(c *gin.Context, p generated.CreatePlatformPlaceParams) {
	a, ok := h.writeActor(c, p.XCSRFToken)
	if !ok {
		return
	}
	var r generated.CreatePlatformPlaceRequest
	if !h.decode(c, &r, []string{"slug", "type", "parentId", "countryCode", "details", "reason"}) {
		return
	}
	input := places.Create{Slug: r.Slug, Type: string(r.Type), CountryCode: r.CountryCode, Details: detailsInput(r.Details)}
	if r.ParentId != nil {
		parent := r.ParentId.String()
		input.ParentID = &parent
	}
	e, err := h.service.Create(c.Request.Context(), writeContext(c, a, r.Reason), input)
	if h.writeError(c, err) {
		return
	}
	c.JSON(201, managedResponse(e))
}

// UpdatePlatformPlace replaces only editable metadata and aliases.
func (h *PlatformPlacesHandler) UpdatePlatformPlace(c *gin.Context, id uuid.UUID, p generated.UpdatePlatformPlaceParams) {
	a, ok := h.writeActor(c, p.XCSRFToken)
	if !ok {
		return
	}
	var r generated.UpdatePlatformPlaceRequest
	if !h.decode(c, &r, []string{"expectedVersion", "details", "reason"}) {
		return
	}
	e, err := h.service.Update(c.Request.Context(), writeContext(c, a, r.Reason), id.String(), r.ExpectedVersion, detailsInput(r.Details))
	if h.writeError(c, err) {
		return
	}
	c.JSON(200, managedResponse(e))
}

// SetPlacePublication toggles publication with an expected version.
func (h *PlatformPlacesHandler) SetPlacePublication(c *gin.Context, id uuid.UUID, p generated.SetPlacePublicationParams) {
	a, ok := h.writeActor(c, p.XCSRFToken)
	if !ok {
		return
	}
	var r generated.SetPlacePublicationRequest
	if !h.decode(c, &r, []string{"expectedVersion", "published", "reason"}) {
		return
	}
	e, err := h.service.Publish(c.Request.Context(), writeContext(c, a, r.Reason), id.String(), r.ExpectedVersion, r.Published)
	if h.writeError(c, err) {
		return
	}
	c.JSON(200, managedResponse(e))
}
func writeContext(c *gin.Context, a platformadmin.Actor, reason string) places.WriteContext {
	return places.WriteContext{Actor: a, Reason: reason, CorrelationID: c.GetHeader("X-Request-ID")}
}
func (h *PlatformPlacesHandler) writeError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	status, title := http.StatusInternalServerError, "internal server error"
	switch {
	case errors.Is(err, places.ErrDenied):
		status, title = 403, "access denied"
	case errors.Is(err, places.ErrInvalidPlace), errors.Is(err, places.ErrInvalidQuery):
		status, title = 400, "invalid request"
	case errors.Is(err, places.ErrNotFound):
		status, title = 404, "not found"
	case errors.Is(err, places.ErrConflict):
		status, title = 409, "place conflict"
	}
	h.security.Problem(c, status, title)
	return true
}
func (h *PlatformPlacesHandler) decode(c *gin.Context, target any, required []string) bool {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		h.security.Problem(c, 400, "invalid request")
		return false
	}
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	if d.Decode(target) != nil || d.Decode(new(any)) != io.EOF {
		h.security.Problem(c, 400, "invalid request")
		return false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil {
		h.security.Problem(c, 400, "invalid request")
		return false
	}
	for _, key := range required {
		if _, ok := fields[key]; !ok {
			h.security.Problem(c, 400, "invalid request")
			return false
		}
	}
	if details, ok := fields["details"]; ok {
		var nested map[string]json.RawMessage
		_ = json.Unmarshal(details, &nested)
		for _, key := range []string{"name", "timezone", "latitude", "longitude", "currency", "languages", "aliases"} {
			if _, ok := nested[key]; !ok {
				h.security.Problem(c, 400, "invalid request")
				return false
			}
		}
		if bytes.Equal(bytes.TrimSpace(nested["languages"]), []byte("null")) || bytes.Equal(bytes.TrimSpace(nested["aliases"]), []byte("null")) {
			h.security.Problem(c, 400, "invalid request")
			return false
		}
	}
	if details, ok := fields["details"]; ok {
		var nested map[string]json.RawMessage
		_ = json.Unmarshal(details, &nested)
		var aliases []map[string]json.RawMessage
		if json.Unmarshal(nested["aliases"], &aliases) != nil {
			h.security.Problem(c, 400, "invalid request")
			return false
		}
		for _, alias := range aliases {
			for _, key := range []string{"locale", "name", "preferred"} {
				value, ok := alias[key]
				if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
					h.security.Problem(c, 400, "invalid request")
					return false
				}
			}
		}
	}
	if bytes.Equal(bytes.TrimSpace(fields["published"]), []byte("null")) {
		h.security.Problem(c, 400, "invalid request")
		return false
	}
	return true
}
func detailsInput(d generated.PlaceDetails) places.Details {
	r := places.Details{Name: d.Name, Timezone: d.Timezone, Latitude: d.Latitude, Longitude: d.Longitude, Currency: d.Currency, Languages: d.Languages, Aliases: []places.Alias{}}
	for _, a := range d.Aliases {
		r.Aliases = append(r.Aliases, places.Alias{Locale: a.Locale, Name: a.Name, Preferred: a.Preferred})
	}
	return r
}
func managedResponse(e places.Entry) generated.ManagedPlace {
	r := generated.ManagedPlace{Id: uuid.MustParse(e.ID), Slug: e.Slug, Type: generated.PublicPlaceType(e.Type), CountryCode: e.CountryCode, Version: e.Version, CoverageLevel: e.CoverageLevel, PublishedAt: e.PublishedAt, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt, Details: generated.PlaceDetails{Name: e.Name, Timezone: e.Timezone, Latitude: e.Latitude, Longitude: e.Longitude, Currency: e.Currency, Languages: e.Languages, Aliases: []generated.PlaceAlias{}}}
	if e.ParentID != nil {
		id := uuid.MustParse(*e.ParentID)
		r.ParentId = &id
	}
	for _, a := range e.Aliases {
		r.Details.Aliases = append(r.Details.Aliases, generated.PlaceAlias{Locale: a.Locale, Name: a.Name, Preferred: a.Preferred})
	}
	return r
}
