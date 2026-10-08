// Package transport provides HTTP-only primitives shared by module adapters.
package transport

import (
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/modura-dev/modura/backend/internal/api/generated"
)

// Cookie names separate tenant-local and global platform sessions.
const (
	TenantRefreshCookie   = "modura_refresh"
	TenantCSRFCookie      = "modura_csrf"
	PlatformRefreshCookie = "modura_platform_refresh"
	PlatformCSRFCookie    = "modura_platform_csrf"
)

// Security centralizes the cookie, CSRF, bearer, and problem-response policy.
type Security struct {
	cookieSecure bool
	newCSRF      func() (string, error)
}

// NewSecurity constructs shared HTTP security policy.
func NewSecurity(cookieSecure bool, newCSRF func() (string, error)) *Security {
	return &Security{cookieSecure: cookieSecure, newCSRF: newCSRF}
}

// Bearer extracts a valid bearer credential or writes a safe error response.
func (s *Security) Bearer(c *gin.Context) (string, bool) {
	prefix, token, ok := strings.Cut(c.GetHeader("Authorization"), " ")
	if !ok || !strings.EqualFold(prefix, "Bearer") || token == "" {
		s.Problem(c, http.StatusUnauthorized, "authentication failed")
		return "", false
	}
	return token, true
}

// CookieAndCSRF verifies a named double-submit CSRF pair and returns the refresh token.
func (s *Security) CookieAndCSRF(c *gin.Context, refreshName, csrfName, header string) (string, bool) {
	refresh, refreshErr := c.Cookie(refreshName)
	csrf, csrfErr := c.Cookie(csrfName)
	if refreshErr != nil || csrfErr != nil || header == "" || subtle.ConstantTimeCompare([]byte(header), []byte(csrf)) != 1 {
		s.Problem(c, http.StatusForbidden, "request verification failed")
		return "", false
	}
	return refresh, true
}

// WriteTokens writes protected session cookies and the public access-token response.
func (s *Security) WriteTokens(c *gin.Context, accessToken, refreshToken string, expiresIn, refreshExpiresIn time.Duration, refreshName, csrfName, path string) {
	csrf, err := s.newCSRF()
	if err != nil {
		s.Problem(c, http.StatusInternalServerError, "internal server error")
		return
	}
	maxAge := int(refreshExpiresIn.Seconds())
	s.setCookie(c, refreshName, refreshToken, maxAge, true, path)
	// The CSRF cookie must be readable by the SPA on every route, so it is
	// scoped to the site root even though the refresh cookie is path-limited.
	s.setCookie(c, csrfName, csrf, maxAge, false, "/")
	c.JSON(http.StatusOK, generated.AccessTokenResponse{AccessToken: accessToken, TokenType: generated.Bearer, ExpiresIn: int64(expiresIn.Seconds()), CsrfToken: csrf})
}

// ClearCookies expires a named refresh and CSRF cookie pair.
func (s *Security) ClearCookies(c *gin.Context, refreshName, csrfName, path string) {
	s.setCookie(c, refreshName, "", -1, true, path)
	s.setCookie(c, csrfName, "", -1, false, "/")
}

// Problem writes a non-leaking RFC 9457-style problem response.
func (s *Security) Problem(c *gin.Context, status int, title string) {
	c.Header("Content-Type", "application/problem+json")
	c.JSON(status, generated.Problem{Type: "about:blank", Title: title, Status: status})
}

func (s *Security) setCookie(c *gin.Context, name, value string, maxAge int, httpOnly bool, path string) {
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(name, value, maxAge, path, "", s.cookieSecure, httpOnly)
}
