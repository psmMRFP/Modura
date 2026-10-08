package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/modura-dev/modura/backend/internal/api/generated"
	"github.com/modura-dev/modura/backend/internal/modules/places"
	"github.com/modura-dev/modura/backend/internal/modules/platformadmin"
)

type platformPlaceStub struct {
	calls int
	err   error
	write places.WriteContext
	input places.Create
}

func (s *platformPlaceStub) entry() places.Entry {
	return places.Entry{ID: "018bcfe5-6800-7000-8000-000000001601", Slug: "germany", Type: "country", CountryCode: "DE", Version: 1, CreatedAt: time.Unix(1700000000, 0).UTC(), UpdatedAt: time.Unix(1700000000, 0).UTC(), Details: places.Details{Name: "Germany", Languages: []string{}, Aliases: []places.Alias{}}}
}
func (s *platformPlaceStub) List(context.Context, platformadmin.Actor, places.Query) (places.ManagedPage, error) {
	s.calls++
	return places.ManagedPage{Items: []places.Entry{s.entry()}}, s.err
}
func (s *platformPlaceStub) Get(context.Context, platformadmin.Actor, string) (places.Entry, error) {
	s.calls++
	return s.entry(), s.err
}
func (s *platformPlaceStub) Create(_ context.Context, w places.WriteContext, input places.Create) (places.Entry, error) {
	s.calls++
	s.write = w
	s.input = input
	return s.entry(), s.err
}
func (s *platformPlaceStub) Update(context.Context, places.WriteContext, string, int64, places.Details) (places.Entry, error) {
	s.calls++
	return s.entry(), s.err
}
func (s *platformPlaceStub) Publish(context.Context, places.WriteContext, string, int64, bool) (places.Entry, error) {
	s.calls++
	return s.entry(), s.err
}
func managedRouter(s *platformPlaceStub) *gin.Engine {
	router := gin.New()
	generated.RegisterHandlersWithOptions(router, New(Dependencies{PlatformPlaces: s, PlatformAdmin: platformAdminStub{}}, false, func() (string, error) { return "csrf", nil }), generated.GinServerOptions{BaseURL: "/api"})
	return router
}

const createPlaceJSON = `{"slug":"germany","type":"country","parentId":null,"countryCode":"DE","details":{"name":"Germany","timezone":null,"currency":null,"latitude":null,"longitude":null,"languages":[],"aliases":[]},"reason":"initial catalogue"}`

func platformRequest(method, path, body, token string, csrf bool) *http.Request {
	r := httptest.NewRequestWithContext(context.Background(), method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Request-ID", "place-request")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if csrf {
		r.Header.Set("X-CSRF-Token", "csrf")
		r.AddCookie(&http.Cookie{Name: "modura_platform_refresh", Value: "refresh"})
		r.AddCookie(&http.Cookie{Name: "modura_platform_csrf", Value: "csrf"})
	}
	return r
}
func TestPlatformPlacesRequirePlatformIdentityAndCSRF(t *testing.T) {
	s := &platformPlaceStub{}
	router := managedRouter(s)
	for _, tc := range []struct {
		method, path, token string
		csrf                bool
		status              int
	}{{"GET", "/api/platform/places", "", false, 401}, {"GET", "/api/platform/places", "tenant-access", false, 401}, {"GET", "/api/platform/places", "platform-access", false, 200}, {"POST", "/api/platform/places", "platform-access", false, 400}, {"POST", "/api/platform/places", "tenant-access", true, 401}, {"POST", "/api/platform/places", "platform-access", true, 201}} {
		t.Run(tc.method+tc.token, func(t *testing.T) {
			w := httptest.NewRecorder()
			router.ServeHTTP(w, platformRequest(tc.method, tc.path, createPlaceJSON, tc.token, tc.csrf))
			if w.Code != tc.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
	if s.calls != 2 || s.write.Actor.AdministratorID == "" || s.write.CorrelationID != "place-request" || s.input.Slug != "germany" {
		t.Fatalf("unauthorized calls or lost context: %+v", s)
	}
}
func TestPlatformPlacesRejectUnknownFieldsAndMissingPublicationFlag(t *testing.T) {
	s := &platformPlaceStub{}
	router := managedRouter(s)
	for _, body := range []string{`{"expectedVersion":1,"reason":"withdraw"}`, `{"expectedVersion":1,"published":null,"reason":"withdraw"}`, `{"expectedVersion":1,"published":true,"role":"admin","reason":"publish"}`, `{"expectedVersion":1,"published":true,"reason":"publish"} {}`} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, platformRequest("PUT", "/api/platform/places/018bcfe5-6800-7000-8000-000000001601/publication", body, "platform-access", true))
		if w.Code != 400 {
			t.Fatalf("accepted malformed body %s: %d", body, w.Code)
		}
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, platformRequest("POST", "/api/platform/places", strings.Replace(createPlaceJSON, `"aliases":[]`, `"aliases":null`, 1), "platform-access", true))
	if w.Code != 400 || s.calls != 0 {
		t.Fatal("invalid payload reached application")
	}
}
func TestPlatformPlaceErrorsDoNotDisclosePersistence(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
	}{{places.ErrConflict, 409}, {places.ErrInvalidPlace, 400}, {places.ErrNotFound, 404}, {errors.New("private database detail"), 500}} {
		s := &platformPlaceStub{err: tc.err}
		w := httptest.NewRecorder()
		managedRouter(s).ServeHTTP(w, platformRequest("POST", "/api/platform/places", createPlaceJSON, "platform-access", true))
		if w.Code != tc.status || strings.Contains(w.Body.String(), "private database") {
			t.Fatalf("unsafe error %d %s", w.Code, w.Body.String())
		}
	}
}
