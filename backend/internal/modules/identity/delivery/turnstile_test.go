package delivery

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type testRoundTrip func(*http.Request) (*http.Response, error)

func (f testRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestTurnstileBindsHostnameActionAndFreshness(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	verifier, err := NewTurnstile("secret", "example.org")
	if err != nil {
		t.Fatal(err)
	}
	verifier.now = func() time.Time { return now }
	for _, body := range []string{
		`{"success":true,"hostname":"example.org","action":"public_identity","challenge_ts":"2026-10-08T12:00:00Z"}`,
		`{"success":false}`, `{"success":true,"hostname":"evil.example","action":"public_identity","challenge_ts":"2026-10-08T12:00:00Z"}`,
		`{"success":true,"hostname":"example.org","action":"other","challenge_ts":"2026-10-08T12:00:00Z"}`,
		`{"success":true,"hostname":"example.org","action":"public_identity","challenge_ts":"2026-10-08T11:00:00Z"}`,
	} {
		verifier.client.Transport = testRoundTrip(func(r *http.Request) (*http.Response, error) {
			if r.URL.String() != "https://challenges.cloudflare.com/turnstile/v0/siteverify" || r.Method != "POST" {
				t.Fatal("untrusted endpoint")
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		})
		err := verifier.Verify(context.Background(), "one-time-proof", "192.0.2.1")
		if strings.Contains(body, `"action":"public_identity"`) && strings.Contains(body, `"hostname":"example.org"`) && strings.Contains(body, `12:00:00Z`) {
			if err != nil {
				t.Fatal("valid challenge failed")
			}
		} else if err == nil {
			t.Fatal("invalid challenge accepted")
		}
	}
}
func TestSMTPRejectsUntrustedConfiguration(t *testing.T) {
	for _, origin := range []string{"http://example.org", "https://example.org/path", "https://user:password@example.org", "https://example.org?redirect=x"} {
		if _, err := NewSMTP("mail.example.org:465", "user", "password", "sender@example.org", origin); err == nil {
			t.Fatal("unsafe origin accepted")
		}
	}
	if _, err := NewSMTP("mail.example.org:465", "user", "password", "sender@example.org", "https://example.org"); err != nil {
		t.Fatal(err)
	}
}
