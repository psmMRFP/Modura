package handler

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/modura-dev/modura/backend/internal/api/generated"
	"github.com/modura-dev/modura/backend/internal/modules/identity"
)

type publicIdentityStub struct {
	PublicIdentity
	enabled bool
	err     error
	calls   int
	request identity.PublicRequest
}

func (s *publicIdentityStub) Available(context.Context) bool { return s.enabled }
func (s *publicIdentityStub) Enabled() bool                  { return s.enabled }
func (s *publicIdentityStub) Request(_ context.Context, _ string, r identity.PublicRequest) error {
	s.calls++
	s.request = r
	return s.err
}
func (s *publicIdentityStub) Login(context.Context, identity.PublicRequest) (identity.Tokens, error) {
	s.calls++
	return identity.Tokens{AccessToken: "consumer-access", RefreshToken: "consumer-refresh", ExpiresIn: time.Minute, RefreshExpiresIn: time.Hour}, s.err
}
func publicIdentityRouter(stub *publicIdentityStub) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	generated.RegisterHandlersWithOptions(router, New(Dependencies{PublicIdentity: stub, PublicChallengeSiteKey: "site-key"}, false, func() (string, error) { return "csrf", nil }), generated.GinServerOptions{BaseURL: "/api"})
	return router
}
func TestPublicIdentityClosedStrictAndNonEnumeratingHTTP(t *testing.T) {
	stub := &publicIdentityStub{}
	router := publicIdentityRouter(stub)
	request := httptest.NewRequestWithContext(context.Background(), "POST", "/api/public/auth/register", strings.NewReader(`{"email":"person@example.org","password":"long password","challenge":"proof"}`))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != 503 || stub.calls != 0 {
		t.Fatal("disabled registration open")
	}
	stub.enabled = true
	for _, body := range []string{`{"email":"person@example.org","password":"long password","challenge":"proof","tenant_id":"foreign"}`, `{"email":"person@example.org","password":"long password","challenge":"proof","role":"admin"}`, `{} {}`} {
		response = httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequestWithContext(context.Background(), "POST", "/api/public/auth/register", strings.NewReader(body)))
		if response.Code != 400 || stub.calls != 0 {
			t.Fatal("untrusted input accepted")
		}
	}
	for _, err := range []error{nil, identity.ErrPublicAcceptedFailure} {
		stub.err = err
		response = httptest.NewRecorder()
		request = httptest.NewRequestWithContext(context.Background(), "POST", "/api/public/auth/register", strings.NewReader(`{"email":"person@example.org","password":"long password","challenge":"proof"}`))
		request.Header.Set("X-Request-ID", "public-test")
		request.Header.Set("X-Forwarded-For", "203.0.113.99")
		router.ServeHTTP(response, request)
		if response.Code != 202 || response.Body.Len() != 0 || stub.request.Network == "203.0.113.99" {
			t.Fatal("account existence leaked or forwarded input trusted")
		}
	}
	stub.err = errors.New("private database diagnostics")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequestWithContext(context.Background(), "POST", "/api/public/auth/register", strings.NewReader(`{}`)))
	if response.Code != 500 || strings.Contains(response.Body.String(), "diagnostics") {
		t.Fatal("private error leaked")
	}
}
func TestConsumerCookiesAreIsolatedAndHttpOnly(t *testing.T) {
	stub := &publicIdentityStub{enabled: true}
	router := publicIdentityRouter(stub)
	response := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(context.Background(), "POST", "/api/public/auth/login", strings.NewReader(`{"email":"person@example.org","password":"long password","challenge":"proof"}`))
	router.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatal(response.Code)
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 2 {
		t.Fatal("missing cookies")
	}
	for _, cookie := range cookies {
		if cookie.Name == "modura_refresh" || cookie.Name == "modura_platform_refresh" {
			t.Fatal("administrator cookie overwritten")
		}
		if cookie.Name == "wheretolive_refresh" && (!cookie.HttpOnly || cookie.Path != "/api/public/auth") {
			t.Fatal("unprotected consumer refresh")
		}
	}
	if strings.Contains(response.Body.String(), "consumer-refresh") {
		t.Fatal("refresh token exposed in body")
	}
}
