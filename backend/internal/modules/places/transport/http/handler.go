// Package http delivers the anonymous places contract; it never resolves an actor.
package http

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/psmMRFP/WhereToLive/backend/internal/api/generated"
	apihttp "github.com/psmMRFP/WhereToLive/backend/internal/api/transport"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/places"
)

// Service is the public read capability consumed by HTTP.
type Service interface {
	Search(context.Context, places.Query) (places.Page, error)
	Get(context.Context, string, string) (places.Place, error)
}

// PlacesHandler serves only published global reads.
type PlacesHandler struct {
	service  Service
	security *apihttp.Security
}

// NewHandler constructs the anonymous place adapter.
func NewHandler(service Service, security *apihttp.Security) *PlacesHandler {
	return &PlacesHandler{service: service, security: security}
}

// SearchPublicPlaces implements bounded anonymous search.
func (h *PlacesHandler) SearchPublicPlaces(c *gin.Context, params generated.SearchPublicPlacesParams) {
	if h.service == nil {
		h.security.Problem(c, http.StatusServiceUnavailable, "service unavailable")
		return
	}
	query := places.Query{Locale: "en", Limit: 20}
	if params.Q != nil {
		query.Search = *params.Q
	}
	if params.Locale != nil {
		query.Locale = string(*params.Locale)
	}
	if params.Limit != nil {
		query.Limit = *params.Limit
	}
	if params.Offset != nil {
		query.Offset = *params.Offset
	}
	result, err := h.service.Search(c.Request.Context(), query)
	if h.writeError(c, err) {
		return
	}
	response := generated.PublicPlacePage{Items: make([]generated.PublicPlace, 0, len(result.Items)), NextOffset: result.NextOffset}
	for _, place := range result.Items {
		response.Items = append(response.Items, placeResponse(place))
	}
	c.JSON(http.StatusOK, response)
}

// GetPublicPlace exposes no distinction between a missing place and a draft.
func (h *PlacesHandler) GetPublicPlace(c *gin.Context, slug string, params generated.GetPublicPlaceParams) {
	if h.service == nil {
		h.security.Problem(c, http.StatusServiceUnavailable, "service unavailable")
		return
	}
	locale := "en"
	if params.Locale != nil {
		locale = string(*params.Locale)
	}
	result, err := h.service.Get(c.Request.Context(), slug, locale)
	if h.writeError(c, err) {
		return
	}
	c.JSON(http.StatusOK, placeResponse(result))
}

func (h *PlacesHandler) writeError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, places.ErrInvalidQuery):
		h.security.Problem(c, http.StatusBadRequest, "invalid request")
	case errors.Is(err, places.ErrNotFound):
		h.security.Problem(c, http.StatusNotFound, "not found")
	default:
		h.security.Problem(c, http.StatusInternalServerError, "internal server error")
	}
	return true
}

func placeResponse(p places.Place) generated.PublicPlace {
	result := generated.PublicPlace{Id: uuid.MustParse(p.ID), Slug: p.Slug, Name: p.Name, DisplayName: p.DisplayName, Type: generated.PublicPlaceType(p.Type), CountryCode: p.CountryCode, Timezone: p.Timezone, Latitude: p.Latitude, Longitude: p.Longitude, Currency: p.Currency, Languages: p.Languages, CoverageLevel: p.CoverageLevel, PublishedAt: p.PublishedAt}
	if p.ParentID != nil {
		id := uuid.MustParse(*p.ParentID)
		result.ParentId = &id
	}
	return result
}
