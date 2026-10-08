package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/goccy/go-yaml"
	"github.com/psmMRFP/WhereToLive/backend/internal/api/generated"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/places"
)

type publicStore struct{ err error }

func (s publicStore) SearchPublic(context.Context, places.Query) ([]places.Place, error) {
	return []places.Place{publicFixture()}, s.err
}
func (s publicStore) GetPublic(_ context.Context, slug, locale string) (places.Place, error) {
	if s.err != nil {
		return places.Place{}, s.err
	}
	if slug != "munich" {
		return places.Place{}, places.ErrNotFound
	}
	p := publicFixture()
	if locale == "de" {
		p.DisplayName = "München"
	}
	return p, nil
}
func publicFixture() places.Place {
	return places.Place{ID: "018bcfe5-6800-7000-8000-000000001002", Slug: "munich", Name: "Munich", DisplayName: "Munich", Type: "city", CountryCode: "DE", Languages: []string{}, CoverageLevel: 1, PublishedAt: time.Unix(1700000000, 0).UTC()}
}
func publicRouter(store publicStore) *gin.Engine {
	service, _ := places.NewService(store)
	router := gin.New()
	generated.RegisterHandlersWithOptions(router, New(Dependencies{Places: service}, false, func() (string, error) { return "", nil }), generated.GinServerOptions{BaseURL: "/api"})
	return router
}
func TestPublicPlaceContractWithoutAuthentication(t *testing.T) {
	router := publicRouter(publicStore{})
	document, err := os.ReadFile("../../../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var contract map[string]any
	if err := yaml.Unmarshal(document, &contract); err != nil {
		t.Fatal(err)
	}
	schema := contract["components"].(map[string]any)["schemas"].(map[string]any)["PublicPlace"].(map[string]any)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequestWithContext(context.Background(), "GET", "/api/public/places/munich?locale=de", nil))
	if recorder.Code != 200 {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var result map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	for _, key := range schema["required"].([]any) {
		if _, ok := result[key.(string)]; !ok {
			t.Errorf("missing required field %s", key)
		}
	}
	if result["displayName"] != "München" || result["slug"] != "munich" || result["currency"] != nil || result["parentId"] != nil {
		t.Fatalf("response=%+v", result)
	}
	if strings.Contains(recorder.Body.String(), "tenant") || len(recorder.Result().Cookies()) != 0 {
		t.Fatal("anonymous read leaked identity context")
	}
	for _, path := range []string{"/public/places", "/public/places/{slug}"} {
		op := contract["paths"].(map[string]any)[path].(map[string]any)["get"].(map[string]any)
		if security, ok := op["security"].([]any); !ok || len(security) != 0 {
			t.Fatalf("%s is not explicitly anonymous", path)
		}
	}
}
func TestPublicPlaceValidationAndSafeErrors(t *testing.T) {
	router := publicRouter(publicStore{})
	for _, tc := range []struct {
		path   string
		status int
	}{
		{"/api/public/places", 200},
		{"/api/public/places?limit=0", 400},
		{"/api/public/places?limit=51", 400},
		{"/api/public/places?offset=-1", 400},
		{"/api/public/places?offset=10001", 400},
		{"/api/public/places?locale=xx", 400},
		{"/api/public/places/missing", 404},
		{"/api/public/places/Munich", 400},
		{"/api/public/places/munich?locale=xx", 400},
	} {
		t.Run(tc.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), "GET", tc.path, nil))
			if w.Code != tc.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
	w := httptest.NewRecorder()
	publicRouter(publicStore{err: errors.New("private database detail")}).ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), "GET", "/api/public/places", nil))
	if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "private database") || !strings.Contains(w.Header().Get("Content-Type"), "application/problem+json") {
		t.Fatalf("error=%d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), "POST", "/api/public/places", strings.NewReader(`{"published":true}`)))
	if w.Code != 404 && w.Code != 405 {
		t.Fatal("public write route exists")
	}
}
