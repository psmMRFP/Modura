package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/psmMRFP/WhereToLive/backend/internal/api/generated"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/feedback"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/platformadmin"
)

type feedbackStub struct {
	calls int
	err   error
	write feedback.WriteContext
	query feedback.Query
}

func (s *feedbackStub) entry() feedback.Entry {
	return feedback.Entry{ID: "018bcfe5-6800-7000-8000-000000007201", Category: "bug", Title: "Title", Message: "Message", Status: "open", Version: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
}
func (s *feedbackStub) Categories(context.Context, platformadmin.Actor) ([]feedback.Category, error) {
	s.calls++
	return []feedback.Category{{Key: "bug", Label: "Bug"}}, s.err
}
func (s *feedbackStub) List(_ context.Context, _ platformadmin.Actor, q feedback.Query) (feedback.Page, error) {
	s.calls++
	s.query = q
	return feedback.Page{Items: []feedback.Entry{s.entry()}}, s.err
}
func (s *feedbackStub) Create(_ context.Context, w feedback.WriteContext, _ feedback.Create) (feedback.Entry, error) {
	s.calls++
	s.write = w
	return s.entry(), s.err
}
func (s *feedbackStub) Review(_ context.Context, w feedback.WriteContext, _ string, _ feedback.Review) (feedback.Entry, error) {
	s.calls++
	s.write = w
	return s.entry(), s.err
}
func feedbackRouter(s *feedbackStub) *gin.Engine {
	r := gin.New()
	generated.RegisterHandlersWithOptions(r, New(Dependencies{PlatformFeedback: s, PlatformAdmin: platformAdminStub{}}, false, func() (string, error) { return "csrf", nil }), generated.GinServerOptions{BaseURL: "/api"})
	return r
}

const feedbackJSON = `{"category":"bug","placeId":null,"title":"Title","message":"Message","reason":"manual intake"}`

func TestFeedbackRequiresPlatformAudienceAndCSRF(t *testing.T) {
	s := &feedbackStub{}
	r := feedbackRouter(s)
	for _, tc := range []struct {
		method, path, token string
		csrf                bool
		want                int
	}{
		{"GET", "/api/platform/feedback", "", false, 401},
		{"GET", "/api/platform/feedback", "tenant-access", false, 401},
		{"GET", "/api/platform/feedback", "consumer-access", false, 401},
		{"GET", "/api/platform/feedback/categories", "tenant-access", false, 401},
		{"GET", "/api/platform/feedback", "platform-access", false, 200},
		{"POST", "/api/platform/feedback", "platform-access", false, 400},
		{"POST", "/api/platform/feedback", "consumer-access", true, 401},
		{"POST", "/api/platform/feedback", "platform-access", true, 201},
		{"POST", "/api/public/feedback", "", false, 404},
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, platformRequest(tc.method, tc.path, feedbackJSON, tc.token, tc.csrf))
		if w.Code != tc.want {
			t.Fatalf("%s %s %s: %d %s", tc.method, tc.path, tc.token, w.Code, w.Body)
		}
	}
	if s.calls != 2 || s.write.Actor.AdministratorID == "" || s.write.CorrelationID != "place-request" {
		t.Fatalf("unauthorized calls or missing context %+v", s)
	}
}
func TestFeedbackContractAndMalformedInput(t *testing.T) {
	s := &feedbackStub{}
	r := feedbackRouter(s)
	for _, body := range []string{strings.Replace(feedbackJSON, `"placeId":null,`, "", 1), strings.Replace(feedbackJSON, `"title":"Title"`, `"title":"Title","tenantId":"other"`, 1), feedbackJSON + " {}"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, platformRequest("POST", "/api/platform/feedback", body, "platform-access", true))
		if w.Code != 400 {
			t.Fatalf("malformed input accepted %d", w.Code)
		}
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, platformRequest("GET", "/api/platform/feedback?status=in_review&category=bug&limit=5&offset=10", "", "platform-access", false))
	if w.Code != 200 || s.calls != 1 || s.query.Status != "in_review" || s.query.Category != "bug" || s.query.Limit != 5 || s.query.Offset != 10 {
		t.Fatalf("query binding %+v %d", s.query, w.Code)
	}
	var page map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if _, ok := page["nextOffset"]; !ok {
		t.Fatal("missing required nextOffset")
	}
	var items []map[string]json.RawMessage
	if err := json.Unmarshal(page["items"], &items); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"id", "category", "placeId", "title", "message", "status", "outcome", "version", "createdAt", "updatedAt"} {
		if _, ok := items[0][key]; !ok {
			t.Fatalf("missing contract property %s", key)
		}
	}
}
func TestFeedbackErrorsRemainPrivate(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want int
	}{{feedback.ErrInvalid, 400}, {feedback.ErrNotFound, 404}, {feedback.ErrConflict, 409}, {errors.New("postgres private-feedback-body password-secret"), 500}} {
		s := &feedbackStub{err: tc.err}
		w := httptest.NewRecorder()
		feedbackRouter(s).ServeHTTP(w, platformRequest("PUT", "/api/platform/feedback/018bcfe5-6800-7000-8000-000000007201/review", `{"expectedVersion":1,"status":"in_review","outcome":null,"reason":"review"}`, "platform-access", true))
		if w.Code != tc.want || strings.Contains(w.Body.String(), "password-secret") || strings.Contains(w.Body.String(), "private-feedback") {
			t.Fatalf("error response %d %s", w.Code, w.Body)
		}
	}
}
