package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/psmMRFP/WhereToLive/backend/internal/api/generated"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/authorization"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/identity"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/organization"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/platformadmin"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/platformtenant"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/provisioning"
)

type identityStub struct {
	actor identity.Actor
}

func (s *identityStub) Profile(context.Context, identity.Actor) (identity.Profile, error) {
	return identity.Profile{ID: "018bcfe5-6800-7000-8000-000000000001", Username: "alice", Status: "active", UpdatedAt: time.Unix(1_700_000_000, 0)}, nil
}
func (s *identityStub) ListUsers(context.Context, identity.TenantID) ([]identity.TenantUser, error) {
	return nil, nil
}
func (s *identityStub) GetUser(context.Context, identity.TenantID, identity.UserID) (identity.TenantUser, error) {
	return identity.TenantUser{}, identity.ErrUserNotFound
}
func (s *identityStub) DisableUser(context.Context, identity.Actor, identity.UserID, string, string) (identity.TenantUser, error) {
	return identity.TenantUser{ID: "018bcfe5-6800-7000-8000-000000000002", Username: "bob", Status: "disabled", UpdatedAt: time.Unix(1_700_000_000, 0)}, nil
}
func (s *identityStub) UnlockUser(context.Context, identity.Actor, identity.UserID, string) (identity.TenantUser, error) {
	return identity.TenantUser{ID: "018bcfe5-6800-7000-8000-000000000002", Username: "bob", Status: "active", UpdatedAt: time.Unix(1_700_000_000, 0)}, nil
}
func (s *identityStub) UpdateProfile(_ context.Context, actor identity.Actor, username string, email *string, _ string) (identity.Profile, error) {
	return identity.Profile{ID: actor.UserID, Username: username, Email: email, Status: "active", UpdatedAt: time.Unix(1_700_000_000, 0)}, nil
}

type authorizerStub struct{ denied bool }

func (s authorizerStub) Authorize(context.Context, identity.Actor, authorization.Permission) error {
	if s.denied {
		return authorization.ErrDenied
	}
	return nil
}

func (s authorizerStub) ResolveDataScope(context.Context, identity.Actor, authorization.Permission) (authorization.ResolvedDataScope, error) {
	if s.denied {
		return authorization.ResolvedDataScope{}, authorization.ErrDenied
	}
	return authorization.ResolvedDataScope{All: true}, nil
}

type organizationStub struct{}

type platformAdminStub struct{}

type platformTenantStub struct{}

type provisioningStub struct {
	request provisioning.Request
	err     error
}

func (s *provisioningStub) Provision(_ context.Context, request provisioning.Request) (provisioning.Result, error) {
	s.request = request
	if s.err != nil {
		return provisioning.Result{}, s.err
	}
	return provisioning.Result{TenantID: "018bcfe5-6800-7000-8000-000000000901", AdministratorID: "018bcfe5-6800-7000-8000-000000000902", InvitationToken: "must-never-be-exposed", Created: true}, nil
}

func (platformAdminStub) Login(_ context.Context, username, password string) (platformadmin.Tokens, error) {
	if username != "operator" || password != "secret password" {
		return platformadmin.Tokens{}, platformadmin.ErrInvalidCredentials
	}
	return platformadmin.Tokens{AccessToken: "platform-access", RefreshToken: "platform-refresh", ExpiresIn: 5 * time.Minute, RefreshExpiresIn: 24 * time.Hour}, nil
}

func (platformAdminStub) Refresh(_ context.Context, refresh string) (platformadmin.Tokens, error) {
	if refresh != "platform-refresh" {
		return platformadmin.Tokens{}, platformadmin.ErrInvalidToken
	}
	return platformadmin.Tokens{AccessToken: "rotated-platform-access", RefreshToken: "rotated-platform-refresh", ExpiresIn: 5 * time.Minute, RefreshExpiresIn: 24 * time.Hour}, nil
}

func (platformAdminStub) AuthenticateAccess(_ context.Context, token string) (platformadmin.Actor, error) {
	if token != "platform-access" {
		return platformadmin.Actor{}, platformadmin.ErrInvalidToken
	}
	return platformadmin.Actor{AdministratorID: "018bcfe5-6800-7000-8000-000000000801", SessionID: "018bcfe5-6800-7000-8000-000000000802"}, nil
}
func (platformAdminStub) Logout(context.Context, platformadmin.Actor) error { return nil }

func (platformTenantStub) List(context.Context, platformadmin.Actor) ([]platformtenant.Tenant, error) {
	return nil, nil
}
func (platformTenantStub) UpdateProfile(context.Context, platformadmin.Actor, identity.TenantID, string, time.Time, string, string) error {
	return nil
}
func (platformTenantStub) Suspend(context.Context, platformadmin.Actor, identity.TenantID, string, string) error {
	return nil
}
func (platformTenantStub) Reactivate(context.Context, platformadmin.Actor, identity.TenantID, string, string) error {
	return nil
}

func (organizationStub) ListDepartments(context.Context, identity.TenantID, organization.DataScope) ([]organization.DepartmentView, error) {
	return nil, nil
}
func (organizationStub) ListPositions(context.Context, identity.TenantID) ([]organization.PositionView, error) {
	return nil, nil
}
func (organizationStub) CreateDepartment(context.Context, organization.WriteContext, *organization.DepartmentID, string, int) (organization.DepartmentID, error) {
	return "018bcfe5-6800-7000-8000-000000000701", nil
}
func (organizationStub) UpdateDepartment(context.Context, organization.WriteContext, organization.DepartmentID, string, int) error {
	return nil
}
func (organizationStub) MoveDepartment(context.Context, organization.WriteContext, organization.DepartmentID, organization.DepartmentID) error {
	return nil
}
func (organizationStub) DeleteDepartment(context.Context, organization.WriteContext, organization.DepartmentID) error {
	return nil
}
func (organizationStub) CreatePosition(context.Context, organization.WriteContext, string) (organization.PositionID, error) {
	return "018bcfe5-6800-7000-8000-000000000702", nil
}
func (organizationStub) UpdatePosition(context.Context, organization.WriteContext, organization.PositionID, string, organization.PositionStatus) error {
	return nil
}
func (organizationStub) AssignUser(context.Context, organization.WriteContext, identity.UserID, organization.DepartmentID, *organization.PositionID) error {
	return nil
}

func (s *identityStub) Login(_ context.Context, tenant, login, password, _ string) (identity.Tokens, error) {
	if tenant != "acme" || login != "alice" || password != "secret password" {
		return identity.Tokens{}, identity.ErrInvalidCredentials
	}
	return identity.Tokens{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 5 * time.Minute, RefreshExpiresIn: 24 * time.Hour}, nil
}
func (s *identityStub) Refresh(_ context.Context, refresh, _ string) (identity.Tokens, error) {
	if refresh != "refresh" {
		return identity.Tokens{}, identity.ErrInvalidToken
	}
	return identity.Tokens{AccessToken: "rotated-access", RefreshToken: "rotated-refresh", ExpiresIn: 5 * time.Minute, RefreshExpiresIn: 24 * time.Hour}, nil
}
func (s *identityStub) AuthenticateAccess(_ context.Context, token string) (identity.Actor, error) {
	if token != "access" {
		return identity.Actor{}, identity.ErrInvalidToken
	}
	return s.actor, nil
}
func (*identityStub) Logout(context.Context, identity.Actor, string) error    { return nil }
func (*identityStub) LogoutAll(context.Context, identity.Actor, string) error { return nil }
func (*identityStub) ChangePassword(context.Context, identity.Actor, string, string, string, string) (identity.Tokens, error) {
	return identity.Tokens{AccessToken: "changed-access", RefreshToken: "changed-refresh", ExpiresIn: 5 * time.Minute, RefreshExpiresIn: 24 * time.Hour}, nil
}
func (*identityStub) ConsumeOneTimeToken(_ context.Context, token string, _ identity.OneTimePurpose, password, _ string) error {
	if token != strings.Repeat("t", 32) {
		return identity.ErrInvalidToken
	}
	if len(password) < 12 {
		return identity.ErrInvalidPassword
	}
	return nil
}

func TestLoginSetsProtectedSessionCookies(t *testing.T) {
	router := testRouter(&identityStub{})
	request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/auth/login", strings.NewReader(`{"tenant":"acme","login":"alice","password":"secret password"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 2 || !cookies[0].HttpOnly || cookies[1].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("unexpected cookies: %+v", cookies)
	}
	var body generated.AccessTokenResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.AccessToken != "access" || body.CsrfToken != strings.Repeat("c", 32) {
		t.Fatalf("body = %+v", body)
	}
}

func TestRefreshRequiresMatchingCSRF(t *testing.T) {
	router := testRouter(&identityStub{})
	for _, header := range []string{"", strings.Repeat("x", 32)} {
		request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/auth/refresh", nil)
		request.AddCookie(&http.Cookie{Name: "wheretolive_tenant_refresh", Value: "refresh"})
		request.AddCookie(&http.Cookie{Name: "wheretolive_tenant_csrf", Value: strings.Repeat("c", 32)})
		if header != "" {
			request.Header.Set("X-CSRF-Token", header)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		want := http.StatusBadRequest
		if header != "" {
			want = http.StatusForbidden
		}
		if response.Code != want {
			t.Fatalf("header %q status = %d, want %d", header, response.Code, want)
		}
	}
}

func TestPlatformLoginUsesDistinctCookies(t *testing.T) {
	router := testRouter(&identityStub{})
	request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/platform/auth/login", strings.NewReader(`{"username":"operator","password":"secret password"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 2 || cookies[0].Name != "wheretolive_platform_refresh" || cookies[1].Name != "wheretolive_platform_csrf" {
		t.Fatalf("unexpected platform cookies: %+v", cookies)
	}
	for _, cookie := range cookies {
		// The refresh cookie stays scoped to the platform API surface; the
		// CSRF cookie is site-wide so the SPA can read it on any route.
		want := "/api/platform"
		if cookie.Name == "wheretolive_platform_csrf" {
			want = "/"
		}
		if cookie.Path != want {
			t.Fatalf("cookie %s path = %q, want %q", cookie.Name, cookie.Path, want)
		}
	}
}

func TestPlatformRefreshRejectsTenantCookies(t *testing.T) {
	router := testRouter(&identityStub{})
	request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/platform/auth/refresh", nil)
	request.AddCookie(&http.Cookie{Name: "wheretolive_tenant_refresh", Value: "refresh"})
	request.AddCookie(&http.Cookie{Name: "wheretolive_tenant_csrf", Value: strings.Repeat("c", 32)})
	request.Header.Set("X-CSRF-Token", strings.Repeat("c", 32))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func TestPlatformLogoutClearsCookies(t *testing.T) {
	router := testRouter(&identityStub{})
	request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/platform/auth/logout", nil)
	request.Header.Set("Authorization", "Bearer platform-access")
	request.Header.Set("X-CSRF-Token", strings.Repeat("c", 32))
	request.AddCookie(&http.Cookie{Name: "wheretolive_platform_refresh", Value: "platform-refresh"})
	request.AddCookie(&http.Cookie{Name: "wheretolive_platform_csrf", Value: strings.Repeat("c", 32)})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d, body=%s", response.Code, http.StatusNoContent, response.Body.String())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 2 || cookies[0].MaxAge >= 0 || cookies[1].MaxAge >= 0 {
		t.Fatalf("unexpected cleared cookies: %+v", cookies)
	}
}

func TestProvisionTenantRequiresPlatformSessionAndDoesNotExposeInvitation(t *testing.T) {
	service := &provisioningStub{}
	router := testRouterWithProvisioning(&identityStub{}, service)
	body := `{"slug":"acme","displayName":"Acme","rootDepartmentName":"Acme Root","administratorUsername":"admin","administratorEmail":"admin@example.com","reason":"customer onboarding"}`
	request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/platform/tenants", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer platform-access")
	request.Header.Set("X-CSRF-Token", strings.Repeat("c", 32))
	request.Header.Set("Idempotency-Key", "018bcfe5-6800-7000-8000-000000000903")
	request.Header.Set("X-Request-ID", "request-provision-http")
	request.AddCookie(&http.Cookie{Name: "wheretolive_platform_refresh", Value: "platform-refresh"})
	request.AddCookie(&http.Cookie{Name: "wheretolive_platform_csrf", Value: strings.Repeat("c", 32)})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "must-never-be-exposed") || strings.Contains(response.Body.String(), "invitation") {
		t.Fatalf("response disclosed invitation material: %s", response.Body.String())
	}
	if service.request.Actor.AdministratorID == "" || service.request.Reason != "customer onboarding" || service.request.CorrelationID != "request-provision-http" {
		t.Fatalf("provisioning request = %+v", service.request)
	}
}

func TestProvisionTenantMapsIdempotencyConflict(t *testing.T) {
	service := &provisioningStub{err: provisioning.ErrIdempotencyConflict}
	router := testRouterWithProvisioning(&identityStub{}, service)
	request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/platform/tenants", strings.NewReader(`{"slug":"acme","displayName":"Acme","rootDepartmentName":"Root","administratorUsername":"admin","reason":"onboarding"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer platform-access")
	request.Header.Set("X-CSRF-Token", strings.Repeat("c", 32))
	request.Header.Set("Idempotency-Key", "018bcfe5-6800-7000-8000-000000000903")
	request.AddCookie(&http.Cookie{Name: "wheretolive_platform_refresh", Value: "platform-refresh"})
	request.AddCookie(&http.Cookie{Name: "wheretolive_platform_csrf", Value: strings.Repeat("c", 32)})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d, body=%s", response.Code, http.StatusConflict, response.Body.String())
	}
}

func TestOneTimeCredentialResponseDoesNotDiscloseTokenState(t *testing.T) {
	router := testRouter(&identityStub{})
	for _, token := range []string{strings.Repeat("x", 32), strings.Repeat("t", 32)} {
		body := `{"token":"` + token + `","newPassword":"a secure new password"}`
		request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/auth/password-resets", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		want := http.StatusUnauthorized
		if token == strings.Repeat("t", 32) {
			want = http.StatusNoContent
		}
		if response.Code != want {
			t.Fatalf("status = %d, want %d, body=%s", response.Code, want, response.Body.String())
		}
	}
}

func TestDepartmentManagementRequiresServerAuthorization(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	service := &identityStub{actor: identity.Actor{TenantID: "tenant", UserID: "user", SessionID: "session"}}
	h := New(Dependencies{Identity: service, Authorizer: authorizerStub{denied: true}, Organization: organizationStub{}, PlatformAdmin: platformAdminStub{}, PlatformTenant: platformTenantStub{}}, true, func() (string, error) { return strings.Repeat("c", 32), nil })
	generated.RegisterHandlersWithOptions(router, h, generated.GinServerOptions{BaseURL: "/api"})
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/organization/departments", nil)
	request.Header.Set("Authorization", "Bearer access")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d, body=%s", response.Code, http.StatusForbidden, response.Body.String())
	}
}

func testRouter(service Identity) http.Handler {
	return testRouterWithProvisioning(service, &provisioningStub{})
}

func testRouterWithProvisioning(service Identity, provisioningService Provisioning) http.Handler {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := New(Dependencies{Identity: service, Authorizer: authorizerStub{}, Organization: organizationStub{}, PlatformAdmin: platformAdminStub{}, PlatformTenant: platformTenantStub{}, Provisioning: provisioningService}, true, func() (string, error) { return strings.Repeat("c", 32), nil })
	generated.RegisterHandlersWithOptions(router, h, generated.GinServerOptions{BaseURL: "/api"})
	return router
}
