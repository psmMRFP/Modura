// Package config loads and validates process configuration.
package config

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/modura-dev/modura/backend/internal/platform/database"
)

const (
	defaultAddress         = ":8080"
	defaultReadTimeout     = 10 * time.Second
	defaultReadHeaderTime  = 5 * time.Second
	defaultWriteTimeout    = 15 * time.Second
	defaultIdleTimeout     = 60 * time.Second
	defaultShutdownTimeout = 10 * time.Second
	defaultMaxHeaderBytes  = 1 << 20
	defaultMaxBodyBytes    = 1 << 20
)

// Config contains the validated application configuration.
type Config struct {
	PublicIdentity PublicIdentity
	HTTP           HTTP
	Database       Database
	Auth           Auth
}

// Database contains PostgreSQL connection configuration.
type Database struct {
	URL        string
	AutoCreate bool
}

// Auth contains authentication and session security configuration.
type Auth struct {
	Issuer             string
	Audience           string
	PlatformAudience   string
	SigningKeyID       string
	SigningKey         []byte
	AccessLifetime     time.Duration
	RefreshLifetime    time.Duration
	InvitationLifetime time.Duration
}

// HTTP contains HTTP server configuration.
type HTTP struct {
	Address           string
	ReadTimeout       time.Duration
	ReadHeaderTimeout time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
	MaxHeaderBytes    int
	MaxBodyBytes      int64
	AllowedOrigins    []string
	CookieSecure      bool
}

// FromEnv loads configuration from environment variables and applies defaults.
func FromEnv() (Config, error) {
	httpConfig := HTTP{Address: envOrDefault("MODURA_HTTP_ADDRESS", defaultAddress)}
	var err error
	if httpConfig.ReadTimeout, err = duration("MODURA_HTTP_READ_TIMEOUT", defaultReadTimeout); err != nil {
		return Config{}, err
	}
	if httpConfig.WriteTimeout, err = duration("MODURA_HTTP_WRITE_TIMEOUT", defaultWriteTimeout); err != nil {
		return Config{}, err
	}
	if httpConfig.IdleTimeout, err = duration("MODURA_HTTP_IDLE_TIMEOUT", defaultIdleTimeout); err != nil {
		return Config{}, err
	}
	if httpConfig.ShutdownTimeout, err = duration("MODURA_HTTP_SHUTDOWN_TIMEOUT", defaultShutdownTimeout); err != nil {
		return Config{}, err
	}
	if httpConfig.MaxHeaderBytes, err = integer("MODURA_HTTP_MAX_HEADER_BYTES", defaultMaxHeaderBytes); err != nil {
		return Config{}, err
	}
	if httpConfig.ReadHeaderTimeout, err = duration("MODURA_HTTP_READ_HEADER_TIMEOUT", defaultReadHeaderTime); err != nil {
		return Config{}, err
	}
	if httpConfig.MaxBodyBytes, err = integer64("MODURA_HTTP_MAX_BODY_BYTES", defaultMaxBodyBytes); err != nil {
		return Config{}, err
	}
	// Least-privilege CORS: without an explicit allowlist no cross-origin
	// request receives CORS headers, so browsers fall back to same-origin.
	for _, origin := range strings.Split(os.Getenv("MODURA_HTTP_ALLOWED_ORIGINS"), ",") {
		if trimmed := strings.TrimSpace(origin); trimmed != "" {
			httpConfig.AllowedOrigins = append(httpConfig.AllowedOrigins, trimmed)
		}
	}
	if httpConfig.CookieSecure, err = boolean("MODURA_AUTH_COOKIE_SECURE", true); err != nil {
		return Config{}, err
	}
	databaseConfig, err := DatabaseFromEnv()
	if err != nil {
		return Config{}, err
	}

	signingKey := []byte(os.Getenv("MODURA_AUTH_SIGNING_KEY"))
	if len(signingKey) < 32 {
		return Config{}, fmt.Errorf("MODURA_AUTH_SIGNING_KEY must contain at least 32 bytes")
	}
	auth := Auth{
		Issuer:           envOrDefault("MODURA_AUTH_ISSUER", "modura"),
		Audience:         envOrDefault("MODURA_AUTH_AUDIENCE", "modura-admin"),
		PlatformAudience: envOrDefault("MODURA_PLATFORM_AUTH_AUDIENCE", "modura-platform"),
		SigningKeyID:     envOrDefault("MODURA_AUTH_SIGNING_KEY_ID", "primary"),
		SigningKey:       signingKey,
	}
	if auth.Audience == "wheretolive-community" || auth.PlatformAudience == "wheretolive-community" || auth.Audience == auth.PlatformAudience {
		return Config{}, fmt.Errorf("authentication audiences must be distinct")
	}
	if auth.AccessLifetime, err = duration("MODURA_AUTH_ACCESS_LIFETIME", 5*time.Minute); err != nil {
		return Config{}, err
	}
	if auth.RefreshLifetime, err = duration("MODURA_AUTH_REFRESH_LIFETIME", 24*time.Hour); err != nil {
		return Config{}, err
	}
	if auth.InvitationLifetime, err = duration("MODURA_AUTH_INVITATION_LIFETIME", 24*time.Hour); err != nil {
		return Config{}, err
	}
	publicIdentity, err := publicIdentityFromEnv()
	if err != nil {
		return Config{}, err
	}
	if publicIdentity.Enabled && !httpConfig.CookieSecure {
		return Config{}, fmt.Errorf("public identity requires Secure cookies")
	}
	if publicIdentity.Enabled && bytes.Equal(publicIdentity.EncryptionKey, signingKey) {
		return Config{}, fmt.Errorf("identity mail encryption key must be independent of the signing key")
	}
	return Config{PublicIdentity: publicIdentity, HTTP: httpConfig, Database: databaseConfig, Auth: auth}, nil
}

func boolean(name string, fallback bool) (bool, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("parse %s: %w", name, err)
	}
	return parsed, nil
}

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func duration(name string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("%s must be positive", name)
	}
	return parsed, nil
}

func integer(name string, fallback int) (int, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("%s must be positive", name)
	}
	return parsed, nil
}

func integer64(name string, fallback int64) (int64, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("%s must be positive", name)
	}
	return parsed, nil
}

// DatabaseFromEnv validates database-only configuration for provisioning tools.
func DatabaseFromEnv() (Database, error) {
	url := strings.TrimSpace(os.Getenv("MODURA_DATABASE_URL"))
	if url == "" {
		return Database{}, fmt.Errorf("MODURA_DATABASE_URL is required")
	}
	if err := database.ValidateURL(url); err != nil {
		return Database{}, fmt.Errorf("MODURA_DATABASE_URL: %w", err)
	}
	autoCreate, err := boolean("MODURA_DATABASE_AUTO_CREATE", true)
	if err != nil {
		return Database{}, err
	}
	return Database{URL: url, AutoCreate: autoCreate}, nil
}
