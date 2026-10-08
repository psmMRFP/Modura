package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/identity"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/platformadmin"
	"github.com/psmMRFP/WhereToLive/backend/internal/platform/database/migrationtest"
)

func TestPlatformAuthenticationIsDistinctAndReplaySafe(t *testing.T) {
	pool := integrationPool(t)
	now := time.Unix(1_700_000_000, 0).UTC()
	key := []byte(strings.Repeat("p", 32))
	signer, err := identity.NewAccessTokenSigner("wheretolive", "wheretolive-platform", "platform", key, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	verifier := identity.NewAccessTokenVerifier("wheretolive", "wheretolive-platform", map[string][]byte{"platform": key}, 0)
	sequence := 0
	newID := func(time.Time) (string, error) {
		sequence++
		return fmt.Sprintf("018bcfe5-6800-7000-8000-%012d", 800+sequence), nil
	}
	newSecret := func() (string, error) { sequence++; return fmt.Sprintf("%064d", sequence), nil }
	service, err := platformadmin.NewService(New(pool), signer, verifier, identity.DefaultPasswordParameters(), time.Hour, func() time.Time { return now }, newID, newSecret)
	if err != nil {
		t.Fatal(err)
	}
	administratorID, err := service.Bootstrap(context.Background(), "PlatformAdmin", "a secure platform password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Bootstrap(context.Background(), "Another", "another secure password"); !errors.Is(err, platformadmin.ErrBootstrapComplete) {
		t.Fatalf("second bootstrap error = %v", err)
	}
	tokens, err := service.Login(context.Background(), " platformadmin ", "a secure platform password")
	if err != nil {
		t.Fatal(err)
	}
	actor, err := service.AuthenticateAccess(context.Background(), tokens.AccessToken)
	if err != nil || actor.AdministratorID != administratorID {
		t.Fatalf("actor=%+v err=%v", actor, err)
	}
	if err := service.Logout(context.Background(), actor); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AuthenticateAccess(context.Background(), tokens.AccessToken); !errors.Is(err, platformadmin.ErrInvalidToken) {
		t.Fatalf("logged-out access error = %v", err)
	}
	tokens, err = service.Login(context.Background(), "platformadmin", "a secure platform password")
	if err != nil {
		t.Fatal(err)
	}
	tenantToken, err := signer.Sign(identity.Actor{TenantID: "tenant", UserID: "user", SessionID: "session"}, "tenant-token", 1, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.AuthenticateAccess(context.Background(), tenantToken); !errors.Is(err, platformadmin.ErrInvalidToken) {
		t.Fatalf("tenant token error = %v", err)
	}
	rotated, err := service.Refresh(context.Background(), tokens.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Refresh(context.Background(), tokens.RefreshToken); !errors.Is(err, platformadmin.ErrInvalidToken) {
		t.Fatalf("replay error = %v", err)
	}
	if _, err := service.AuthenticateAccess(context.Background(), rotated.AccessToken); !errors.Is(err, platformadmin.ErrInvalidToken) {
		t.Fatalf("replayed family access error = %v", err)
	}
}

func integrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("WHERETOLIVE_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("WHERETOLIVE_TEST_DATABASE_URL is not set")
	}
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(config.ConnConfig.Database, "_test") {
		t.Fatalf("refusing destructive integration setup for database %q: name must end in _test", config.ConnConfig.Database)
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	migrationtest.Prepare(t, pool)
	return pool
}
