package config

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestPublicIdentityDefaultsClosedAndRequiresCompleteDeployment(t *testing.T) {
	t.Setenv("WHERETOLIVE_PUBLIC_IDENTITY_ENABLED", "")
	p, err := publicIdentityFromEnv()
	if err != nil || p.Enabled {
		t.Fatal("public accounts enabled by default")
	}
	t.Setenv("WHERETOLIVE_PUBLIC_IDENTITY_ENABLED", "true")
	t.Setenv("WHERETOLIVE_IDENTITY_MAIL_KEY", "a secret that must never be echoed")
	_, err = publicIdentityFromEnv()
	if err == nil || strings.Contains(err.Error(), "never be echoed") {
		t.Fatal("invalid key accepted or leaked")
	}
	t.Setenv("WHERETOLIVE_IDENTITY_MAIL_KEY", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", 32))))
	t.Setenv("WHERETOLIVE_SMTP_ADDRESS", "smtp.example.org:465")
	t.Setenv("WHERETOLIVE_SMTP_USERNAME", "user")
	t.Setenv("WHERETOLIVE_SMTP_PASSWORD", "private-password")
	t.Setenv("WHERETOLIVE_SMTP_FROM", "sender@example.org")
	t.Setenv("WHERETOLIVE_PUBLIC_ORIGIN", "https://example.org")
	t.Setenv("WHERETOLIVE_TURNSTILE_SECRET", "live-secret")
	t.Setenv("WHERETOLIVE_TURNSTILE_SITE_KEY", "live-site-key")
	t.Setenv("WHERETOLIVE_TURNSTILE_HOSTNAME", "example.org")
	p, err = publicIdentityFromEnv()
	if err != nil || !p.Enabled {
		t.Fatal(err)
	}
	t.Setenv("WHERETOLIVE_TURNSTILE_HOSTNAME", "another.example")
	if _, err := publicIdentityFromEnv(); err == nil {
		t.Fatal("unbound CAPTCHA accepted")
	}
	t.Setenv("WHERETOLIVE_TURNSTILE_HOSTNAME", "example.org")
	t.Setenv("WHERETOLIVE_PUBLIC_VERIFY_LIFETIME", "25h")
	if _, err := publicIdentityFromEnv(); err == nil {
		t.Fatal("unbounded token lifetime")
	}
}
