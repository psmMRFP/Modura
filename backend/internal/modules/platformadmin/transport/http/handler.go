// Package http adapts platform administrator authentication to HTTP.
package http

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/psmMRFP/WhereToLive/backend/internal/api/generated"
	apihttp "github.com/psmMRFP/WhereToLive/backend/internal/api/transport"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/platformadmin"
)

// Service is the platform administrator application API consumed by this adapter.
type Service interface {
	Login(context.Context, string, string) (platformadmin.Tokens, error)
	Refresh(context.Context, string) (platformadmin.Tokens, error)
	AuthenticateAccess(context.Context, string) (platformadmin.Actor, error)
	Logout(context.Context, platformadmin.Actor) error
}

// PlatformAdminHandler serves global administrator authentication.
type PlatformAdminHandler struct {
	service  Service
	security *apihttp.Security
}

// NewHandler constructs the platform administrator HTTP adapter.
func NewHandler(service Service, security *apihttp.Security) *PlatformAdminHandler {
	return &PlatformAdminHandler{service: service, security: security}
}

// PlatformLogin establishes a global administrator session.
func (h *PlatformAdminHandler) PlatformLogin(c *gin.Context) {
	if h.service == nil {
		h.security.Problem(c, http.StatusServiceUnavailable, "service unavailable")
		return
	}
	var request generated.PlatformLoginRequest
	if err := c.ShouldBindJSON(&request); err != nil || request.Password == nil {
		h.security.Problem(c, http.StatusBadRequest, "invalid request")
		return
	}
	tokens, err := h.service.Login(c.Request.Context(), request.Username, *request.Password)
	if err != nil {
		h.security.Problem(c, http.StatusUnauthorized, "authentication failed")
		return
	}
	h.writeTokens(c, tokens)
}

// PlatformRefresh rotates a global administrator refresh token.
func (h *PlatformAdminHandler) PlatformRefresh(c *gin.Context, params generated.PlatformRefreshParams) {
	if h.service == nil {
		h.security.Problem(c, http.StatusServiceUnavailable, "service unavailable")
		return
	}
	refresh, ok := h.security.CookieAndCSRF(c, apihttp.PlatformRefreshCookie, apihttp.PlatformCSRFCookie, params.XCSRFToken)
	if !ok {
		return
	}
	tokens, err := h.service.Refresh(c.Request.Context(), refresh)
	if err != nil {
		h.security.ClearCookies(c, apihttp.PlatformRefreshCookie, apihttp.PlatformCSRFCookie, "/api/platform")
		h.security.Problem(c, http.StatusUnauthorized, "authentication failed")
		return
	}
	h.writeTokens(c, tokens)
}

// PlatformLogout revokes the current platform session and expires its cookies.
func (h *PlatformAdminHandler) PlatformLogout(c *gin.Context, params generated.PlatformLogoutParams) {
	if _, ok := h.security.CookieAndCSRF(c, apihttp.PlatformRefreshCookie, apihttp.PlatformCSRFCookie, params.XCSRFToken); !ok {
		return
	}
	actor, ok := h.Actor(c)
	if !ok {
		return
	}
	if err := h.service.Logout(c.Request.Context(), actor); err != nil {
		h.security.Problem(c, http.StatusUnauthorized, "authentication failed")
		return
	}
	h.security.ClearCookies(c, apihttp.PlatformRefreshCookie, apihttp.PlatformCSRFCookie, "/api/platform")
	c.Status(http.StatusNoContent)
}

// Actor authenticates a platform bearer credential for downstream adapters.
func (h *PlatformAdminHandler) Actor(c *gin.Context) (platformadmin.Actor, bool) {
	if h.service == nil {
		h.security.Problem(c, http.StatusServiceUnavailable, "service unavailable")
		return platformadmin.Actor{}, false
	}
	token, ok := h.security.Bearer(c)
	if !ok {
		return platformadmin.Actor{}, false
	}
	actor, err := h.service.AuthenticateAccess(c.Request.Context(), token)
	if err != nil {
		h.security.Problem(c, http.StatusUnauthorized, "authentication failed")
		return platformadmin.Actor{}, false
	}
	return actor, true
}
func (h *PlatformAdminHandler) writeTokens(c *gin.Context, tokens platformadmin.Tokens) {
	h.security.WriteTokens(c, tokens.AccessToken, tokens.RefreshToken, tokens.ExpiresIn, tokens.RefreshExpiresIn, apihttp.PlatformRefreshCookie, apihttp.PlatformCSRFCookie, "/api/platform")
}
