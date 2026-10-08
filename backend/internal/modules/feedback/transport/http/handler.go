// Package http delivers restricted staff feedback intake.
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
	"github.com/psmMRFP/WhereToLive/backend/internal/api/generated"
	apihttp "github.com/psmMRFP/WhereToLive/backend/internal/api/transport"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/feedback"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/platformadmin"
)

// Service is the restricted application capability consumed by this adapter.
type Service interface {
	Categories(context.Context, platformadmin.Actor) ([]feedback.Category, error)
	List(context.Context, platformadmin.Actor, feedback.Query) (feedback.Page, error)
	Create(context.Context, feedback.WriteContext, feedback.Create) (feedback.Entry, error)
	Review(context.Context, feedback.WriteContext, string, feedback.Review) (feedback.Entry, error)
}

// ActorResolver authenticates the distinct platform audience.
type ActorResolver interface {
	Actor(*gin.Context) (platformadmin.Actor, bool)
}

// Handler serves only platform routes.
type Handler struct {
	service  Service
	actors   ActorResolver
	security *apihttp.Security
}

// NewHandler wires authorization and the module explicitly.
func NewHandler(service Service, actors ActorResolver, security *apihttp.Security) *Handler {
	return &Handler{service, actors, security}
}
func (h *Handler) actor(c *gin.Context) (platformadmin.Actor, bool) {
	if h.service == nil || h.actors == nil {
		h.security.Problem(c, 503, "service unavailable")
		return platformadmin.Actor{}, false
	}
	return h.actors.Actor(c)
}
func (h *Handler) writer(c *gin.Context, csrf string) (platformadmin.Actor, bool) {
	a, ok := h.actor(c)
	if !ok {
		return a, false
	}
	_, ok = h.security.CookieAndCSRF(c, apihttp.PlatformRefreshCookie, apihttp.PlatformCSRFCookie, csrf)
	return a, ok
}

// ListPlatformFeedbackCategories supplies the active form catalogue.
func (h *Handler) ListPlatformFeedbackCategories(c *gin.Context) {
	a, ok := h.actor(c)
	if !ok {
		return
	}
	items, err := h.service.Categories(c.Request.Context(), a)
	if h.problem(c, err) {
		return
	}
	result := []generated.FeedbackCategory{}
	for _, v := range items {
		result = append(result, generated.FeedbackCategory{Key: v.Key, Label: v.Label})
	}
	c.JSON(200, result)
}

// ListPlatformFeedback exposes no anonymous projection of raw submissions.
func (h *Handler) ListPlatformFeedback(c *gin.Context, p generated.ListPlatformFeedbackParams) {
	a, ok := h.actor(c)
	if !ok {
		return
	}
	q := feedback.Query{Limit: 20}
	if p.Status != nil {
		q.Status = string(*p.Status)
	}
	if p.Category != nil {
		q.Category = *p.Category
	}
	if p.Limit != nil {
		q.Limit = *p.Limit
	}
	if p.Offset != nil {
		q.Offset = *p.Offset
	}
	page, err := h.service.List(c.Request.Context(), a, q)
	if h.problem(c, err) {
		return
	}
	result := generated.FeedbackPage{Items: []generated.FeedbackEntry{}, NextOffset: page.NextOffset}
	for _, e := range page.Items {
		result.Items = append(result.Items, response(e))
	}
	c.JSON(200, result)
}

// CreatePlatformFeedback creates an original manual record.
func (h *Handler) CreatePlatformFeedback(c *gin.Context, p generated.CreatePlatformFeedbackParams) {
	a, ok := h.writer(c, p.XCSRFToken)
	if !ok {
		return
	}
	var r generated.CreateFeedbackRequest
	if !h.decode(c, &r, []string{"category", "placeId", "title", "message", "reason"}) {
		return
	}
	input := feedback.Create{Category: r.Category, Title: r.Title, Message: r.Message}
	if r.PlaceId != nil {
		id := r.PlaceId.String()
		input.PlaceID = &id
	}
	e, err := h.service.Create(c.Request.Context(), write(c, a, r.Reason), input)
	if h.problem(c, err) {
		return
	}
	c.JSON(201, response(e))
}

// ReviewPlatformFeedback enforces state and stale-edit checks in the application.
func (h *Handler) ReviewPlatformFeedback(c *gin.Context, id uuid.UUID, p generated.ReviewPlatformFeedbackParams) {
	a, ok := h.writer(c, p.XCSRFToken)
	if !ok {
		return
	}
	var r generated.ReviewFeedbackRequest
	if !h.decode(c, &r, []string{"expectedVersion", "status", "outcome", "reason"}) {
		return
	}
	e, err := h.service.Review(c.Request.Context(), write(c, a, r.Reason), id.String(), feedback.Review{Version: r.ExpectedVersion, Status: string(r.Status), Outcome: r.Outcome})
	if h.problem(c, err) {
		return
	}
	c.JSON(200, response(e))
}
func (h *Handler) decode(c *gin.Context, output any, required []string) bool {
	data, err := io.ReadAll(c.Request.Body)
	if err != nil {
		h.security.Problem(c, 400, "invalid request")
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	err = decoder.Decode(output)
	var extra any
	if err == nil && decoder.Decode(&extra) != io.EOF {
		err = feedback.ErrInvalid
	}
	var fields map[string]json.RawMessage
	if err == nil {
		err = json.Unmarshal(data, &fields)
	}
	if err == nil {
		for _, key := range required {
			if _, ok := fields[key]; !ok {
				err = feedback.ErrInvalid
				break
			}
		}
	}
	if err != nil {
		h.security.Problem(c, 400, "invalid request")
		return false
	}
	return true
}
func (h *Handler) problem(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	status := http.StatusInternalServerError
	title := "internal server error"
	switch {
	case errors.Is(err, feedback.ErrInvalid):
		status = 400
		title = "invalid request"
	case errors.Is(err, feedback.ErrDenied):
		status = 403
		title = "access denied"
	case errors.Is(err, feedback.ErrNotFound):
		status = 404
		title = "not found"
	case errors.Is(err, feedback.ErrConflict):
		status = 409
		title = "feedback conflict"
	}
	h.security.Problem(c, status, title)
	return true
}
func write(c *gin.Context, a platformadmin.Actor, reason string) feedback.WriteContext {
	return feedback.WriteContext{Actor: a, Reason: reason, CorrelationID: c.GetHeader("X-Request-ID")}
}
func response(e feedback.Entry) generated.FeedbackEntry {
	r := generated.FeedbackEntry{Id: uuid.MustParse(e.ID), Category: e.Category, Title: e.Title, Message: e.Message, Status: generated.FeedbackStatus(e.Status), Outcome: e.Outcome, Version: e.Version, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt}
	if e.PlaceID != nil {
		id := uuid.MustParse(*e.PlaceID)
		r.PlaceId = &id
	}
	return r
}
