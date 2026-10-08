// Command wheretolive-bootstrap creates the first global platform administrator.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/psmMRFP/WhereToLive/backend/internal/modules/identity"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/platformadmin"
	platformadminpostgres "github.com/psmMRFP/WhereToLive/backend/internal/modules/platformadmin/postgres"
	"github.com/psmMRFP/WhereToLive/backend/internal/platform/config"
	"github.com/psmMRFP/WhereToLive/backend/internal/platform/database"
	"github.com/psmMRFP/WhereToLive/backend/internal/platform/identifier"
)

func main() {
	username := flag.String("username", "", "platform administrator username")
	flag.Parse()
	if strings.TrimSpace(*username) == "" {
		fail("-username is required")
	}

	password, err := readPassword(os.Stdin)
	if err != nil {
		fail("read password: %v", err)
	}
	cfg, err := config.FromEnv()
	if err != nil {
		fail("load configuration: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, _, err := database.Open(ctx, cfg.Database.URL, cfg.Database.AutoCreate)
	if err != nil {
		fail("configure database: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		fail("connect database: %v", err)
	}

	signer, err := identity.NewAccessTokenSigner(cfg.Auth.Issuer, cfg.Auth.PlatformAudience, cfg.Auth.SigningKeyID, cfg.Auth.SigningKey, cfg.Auth.AccessLifetime)
	if err != nil {
		fail("configure platform access tokens: %v", err)
	}
	verifier := identity.NewAccessTokenVerifier(cfg.Auth.Issuer, cfg.Auth.PlatformAudience, map[string][]byte{cfg.Auth.SigningKeyID: cfg.Auth.SigningKey}, 5*time.Second)
	service, err := platformadmin.NewService(platformadminpostgres.New(pool), signer, verifier, identity.DefaultPasswordParameters(), cfg.Auth.RefreshLifetime, time.Now, func(now time.Time) (string, error) {
		id, idErr := identifier.NewUUIDv7(now, nil)
		return string(id), idErr
	}, func() (string, error) { return identity.NewOpaqueToken(32) })
	if err != nil {
		fail("configure platform administrator service: %v", err)
	}
	id, err := service.Bootstrap(ctx, *username, password)
	if errors.Is(err, platformadmin.ErrBootstrapComplete) {
		fail("platform bootstrap is already complete")
	}
	if err != nil {
		fail("bootstrap platform administrator: %v", err)
	}
	fmt.Printf("platform administrator created: %s\n", id)
}

func readPassword(reader io.Reader) (string, error) {
	line, err := bufio.NewReader(io.LimitReader(reader, 4097)).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	password := strings.TrimRight(line, "\r\n")
	if password == "" {
		return "", fmt.Errorf("password is required on standard input")
	}
	return password, nil
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
