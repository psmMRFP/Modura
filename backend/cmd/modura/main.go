// Package main composes and runs the Modura backend.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/modura-dev/modura/backend/internal/modules/audit"
	auditpostgres "github.com/modura-dev/modura/backend/internal/modules/audit/postgres"
	"github.com/modura-dev/modura/backend/internal/modules/authorization"
	authorizationpostgres "github.com/modura-dev/modura/backend/internal/modules/authorization/postgres"
	"github.com/modura-dev/modura/backend/internal/modules/identity"
	identitypostgres "github.com/modura-dev/modura/backend/internal/modules/identity/postgres"
	"github.com/modura-dev/modura/backend/internal/modules/organization"
	organizationpostgres "github.com/modura-dev/modura/backend/internal/modules/organization/postgres"
	"github.com/modura-dev/modura/backend/internal/modules/places"
	placespostgres "github.com/modura-dev/modura/backend/internal/modules/places/postgres"
	"github.com/modura-dev/modura/backend/internal/modules/platformadmin"
	platformadminpostgres "github.com/modura-dev/modura/backend/internal/modules/platformadmin/postgres"
	"github.com/modura-dev/modura/backend/internal/modules/platformtenant"
	"github.com/modura-dev/modura/backend/internal/modules/provisioning"
	"github.com/modura-dev/modura/backend/internal/modules/settings"
	settingspostgres "github.com/modura-dev/modura/backend/internal/modules/settings/postgres"
	"github.com/modura-dev/modura/backend/internal/platform/config"
	"github.com/modura-dev/modura/backend/internal/platform/database"
	"github.com/modura-dev/modura/backend/internal/platform/httpserver"
	"github.com/modura-dev/modura/backend/internal/platform/identifier"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.FromEnv()
	if err != nil {
		logger.Error("load configuration", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	startupCtx, cancelStartup := context.WithTimeout(ctx, 10*time.Second)
	defer cancelStartup()
	pool, created, err := database.Open(startupCtx, cfg.Database.URL, cfg.Database.AutoCreate)
	if err != nil {
		logger.Error("initialize application database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	if created {
		logger.Info("application database created; apply database migrations before using business APIs")
	}

	signer, err := identity.NewAccessTokenSigner(cfg.Auth.Issuer, cfg.Auth.Audience, cfg.Auth.SigningKeyID, cfg.Auth.SigningKey, cfg.Auth.AccessLifetime)
	if err != nil {
		logger.Error("configure access tokens", "error", err)
		os.Exit(1)
	}
	verifier := identity.NewAccessTokenVerifier(cfg.Auth.Issuer, cfg.Auth.Audience, map[string][]byte{cfg.Auth.SigningKeyID: cfg.Auth.SigningKey}, 5*time.Second)
	identityService, err := identity.NewService(identitypostgres.New(pool), signer, verifier, identity.DefaultPasswordParameters(), cfg.Auth.RefreshLifetime, time.Now, func(now time.Time) (string, error) {
		id, idErr := identifier.NewUUIDv7(now, nil)
		return string(id), idErr
	}, func() (string, error) { return identity.NewOpaqueToken(32) })
	if err != nil {
		logger.Error("configure identity service", "error", err)
		os.Exit(1)
	}
	authorizationService, err := authorization.NewService(authorizationpostgres.New(pool))
	if err != nil {
		logger.Error("configure authorization service", "error", err)
		os.Exit(1)
	}
	auditStore := auditpostgres.New(pool)
	auditService, err := audit.NewService(auditStore, func(now time.Time) (string, error) {
		id, idErr := identifier.NewUUIDv7(now, nil)
		return string(id), idErr
	})
	if err != nil {
		logger.Error("configure audit service", "error", err)
		os.Exit(1)
	}
	if err := auditService.EnableQueries(auditStore); err != nil {
		logger.Error("configure audit queries", "error", err)
		os.Exit(1)
	}
	if err := identityService.EnableUserManagement(database.NewTransactor(pool), accountAuditor{service: auditService}); err != nil {
		logger.Error("configure identity user management", "error", err)
		os.Exit(1)
	}
	if err := authorizationService.EnableManagement(authorizationpostgres.New(pool), database.NewTransactor(pool), auditService, identityService, time.Now, func(now time.Time) (string, error) {
		id, idErr := identifier.NewUUIDv7(now, nil)
		return string(id), idErr
	}); err != nil {
		logger.Error("configure authorization management", "error", err)
		os.Exit(1)
	}
	settingsService, err := settings.NewService(settingspostgres.New(pool), database.NewTransactor(pool), auditService, time.Now, func(now time.Time) (string, error) {
		id, idErr := identifier.NewUUIDv7(now, nil)
		return string(id), idErr
	})
	if err != nil {
		logger.Error("configure settings service", "error", err)
		os.Exit(1)
	}
	organizationStore := organizationpostgres.New(pool)
	organizationService, err := organization.NewService(organizationStore, database.NewTransactor(pool), auditService, time.Now, func(now time.Time) (string, error) {
		id, idErr := identifier.NewUUIDv7(now, nil)
		return string(id), idErr
	})
	if err != nil {
		logger.Error("configure organization service", "error", err)
		os.Exit(1)
	}
	platformSigner, err := identity.NewAccessTokenSigner(cfg.Auth.Issuer, cfg.Auth.PlatformAudience, cfg.Auth.SigningKeyID, cfg.Auth.SigningKey, cfg.Auth.AccessLifetime)
	if err != nil {
		logger.Error("configure platform access tokens", "error", err)
		os.Exit(1)
	}
	platformVerifier := identity.NewAccessTokenVerifier(cfg.Auth.Issuer, cfg.Auth.PlatformAudience, map[string][]byte{cfg.Auth.SigningKeyID: cfg.Auth.SigningKey}, 5*time.Second)
	platformAdminService, err := platformadmin.NewService(platformadminpostgres.New(pool), platformSigner, platformVerifier, identity.DefaultPasswordParameters(), cfg.Auth.RefreshLifetime, time.Now, func(now time.Time) (string, error) {
		id, idErr := identifier.NewUUIDv7(now, nil)
		return string(id), idErr
	}, func() (string, error) { return identity.NewOpaqueToken(32) })
	if err != nil {
		logger.Error("configure platform administrator service", "error", err)
		os.Exit(1)
	}
	platformTenantService, err := platformtenant.NewService(identityService, database.NewTransactor(pool), auditService, time.Now, func(now time.Time) (string, error) {
		id, idErr := identifier.NewUUIDv7(now, nil)
		return string(id), idErr
	})
	if err != nil {
		logger.Error("configure platform tenant service", "error", err)
		os.Exit(1)
	}
	provisioningService, err := provisioning.NewService(pool, identityService, organizationService, authorizationService, auditService, time.Now, func(now time.Time) (string, error) {
		id, idErr := identifier.NewUUIDv7(now, nil)
		return string(id), idErr
	}, func() (string, error) { return identity.NewOpaqueToken(32) }, cfg.Auth.InvitationLifetime)
	if err != nil {
		logger.Error("configure tenant provisioning service", "error", err)
		os.Exit(1)
	}
	placesService, err := places.NewService(placespostgres.New(pool))
	if err != nil {
		logger.Error("configure places service", "error", err)
		os.Exit(1)
	}
	placeManagement, err := places.NewManagement(placespostgres.New(pool), database.NewTransactor(pool), auditService, time.Now, func(now time.Time) (string, error) { id, err := identifier.NewUUIDv7(now, nil); return string(id), err })
	if err != nil {
		logger.Error("configure place management", "error", err)
		os.Exit(1)
	}
	publicIdentity, err := configurePublicIdentity(ctx, pool, cfg)
	if err != nil {
		logger.Error("configure consumer identity", "error", err)
		os.Exit(1)
	}
	mailDone := make(chan struct{})
	go func() { defer close(mailDone); runIdentityMail(ctx, publicIdentity, logger) }()
	defer func() { stop(); <-mailDone }()
	server := httpserver.New(cfg.HTTP, logger, httpserver.Dependencies{PublicIdentity: publicIdentity, PublicChallengeSiteKey: cfg.PublicIdentity.ChallengeSiteKey, PublicIdentityFailure: func() { logger.Error("consumer identity enqueue failed") }, PlatformPlaces: placeManagement, Places: placesService, Identity: identityService, Authorizer: authorizationService, Authorization: authorizationService, Organization: organizationService, PlatformAdmin: platformAdminService, PlatformTenant: platformTenantService, Provisioning: provisioningService, Settings: settingsService, PlatformSettings: settingsService, Audit: auditService, PlatformAudit: auditService, Ready: pool.Ping})

	errCh := make(chan error, 1)
	go func() {
		logger.Info("http server starting", "address", server.Addr)
		errCh <- server.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server stopped unexpectedly", "error", err)
			os.Exit(1)
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("http server shutdown", "error", err)
			os.Exit(1)
		}
	}

	logger.Info("http server stopped")
}

// accountAuditor adapts the audit module's transactional recorder to the
// identity module's account audit contract, which cannot import audit
// directly because audit already depends on identity types.
type accountAuditor struct {
	service *audit.Service
}

func (a accountAuditor) RecordAccountEvent(ctx context.Context, tx pgx.Tx, event identity.AccountAuditEvent) error {
	return a.service.RecordTenantWrite(ctx, tx, audit.Event{ActorID: event.Actor.UserID, TenantID: event.Actor.TenantID, Action: event.Action, Resource: event.Resource, ResourceID: event.ResourceID, Reason: event.Reason, CorrelationID: event.CorrelationID, OccurredAt: event.OccurredAt, BeforeState: event.BeforeState, AfterState: event.AfterState})
}
