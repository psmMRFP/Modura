package config

import (
	"testing"
	"time"
)

func TestFromEnvUsesDefaults(t *testing.T) {
	setRequired(t)
	for _, name := range []string{"MODURA_HTTP_ADDRESS", "MODURA_HTTP_READ_TIMEOUT", "MODURA_HTTP_READ_HEADER_TIMEOUT", "MODURA_HTTP_WRITE_TIMEOUT", "MODURA_HTTP_IDLE_TIMEOUT", "MODURA_HTTP_SHUTDOWN_TIMEOUT", "MODURA_HTTP_MAX_HEADER_BYTES", "MODURA_HTTP_MAX_BODY_BYTES", "MODURA_HTTP_ALLOWED_ORIGINS", "MODURA_AUTH_COOKIE_SECURE", "MODURA_AUTH_ACCESS_LIFETIME", "MODURA_AUTH_REFRESH_LIFETIME", "MODURA_AUTH_INVITATION_LIFETIME"} {
		t.Setenv(name, "")
	}
	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv() error = %v", err)
	}
	if cfg.HTTP.Address != defaultAddress {
		t.Fatalf("Address = %q, want %q", cfg.HTTP.Address, defaultAddress)
	}
	if cfg.HTTP.ReadTimeout != defaultReadTimeout {
		t.Fatalf("ReadTimeout = %v, want %v", cfg.HTTP.ReadTimeout, defaultReadTimeout)
	}
	if cfg.HTTP.ReadHeaderTimeout != defaultReadHeaderTime {
		t.Fatalf("ReadHeaderTimeout = %v, want %v", cfg.HTTP.ReadHeaderTimeout, defaultReadHeaderTime)
	}
	if cfg.HTTP.MaxBodyBytes != defaultMaxBodyBytes {
		t.Fatalf("MaxBodyBytes = %d, want %d", cfg.HTTP.MaxBodyBytes, defaultMaxBodyBytes)
	}
	if len(cfg.HTTP.AllowedOrigins) != 0 {
		t.Fatalf("AllowedOrigins = %v, want an empty least-privilege allowlist", cfg.HTTP.AllowedOrigins)
	}
	if cfg.Auth.PlatformAudience != "modura-platform" {
		t.Fatalf("PlatformAudience = %q", cfg.Auth.PlatformAudience)
	}
	if cfg.Auth.InvitationLifetime != 24*time.Hour {
		t.Fatalf("InvitationLifetime = %v", cfg.Auth.InvitationLifetime)
	}
}

func TestFromEnvParsesOriginAllowlist(t *testing.T) {
	setRequired(t)
	t.Setenv("MODURA_HTTP_ALLOWED_ORIGINS", " https://admin.example.com , https://staging.example.com ,,")
	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv() error = %v", err)
	}
	if len(cfg.HTTP.AllowedOrigins) != 2 || cfg.HTTP.AllowedOrigins[0] != "https://admin.example.com" {
		t.Fatalf("AllowedOrigins = %v", cfg.HTTP.AllowedOrigins)
	}
}

func TestFromEnvRejectsInvalidDuration(t *testing.T) {
	setRequired(t)
	t.Setenv("MODURA_HTTP_READ_TIMEOUT", "never")
	if _, err := FromEnv(); err == nil {
		t.Fatal("FromEnv() error = nil, want an error")
	}
}

func TestFromEnvReadsValues(t *testing.T) {
	setRequired(t)
	t.Setenv("MODURA_HTTP_ADDRESS", "127.0.0.1:9000")
	t.Setenv("MODURA_HTTP_READ_TIMEOUT", "3s")
	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv() error = %v", err)
	}
	if cfg.HTTP.Address != "127.0.0.1:9000" || cfg.HTTP.ReadTimeout != 3*time.Second {
		t.Fatalf("unexpected config: %+v", cfg.HTTP)
	}
}

func TestFromEnvRequiresSecrets(t *testing.T) {
	t.Setenv("MODURA_DATABASE_URL", "")
	t.Setenv("MODURA_AUTH_SIGNING_KEY", "")
	if _, err := FromEnv(); err == nil {
		t.Fatal("FromEnv() error = nil, want an error")
	}
}

func setRequired(t *testing.T) {
	t.Helper()
	t.Setenv("MODURA_DATABASE_URL", "postgres://modura@localhost/modura")
	t.Setenv("MODURA_DATABASE_AUTO_CREATE", "")
	t.Setenv("MODURA_AUTH_SIGNING_KEY", "test-only-signing-key-with-32-bytes")
}

func TestDatabaseCreationConfig(t *testing.T) {
	setRequired(t)
	t.Setenv("MODURA_DATABASE_AUTO_CREATE", "")
	cfg, err := FromEnv()
	if err != nil || !cfg.Database.AutoCreate {
		t.Fatalf("default creation setting: %v", err)
	}
	t.Setenv("MODURA_DATABASE_AUTO_CREATE", "false")
	cfg, err = FromEnv()
	if err != nil || cfg.Database.AutoCreate {
		t.Fatalf("disabled creation setting: %v", err)
	}
	t.Setenv("MODURA_DATABASE_AUTO_CREATE", "invalid")
	if _, err = FromEnv(); err == nil {
		t.Fatal("invalid boolean accepted")
	}
}
func TestDatabaseConfigRejectsPostgres(t *testing.T) {
	setRequired(t)
	for _, dsn := range []string{"postgres://app@localhost/postgres", "postgres://postgres@localhost/wheretolive_test"} {
		t.Setenv("MODURA_DATABASE_URL", dsn)
		if _, err := FromEnv(); err == nil {
			t.Fatal("default postgres database/role accepted")
		}
	}
}

func TestDatabaseOnlyConfigurationNeedsNoSigningKey(t *testing.T) {
	setRequired(t)
	t.Setenv("MODURA_AUTH_SIGNING_KEY", "")
	if _, err := DatabaseFromEnv(); err != nil {
		t.Fatal(err)
	}
}
