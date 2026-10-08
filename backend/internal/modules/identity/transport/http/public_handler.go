package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/modura-dev/modura/backend/internal/api/generated"
	apihttp "github.com/modura-dev/modura/backend/internal/api/transport"
	"github.com/modura-dev/modura/backend/internal/modules/identity"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// PublicService is the consumer-only capability consumed by HTTP.
type PublicService interface {
	Enabled() bool
	Available(context.Context) bool
	Request(context.Context, string, identity.PublicRequest) error
	Complete(context.Context, string, string, string, identity.PublicRequest) error
	Login(context.Context, identity.PublicRequest) (identity.Tokens, error)
	Refresh(context.Context, string, string) (identity.Tokens, error)
	Authenticate(context.Context, string) (identity.Actor, error)
	Profile(context.Context, identity.Actor) (identity.Profile, error)
	Logout(context.Context, identity.Actor, string) error
}

// PublicIdentityHandler delivers the isolated consumer operation set.
type PublicIdentityHandler struct {
	service       PublicService
	security      *apihttp.Security
	siteKey       string
	reportFailure func()
}

// NewPublicHandler composes explicit identity, security and redacted diagnostics.
func NewPublicHandler(service PublicService, security *apihttp.Security, siteKey string, reportFailure func()) *PublicIdentityHandler {
	return &PublicIdentityHandler{service: service, security: security, siteKey: siteKey, reportFailure: reportFailure}
}

// GetConsumerAuthStatus returns only safe configuration readiness.
func (h *PublicIdentityHandler) GetConsumerAuthStatus(c *gin.Context) {
	enabled := h.service != nil && h.service.Available(c.Request.Context())
	var key *string
	if enabled && h.siteKey != "" {
		key = &h.siteKey
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, generated.ConsumerAuthStatus{Enabled: enabled, ChallengeSiteKey: key})
}
func (h *PublicIdentityHandler) available(c *gin.Context) bool {
	c.Header("Cache-Control", "no-store")
	if h.service == nil || !h.service.Enabled() {
		h.security.Problem(c, http.StatusServiceUnavailable, "public accounts unavailable")
		return false
	}
	return true
}
func (h *PublicIdentityHandler) decode(c *gin.Context, target any) bool {
	d := json.NewDecoder(c.Request.Body)
	d.DisallowUnknownFields()
	if d.Decode(target) != nil {
		h.security.Problem(c, 400, "invalid request")
		return false
	}
	if d.Decode(new(any)) != io.EOF {
		h.security.Problem(c, 400, "invalid request")
		return false
	}
	return true
}
func publicRequest(c *gin.Context, email, password, challenge string) identity.PublicRequest {
	// Network identity comes from the peer socket, never an untrusted forwarded header.
	host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		host = ""
	}
	ip := net.ParseIP(host)
	network := ""
	if ip != nil {
		network = ip.String()
	}
	return identity.PublicRequest{Email: email, Password: password, Challenge: challenge, Network: network, CorrelationID: c.GetHeader("X-Request-ID")}
}
func (h *PublicIdentityHandler) outcome(c *gin.Context, err error) {
	if err == nil || errors.Is(err, identity.ErrPublicAcceptedFailure) {
		if err != nil && h.reportFailure != nil {
			h.reportFailure()
		}
		c.Status(http.StatusAccepted)
		return
	}
	h.failure(c, err)
}
func (h *PublicIdentityHandler) failure(c *gin.Context, err error) {
	switch {
	case errors.Is(err, identity.ErrPublicUnavailable):
		h.security.Problem(c, 503, "public accounts unavailable")
	case errors.Is(err, identity.ErrPublicLimited):
		h.security.Problem(c, 429, "too many requests")
	case errors.Is(err, identity.ErrPublicChallenge):
		h.security.Problem(c, 403, "request verification failed")
	case errors.Is(err, identity.ErrPublicInvalid) || errors.Is(err, identity.ErrInvalidPassword):
		h.security.Problem(c, 400, "invalid request")
	case errors.Is(err, identity.ErrInvalidToken) || errors.Is(err, identity.ErrExpiredToken) || errors.Is(err, identity.ErrInvalidCredentials) || errors.Is(err, identity.ErrAccountLocked) || errors.Is(err, identity.ErrRefreshReuse):
		h.security.Problem(c, 401, "authentication failed")
	default:
		h.security.Problem(c, 500, "internal server error")
	}
}

// RegisterConsumer queues verification without revealing account existence.
func (h *PublicIdentityHandler) RegisterConsumer(c *gin.Context) {
	if !h.available(c) {
		return
	}
	var r generated.ConsumerRegistrationRequest
	if !h.decode(c, &r) {
		return
	}
	h.outcome(c, h.service.Request(c.Request.Context(), "register", publicRequest(c, string(r.Email), r.Password, r.Challenge)))
}

// ResendConsumerVerification does not replace an existing password.
func (h *PublicIdentityHandler) ResendConsumerVerification(c *gin.Context) {
	h.emailRequest(c, "resend")
}

// RequestConsumerRecovery queues recovery only for eligible consumer accounts.
func (h *PublicIdentityHandler) RequestConsumerRecovery(c *gin.Context) { h.emailRequest(c, "recover") }
func (h *PublicIdentityHandler) emailRequest(c *gin.Context, operation string) {
	if !h.available(c) {
		return
	}
	var r generated.ConsumerEmailRequest
	if !h.decode(c, &r) {
		return
	}
	h.outcome(c, h.service.Request(c.Request.Context(), operation, publicRequest(c, string(r.Email), "", r.Challenge)))
}

// VerifyConsumerEmail consumes an email-purpose code without creating a session.
func (h *PublicIdentityHandler) VerifyConsumerEmail(c *gin.Context) {
	if !h.available(c) {
		return
	}
	var r generated.ConsumerCodeRequest
	if !h.decode(c, &r) {
		return
	}
	err := h.service.Complete(c.Request.Context(), "email_verification", r.Code, "", publicRequest(c, "", "", r.Challenge))
	if err != nil {
		h.failure(c, err)
		return
	}
	c.Status(204)
}

// ResetConsumerPassword consumes a reset-purpose code and revokes old sessions.
func (h *PublicIdentityHandler) ResetConsumerPassword(c *gin.Context) {
	if !h.available(c) {
		return
	}
	var r generated.ConsumerResetRequest
	if !h.decode(c, &r) {
		return
	}
	err := h.service.Complete(c.Request.Context(), "password_reset", r.Code, r.Password, publicRequest(c, "", "", r.Challenge))
	if err != nil {
		h.failure(c, err)
		return
	}
	c.Status(204)
}

// LoginConsumer sets cookies distinct from administrator cookies.
func (h *PublicIdentityHandler) LoginConsumer(c *gin.Context) {
	if !h.available(c) {
		return
	}
	var r generated.ConsumerLoginRequest
	if !h.decode(c, &r) {
		return
	}
	tokens, err := h.service.Login(c.Request.Context(), publicRequest(c, string(r.Email), r.Password, r.Challenge))
	if err != nil {
		h.failure(c, err)
		return
	}
	h.writeTokens(c, tokens)
}
func (h *PublicIdentityHandler) writeTokens(c *gin.Context, t identity.Tokens) {
	h.security.WriteTokens(c, t.AccessToken, t.RefreshToken, t.ExpiresIn, t.RefreshExpiresIn, apihttp.ConsumerRefreshCookie, apihttp.ConsumerCSRFCookie, "/api/public/auth")
}

// RefreshConsumer rotates only consumer refresh tokens after CSRF validation.
func (h *PublicIdentityHandler) RefreshConsumer(c *gin.Context, p generated.RefreshConsumerParams) {
	if !h.available(c) {
		return
	}
	refresh, ok := h.security.CookieAndCSRF(c, apihttp.ConsumerRefreshCookie, apihttp.ConsumerCSRFCookie, p.XCSRFToken)
	if !ok {
		return
	}
	tokens, err := h.service.Refresh(c.Request.Context(), refresh, c.GetHeader("X-Request-ID"))
	if err != nil {
		h.security.ClearCookies(c, apihttp.ConsumerRefreshCookie, apihttp.ConsumerCSRFCookie, "/api/public/auth")
		h.failure(c, err)
		return
	}
	h.writeTokens(c, tokens)
}
func (h *PublicIdentityHandler) actor(c *gin.Context) (identity.Actor, bool) {
	if !h.available(c) {
		return identity.Actor{}, false
	}
	token, ok := h.security.Bearer(c)
	if !ok {
		return identity.Actor{}, false
	}
	actor, err := h.service.Authenticate(c.Request.Context(), token)
	if err != nil {
		h.failure(c, err)
		return identity.Actor{}, false
	}
	return actor, true
}

// GetConsumerProfile reads only the caller's verified community profile.
func (h *PublicIdentityHandler) GetConsumerProfile(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	profile, err := h.service.Profile(c.Request.Context(), actor)
	if err != nil {
		h.failure(c, err)
		return
	}
	id, err := uuid.Parse(string(profile.ID))
	if err != nil {
		h.failure(c, err)
		return
	}
	r := generated.UserProfile{Id: id, Username: profile.Username, Status: generated.UserProfileStatus(profile.Status), UpdatedAt: profile.UpdatedAt}
	if profile.Email != nil {
		email := openapi_types.Email(*profile.Email)
		r.Email = &email
	}
	c.JSON(200, r)
}

// LogoutConsumer revokes the current consumer session after bearer and CSRF checks.
func (h *PublicIdentityHandler) LogoutConsumer(c *gin.Context, p generated.LogoutConsumerParams) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	if _, ok := h.security.CookieAndCSRF(c, apihttp.ConsumerRefreshCookie, apihttp.ConsumerCSRFCookie, p.XCSRFToken); !ok {
		return
	}
	if err := h.service.Logout(c.Request.Context(), actor, c.GetHeader("X-Request-ID")); err != nil {
		h.failure(c, err)
		return
	}
	h.security.ClearCookies(c, apihttp.ConsumerRefreshCookie, apihttp.ConsumerCSRFCookie, "/api/public/auth")
	c.Status(204)
}
