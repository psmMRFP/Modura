// Package http adapts identity application use cases to the HTTP contract.
package http

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/modura-dev/modura/backend/internal/api/generated"
	apihttp "github.com/modura-dev/modura/backend/internal/api/transport"
	"github.com/modura-dev/modura/backend/internal/modules/identity"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// Service is the identity application API consumed by this adapter.
type Service interface {
	Login(context.Context, string, string, string) (identity.Tokens, error)
	Refresh(context.Context, string) (identity.Tokens, error)
	AuthenticateAccess(context.Context, string) (identity.Actor, error)
	Logout(context.Context, identity.Actor) error
	LogoutAll(context.Context, identity.Actor) error
	ChangePassword(context.Context, identity.Actor, string, string, string) (identity.Tokens, error)
	Profile(context.Context, identity.Actor) (identity.Profile, error)
	UpdateProfile(context.Context, identity.Actor, string, *string, string) (identity.Profile, error)
	ConsumeOneTimeToken(context.Context, string, identity.OneTimePurpose, string) error
}

// GetMyProfile returns the authenticated user's self-service projection.
func (h *IdentityHandler) GetMyProfile(c *gin.Context) {
	actor, ok := h.Actor(c)
	if !ok {
		return
	}
	profile, err := h.service.Profile(c.Request.Context(), actor)
	if err != nil {
		h.security.Problem(c, http.StatusUnauthorized, "authentication failed")
		return
	}
	h.writeProfile(c, profile)
}

// UpdateMyProfile updates mutable self-service fields.
func (h *IdentityHandler) UpdateMyProfile(c *gin.Context, params generated.UpdateMyProfileParams) {
	if _, ok := h.security.CookieAndCSRF(c, apihttp.TenantRefreshCookie, apihttp.TenantCSRFCookie, params.XCSRFToken); !ok {
		return
	}
	actor, ok := h.Actor(c)
	if !ok {
		return
	}
	var request generated.UpdateUserProfileRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		h.security.Problem(c, http.StatusBadRequest, "invalid request")
		return
	}
	var email *string
	if request.Email != nil {
		value := string(*request.Email)
		email = &value
	}
	profile, err := h.service.UpdateProfile(c.Request.Context(), actor, request.Username, email, c.GetHeader("X-Request-ID"))
	if err != nil {
		h.security.Problem(c, http.StatusBadRequest, "invalid request")
		return
	}
	h.writeProfile(c, profile)
}

func (h *IdentityHandler) writeProfile(c *gin.Context, profile identity.Profile) {
	id, err := uuid.Parse(string(profile.ID))
	if err != nil {
		h.security.Problem(c, http.StatusInternalServerError, "internal server error")
		return
	}
	response := generated.UserProfile{Id: id, Username: profile.Username, Status: generated.UserProfileStatus(profile.Status), UpdatedAt: profile.UpdatedAt}
	if profile.Email != nil {
		value := openapi_types.Email(*profile.Email)
		response.Email = &value
	}
	c.JSON(http.StatusOK, response)
}

// IdentityHandler serves tenant-local authentication operations.
type IdentityHandler struct {
	service  Service
	security *apihttp.Security
}

// NewHandler constructs the identity HTTP adapter.
func NewHandler(service Service, security *apihttp.Security) *IdentityHandler {
	return &IdentityHandler{service: service, security: security}
}

// Login establishes a tenant-local session.
func (h *IdentityHandler) Login(c *gin.Context) {
	if h.service == nil {
		h.security.Problem(c, http.StatusServiceUnavailable, "service unavailable")
		return
	}
	var request generated.LoginRequest
	if err := c.ShouldBindJSON(&request); err != nil || request.Password == nil {
		h.security.Problem(c, http.StatusBadRequest, "invalid request")
		return
	}
	tokens, err := h.service.Login(c.Request.Context(), request.Tenant, request.Login, *request.Password)
	if err != nil {
		h.security.Problem(c, http.StatusUnauthorized, "authentication failed")
		return
	}
	h.writeTokens(c, tokens)
}

// Refresh rotates a tenant-local refresh token.
func (h *IdentityHandler) Refresh(c *gin.Context, params generated.RefreshParams) {
	refresh, ok := h.security.CookieAndCSRF(c, apihttp.TenantRefreshCookie, apihttp.TenantCSRFCookie, params.XCSRFToken)
	if !ok {
		return
	}
	tokens, err := h.service.Refresh(c.Request.Context(), refresh)
	if err != nil {
		h.clearCookies(c)
		h.security.Problem(c, http.StatusUnauthorized, "authentication failed")
		return
	}
	h.writeTokens(c, tokens)
}

// Logout revokes the current session.
func (h *IdentityHandler) Logout(c *gin.Context, params generated.LogoutParams) {
	if _, ok := h.security.CookieAndCSRF(c, apihttp.TenantRefreshCookie, apihttp.TenantCSRFCookie, params.XCSRFToken); !ok {
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
	h.clearCookies(c)
	c.Status(http.StatusNoContent)
}

// LogoutAll revokes every session for the current user.
func (h *IdentityHandler) LogoutAll(c *gin.Context, params generated.LogoutAllParams) {
	if _, ok := h.security.CookieAndCSRF(c, apihttp.TenantRefreshCookie, apihttp.TenantCSRFCookie, params.XCSRFToken); !ok {
		return
	}
	actor, ok := h.Actor(c)
	if !ok {
		return
	}
	if err := h.service.LogoutAll(c.Request.Context(), actor); err != nil {
		h.security.Problem(c, http.StatusUnauthorized, "authentication failed")
		return
	}
	h.clearCookies(c)
	c.Status(http.StatusNoContent)
}

// ChangePassword replaces credentials and rotates the current session.
func (h *IdentityHandler) ChangePassword(c *gin.Context, params generated.ChangePasswordParams) {
	refresh, ok := h.security.CookieAndCSRF(c, apihttp.TenantRefreshCookie, apihttp.TenantCSRFCookie, params.XCSRFToken)
	if !ok {
		return
	}
	actor, ok := h.Actor(c)
	if !ok {
		return
	}
	var request generated.ChangePasswordRequest
	if err := c.ShouldBindJSON(&request); err != nil || request.CurrentPassword == nil || request.NewPassword == nil {
		h.security.Problem(c, http.StatusBadRequest, "invalid request")
		return
	}
	tokens, err := h.service.ChangePassword(c.Request.Context(), actor, *request.CurrentPassword, *request.NewPassword, refresh)
	if err != nil {
		h.security.Problem(c, http.StatusUnauthorized, "authentication failed")
		return
	}
	h.writeTokens(c, tokens)
}

// AcceptInvitation consumes a single-use invitation credential.
func (h *IdentityHandler) AcceptInvitation(c *gin.Context) {
	h.consumeOneTimeCredential(c, identity.PurposeInvitation)
}

// ResetPassword consumes a single-use password reset credential.
func (h *IdentityHandler) ResetPassword(c *gin.Context) {
	h.consumeOneTimeCredential(c, identity.PurposePasswordReset)
}

// Actor authenticates the tenant-local bearer credential for downstream adapters.
func (h *IdentityHandler) Actor(c *gin.Context) (identity.Actor, bool) {
	if h.service == nil {
		h.security.Problem(c, http.StatusServiceUnavailable, "service unavailable")
		return identity.Actor{}, false
	}
	token, ok := h.security.Bearer(c)
	if !ok {
		return identity.Actor{}, false
	}
	actor, err := h.service.AuthenticateAccess(c.Request.Context(), token)
	if err != nil {
		h.security.Problem(c, http.StatusUnauthorized, "authentication failed")
		return identity.Actor{}, false
	}
	return actor, true
}

func (h *IdentityHandler) consumeOneTimeCredential(c *gin.Context, purpose identity.OneTimePurpose) {
	if h.service == nil {
		h.security.Problem(c, http.StatusServiceUnavailable, "service unavailable")
		return
	}
	var request generated.OneTimeCredentialRequest
	if err := c.ShouldBindJSON(&request); err != nil || request.Token == nil || request.NewPassword == nil {
		h.security.Problem(c, http.StatusBadRequest, "invalid request")
		return
	}
	if err := h.service.ConsumeOneTimeToken(c.Request.Context(), *request.Token, purpose, *request.NewPassword); err != nil {
		if errors.Is(err, identity.ErrInvalidPassword) {
			h.security.Problem(c, http.StatusBadRequest, "invalid request")
			return
		}
		h.security.Problem(c, http.StatusUnauthorized, "authentication failed")
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *IdentityHandler) writeTokens(c *gin.Context, tokens identity.Tokens) {
	h.security.WriteTokens(c, tokens.AccessToken, tokens.RefreshToken, tokens.ExpiresIn, tokens.RefreshExpiresIn, apihttp.TenantRefreshCookie, apihttp.TenantCSRFCookie, "/api/auth")
}
func (h *IdentityHandler) clearCookies(c *gin.Context) {
	h.security.ClearCookies(c, apihttp.TenantRefreshCookie, apihttp.TenantCSRFCookie, "/api/auth")
}
