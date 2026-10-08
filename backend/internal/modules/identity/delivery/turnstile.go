// Package delivery supplies bounded external adapters for consumer identity.
package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Turnstile validates single-use proof through the fixed Cloudflare endpoint.
type Turnstile struct {
	secret, hostname string
	client           *http.Client
	now              func() time.Time
}

// NewTurnstile pins destination, action, hostname, timeout and redirect policy.
func NewTurnstile(secret, hostname string) (*Turnstile, error) {
	if secret == "" || hostname == "" || strings.ContainsAny(hostname, "/:\r\n ") {
		return nil, errors.New("invalid challenge configuration")
	}
	return &Turnstile{secret: secret, hostname: hostname, now: time.Now, client: &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

// Verify never logs proof, response diagnostics or network metadata.
func (t *Turnstile) Verify(ctx context.Context, token string, _ string) error {
	fail := errors.New("challenge verification failed")
	if token == "" || len(token) > 2048 {
		return fail
	}
	data := url.Values{"secret": {t.secret}, "response": {token}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://challenges.cloudflare.com/turnstile/v0/siteverify", strings.NewReader(data.Encode()))
	if err != nil {
		return fail
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := t.client.Do(req)
	if err != nil {
		return fail
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fail
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 16385))
	if err != nil || len(raw) > 16384 {
		return fail
	}
	var result struct {
		Success   bool      `json:"success"`
		Hostname  string    `json:"hostname"`
		Action    string    `json:"action"`
		Timestamp time.Time `json:"challenge_ts"`
	}
	if json.Unmarshal(raw, &result) != nil || !result.Success || result.Hostname != t.hostname || result.Action != "public_identity" || result.Timestamp.Before(t.now().Add(-5*time.Minute)) || result.Timestamp.After(t.now().Add(30*time.Second)) {
		return fail
	}
	return nil
}
