// Package http adapts identity application use cases to the HTTP contract.
package http

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/psmMRFP/WhereToLive/backend/internal/api/generated"
	apihttp "github.com/psmMRFP/WhereToLive/backend/internal/api/transport"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/authorization"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/identity"
)

// Service is the identity application API consumed by this adapter.
type Service interface {
	Login(context.Context, string, string, string, string) (identity.Tokens, error)
	Refresh(context.Context, string, string) (identity.Tokens, error)
	AuthenticateAccess(context.Context, string) (identity.Actor, error)
	Logout(context.Context, identity.Actor, string) error
	LogoutAll(context.Context, identity.Actor, string) error
	ChangePassword(context.Context, identity.Actor, string, string, string, string) (identity.Tokens, error)
	Profile(context.Context, identity.Actor) (identity.Profile, error)
	ListUsers(context.Context, identity.TenantID) ([]identity.TenantUser, error)
	GetUser(context.Context, identity.TenantID, identity.UserID) (identity.TenantUser, error)
	DisableUser(context.Context, identity.Actor, identity.UserID, string, string) (identity.TenantUser, error)
	UnlockUser(context.Context, identity.Actor, identity.UserID, string) (identity.TenantUser, error)
	UpdateProfile(context.Context, identity.Actor, string, *string, string) (identity.Profile, error)
	ConsumeOneTimeToken(context.Context, string, identity.OneTimePurpose, string, string) error
}

// Authorizer checks canonical tenant user permissions.
type Authorizer interface {
	Authorize(context.Context, identity.Actor, authorization.Permission) error
}

// ListTenantUsers returns the current tenant's account catalogue.
func (h *IdentityHandler) ListTenantUsers(c *gin.Context) {
	actor, ok := h.authorizedUserManager(c, authorization.ActionRead)
	if !ok {
		return
	}
	users, err := h.service.ListUsers(c.Request.Context(), actor.TenantID)
	if err != nil {
		h.security.Problem(c, http.StatusInternalServerError, "internal server error")
		return
	}
	response := make([]generated.TenantUser, 0, len(users))
	for _, user := range users {
		item, ok := h.tenantUser(c, user)
		if !ok {
			return
		}
		response = append(response, item)
	}
	c.JSON(http.StatusOK, response)
}

// GetTenantUser returns one account only within the current tenant.
func (h *IdentityHandler) GetTenantUser(c *gin.Context, userID generated.UserId) {
	actor, ok := h.authorizedUserManager(c, authorization.ActionRead)
	if !ok {
		return
	}
	user, err := h.service.GetUser(c.Request.Context(), actor.TenantID, identity.UserID(userID.String()))
	if errors.Is(err, identity.ErrUserNotFound) {
		h.security.Problem(c, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		h.security.Problem(c, http.StatusInternalServerError, "internal server error")
		return
	}
	item, ok := h.tenantUser(c, user)
	if ok {
		c.JSON(http.StatusOK, item)
	}
}

// DisableTenantUser disables one tenant-owned account after an authorized
// management request.
func (h *IdentityHandler) DisableTenantUser(c *gin.Context, userID generated.UserId, params generated.DisableTenantUserParams) {
	if _, ok := h.security.CookieAndCSRF(c, apihttp.TenantRefreshCookie, apihttp.TenantCSRFCookie, params.XCSRFToken); !ok {
		return
	}
	actor, ok := h.authorizedUserManager(c, authorization.ActionUpdate)
	if !ok {
		return
	}
	var request generated.DisableUserRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		h.security.Problem(c, http.StatusBadRequest, "invalid request")
		return
	}
	user, err := h.service.DisableUser(c.Request.Context(), actor, identity.UserID(userID.String()), request.Reason, c.GetHeader("X-Request-ID"))
	h.writeTenantUser(c, user, err)
}

// UnlockTenantUser restores one abuse-locked tenant-owned account.
func (h *IdentityHandler) UnlockTenantUser(c *gin.Context, userID generated.UserId, params generated.UnlockTenantUserParams) {
	if _, ok := h.security.CookieAndCSRF(c, apihttp.TenantRefreshCookie, apihttp.TenantCSRFCookie, params.XCSRFToken); !ok {
		return
	}
	actor, ok := h.authorizedUserManager(c, authorization.ActionUpdate)
	if !ok {
		return
	}
	user, err := h.service.UnlockUser(c.Request.Context(), actor, identity.UserID(userID.String()), c.GetHeader("X-Request-ID"))
	h.writeTenantUser(c, user, err)
}

// writeTenantUser maps a user-management outcome onto the response or problem surface.
func (h *IdentityHandler) writeTenantUser(c *gin.Context, user identity.TenantUser, err error) {
	if errors.Is(err, identity.ErrUserNotFound) {
		h.security.Problem(c, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		h.security.Problem(c, http.StatusBadRequest, "invalid request")
		return
	}
	item, ok := h.tenantUser(c, user)
	if ok {
		c.JSON(http.StatusOK, item)
	}
}

func (h *IdentityHandler) authorizedUserManager(c *gin.Context, action authorization.Action) (identity.Actor, bool) {
	actor, ok := h.Actor(c)
	if !ok {
		return identity.Actor{}, false
	}
	if h.authorizer == nil {
		h.security.Problem(c, http.StatusServiceUnavailable, "service unavailable")
		return identity.Actor{}, false
	}
	err := h.authorizer.Authorize(c.Request.Context(), actor, authorization.Permission{Resource: authorization.ResourceUsers, Action: action})
	if errors.Is(err, authorization.ErrDenied) {
		h.security.Problem(c, http.StatusForbidden, "forbidden")
		return identity.Actor{}, false
	}
	if err != nil {
		h.security.Problem(c, http.StatusInternalServerError, "internal server error")
		return identity.Actor{}, false
	}
	return actor, true
}

func (h *IdentityHandler) tenantUser(c *gin.Context, user identity.TenantUser) (generated.TenantUser, bool) {
	id, err := uuid.Parse(string(user.ID))
	if err != nil {
		h.security.Problem(c, http.StatusInternalServerError, "internal server error")
		return generated.TenantUser{}, false
	}
	item := generated.TenantUser{Id: id, Username: user.Username, Status: generated.TenantUserStatus(user.Status), CreatedAt: user.CreatedAt, UpdatedAt: user.UpdatedAt}
	if user.Email != nil {
		value := openapi_types.Email(*user.Email)
		item.Email = &value
	}
	return item, true
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
	service    Service
	authorizer Authorizer
	security   *apihttp.Security
}

// NewHandler constructs the identity HTTP adapter.
func NewHandler(service Service, authorizer Authorizer, security *apihttp.Security) *IdentityHandler {
	return &IdentityHandler{service: service, authorizer: authorizer, security: security}
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
	tokens, err := h.service.Login(c.Request.Context(), request.Tenant, request.Login, *request.Password, c.GetHeader("X-Request-ID"))
	if errors.Is(err, identity.ErrAccountLocked) {
		h.security.Problem(c, http.StatusTooManyRequests, "too many attempts")
		return
	}
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
	tokens, err := h.service.Refresh(c.Request.Context(), refresh, c.GetHeader("X-Request-ID"))
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
	if err := h.service.Logout(c.Request.Context(), actor, c.GetHeader("X-Request-ID")); err != nil {
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
	if err := h.service.LogoutAll(c.Request.Context(), actor, c.GetHeader("X-Request-ID")); err != nil {
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
	tokens, err := h.service.ChangePassword(c.Request.Context(), actor, *request.CurrentPassword, *request.NewPassword, refresh, c.GetHeader("X-Request-ID"))
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
	if err := h.service.ConsumeOneTimeToken(c.Request.Context(), *request.Token, purpose, *request.NewPassword, c.GetHeader("X-Request-ID")); err != nil {
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
