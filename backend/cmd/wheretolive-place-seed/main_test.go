package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/psmMRFP/WhereToLive/backend/internal/api/generated"
)

type fixture struct {
	entries map[string]generated.ManagedPlace
	writes  int
	logouts int
	failAt  int
	race    bool
}

func (f *fixture) serve(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/api/platform/auth/login" {
		http.SetCookie(w, &http.Cookie{Name: "wheretolive_platform_refresh", Value: "test-refresh", Path: "/api/platform"})
		_ = json.NewEncoder(w).Encode(generated.AccessTokenResponse{AccessToken: "test-token", CsrfToken: "test-csrf"})
		return
	}
	if r.Header.Get("Authorization") != "Bearer test-token" {
		t.Error("missing bearer")
		w.WriteHeader(401)
		return
	}
	if r.Method == http.MethodPost {
		cookie, err := r.Cookie("wheretolive_platform_refresh")
		if err != nil || cookie.Value != "test-refresh" || r.Header.Get("X-CSRF-Token") != "test-csrf" {
			t.Error("missing write cookie or CSRF")
			w.WriteHeader(403)
			return
		}
	}
	switch {
	case r.URL.Path == "/api/platform/auth/logout":
		f.logouts++
		w.WriteHeader(204)
	case r.Method == http.MethodGet && r.URL.Path == "/api/platform/places":
		items := []generated.ManagedPlace{}
		if entry, ok := f.entries[r.URL.Query().Get("q")]; ok {
			items = append(items, entry)
		}
		for _, entry := range f.entries {
			if r.URL.Query().Get("countryCode") == entry.CountryCode {
				items = append(items, entry)
			}
		}
		_ = json.NewEncoder(w).Encode(generated.ManagedPlacePage{Items: items})
	case r.Method == http.MethodPost && r.URL.Path == "/api/platform/places":
		var input generated.CreatePlatformPlaceRequest
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Fatal(err)
		}
		if input.Reason == "" {
			t.Error("no audit reason")
		}
		if f.failAt > 0 && f.writes == f.failAt {
			w.WriteHeader(503)
			return
		}
		if _, exists := f.entries[input.Slug]; exists {
			t.Error("duplicate import write")
			w.WriteHeader(409)
			return
		}
		id, err := uuid.NewV7()
		if err != nil {
			t.Fatal(err)
		}
		entry := generated.ManagedPlace{Id: id, Slug: input.Slug, Type: input.Type, CountryCode: input.CountryCode, ParentId: input.ParentId, Details: input.Details, Version: 1}
		f.entries[input.Slug] = entry
		f.writes++
		if f.race {
			f.race = false
			w.WriteHeader(409)
			return
		}
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(entry)
	default:
		t.Errorf("unexpected request %s %s", r.Method, r.URL)
		w.WriteHeader(404)
	}
}

func TestImportPreviewResumeAndPreserve(t *testing.T) {
	f := &fixture{entries: map[string]generated.ManagedPlace{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { f.serve(t, w, r) }))
	defer server.Close()
	var output bytes.Buffer
	runImport := func(apply bool) error {
		output.Reset()
		return run(context.Background(), server.URL, "operator", "secret", "reviewed candidates", apply, &output)
	}
	if err := runImport(false); err != nil || f.writes != 0 || !strings.Contains(output.String(), "missing: 40") {
		t.Fatalf("preview: %v %s", err, &output)
	}
	f.failAt = 3
	if err := runImport(true); err == nil || f.writes != 3 {
		t.Fatalf("failure should leave resumable drafts: %v writes=%d", err, f.writes)
	}
	f.failAt = 0
	f.race = true
	if err := runImport(true); err != nil || len(f.entries) != 40 {
		t.Fatalf("resume including concurrent insert: %v count=%d", err, len(f.entries))
	}
	// Preserve operator edits and published records on repeat, without writes.
	germany := f.entries["germany"]
	germany.Details.Name = "Operator's localized name"
	germany.CoverageLevel = 2
	f.entries["germany"] = germany
	if err := runImport(true); err != nil || f.writes != 40 || f.entries["germany"].Details.Name != germany.Details.Name || f.entries["germany"].CoverageLevel != 2 {
		t.Fatalf("repeat changed existing entries: %v", err)
	}
	if f.logouts != 4 {
		t.Fatalf("sessions not revoked: %d", f.logouts)
	}
	if strings.Contains(output.String(), "test-token") || strings.Contains(output.String(), "secret") {
		t.Fatal("credentials leaked")
	}
}

func TestGeographyCollisionPreventsAllWrites(t *testing.T) {
	id, _ := uuid.NewV7()
	f := &fixture{entries: map[string]generated.ManagedPlace{"sydney": {Id: id, Slug: "sydney", Type: generated.City, CountryCode: "CA"}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { f.serve(t, w, r) }))
	defer server.Close()
	err := run(context.Background(), server.URL, "operator", "secret", "reviewed candidates", true, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "conflicting geography at sydney") || f.writes != 0 || f.logouts != 1 {
		t.Fatalf("conflict: %v writes=%d logout=%d", err, f.writes, f.logouts)
	}
}

func TestCredentialsCannotBeRedirectedOrPrinted(t *testing.T) {
	destinationRequests := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { destinationRequests++; w.WriteHeader(200) }))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	err := run(context.Background(), origin.URL, "operator", "private-password", "", false, &bytes.Buffer{})
	if err == nil || destinationRequests != 0 || strings.Contains(fmt.Sprint(err), "private-password") {
		t.Fatalf("redirect credential leak: %v requests=%d", err, destinationRequests)
	}
	for _, base := range []string{"http://example.com", "https://user:password@example.com", "https://example.com/path", "https://example.com?token=secret"} {
		if _, err := newClient(base); err == nil {
			t.Fatalf("accepted unsafe base %s", base)
		}
	}
}

func TestReuseCountryWithDifferentSlug(t *testing.T) {
	id, _ := uuid.NewV7()
	f := &fixture{entries: map[string]generated.ManagedPlace{"operator-germany": {Id: id, Slug: "operator-germany", Type: generated.Country, CountryCode: "DE", Details: generated.PlaceDetails{Name: "Deutschland"}, CoverageLevel: 1}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { f.serve(t, w, r) }))
	defer server.Close()
	err := run(context.Background(), server.URL, "operator", "secret", "reviewed candidates", true, &bytes.Buffer{})
	if err != nil || f.writes != 39 || f.entries["berlin"].ParentId == nil || *f.entries["berlin"].ParentId != id || f.entries["operator-germany"].Details.Name != "Deutschland" {
		t.Fatalf("country reuse: %v writes=%d", err, f.writes)
	}
}
