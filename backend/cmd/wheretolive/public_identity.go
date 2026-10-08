package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/identity"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/identity/delivery"
	identitypostgres "github.com/psmMRFP/WhereToLive/backend/internal/modules/identity/postgres"
	"github.com/psmMRFP/WhereToLive/backend/internal/platform/config"
	"github.com/psmMRFP/WhereToLive/backend/internal/platform/database"
	"github.com/psmMRFP/WhereToLive/backend/internal/platform/identifier"
)

func configurePublicIdentity(ctx context.Context, pool *pgxpool.Pool, cfg config.Config) (*identity.PublicService, error) {
	signer, err := identity.NewAccessTokenSigner(cfg.Auth.Issuer, "wheretolive-community", cfg.Auth.SigningKeyID, cfg.Auth.SigningKey, cfg.Auth.AccessLifetime)
	if err != nil {
		return nil, err
	}
	verifier := identity.NewAccessTokenVerifier(cfg.Auth.Issuer, "wheretolive-community", map[string][]byte{cfg.Auth.SigningKeyID: cfg.Auth.SigningKey}, 5*time.Second)
	store := identitypostgres.NewConsumer(pool)
	core, err := identity.NewService(store, signer, verifier, identity.DefaultPasswordParameters(), cfg.Auth.RefreshLifetime, time.Now, func(now time.Time) (string, error) { id, err := identifier.NewUUIDv7(now, nil); return string(id), err }, func() (string, error) { return identity.NewOpaqueToken(32) })
	if err != nil {
		return nil, err
	}
	p := cfg.PublicIdentity
	var captcha identity.ChallengeVerifier
	var mailer identity.MailSender
	if p.Enabled {
		captcha, err = delivery.NewTurnstile(p.ChallengeSecret, p.ChallengeHostname)
		if err != nil {
			return nil, err
		}
		mailer, err = delivery.NewSMTP(p.SMTPAddress, p.SMTPUsername, p.SMTPPassword, p.SMTPFrom, p.Origin)
		if err != nil {
			return nil, err
		}
	}
	service, err := identity.NewPublicService(store, database.NewTransactor(pool), core, captcha, mailer, identity.PublicOptions{Enabled: p.Enabled, EncryptionKey: p.EncryptionKey, VerificationLifetime: p.VerificationLifetime, ResetLifetime: p.ResetLifetime, RateWindow: p.RateWindow, GlobalLimit: p.GlobalLimit, NetworkLimit: p.NetworkLimit, AccountLimit: p.AccountLimit})
	if err != nil {
		return nil, err
	}
	if p.Enabled {
		bootstrapCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if err := service.Bootstrap(bootstrapCtx); err != nil {
			return nil, err
		}
	}
	return service, nil
}
func runIdentityMail(ctx context.Context, service *identity.PublicService, logger *slog.Logger) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			maintenanceCtx, maintenanceCancel := context.WithTimeout(ctx, 10*time.Second)
			maintenanceErr := service.Maintain(maintenanceCtx)
			maintenanceCancel()
			if maintenanceErr != nil && ctx.Err() == nil {
				logger.Error("consumer identity retention cleanup failed")
			}
			if !service.Enabled() {
				continue
			}
			for i := 0; i < 20; i++ {
				taskCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
				found, err := service.DeliverOne(taskCtx)
				cancel()
				if ctx.Err() != nil {
					return
				}
				if err != nil {
					logger.Error("consumer identity mail delivery failed")
				}
				if !found {
					break
				}
			}
		}
	}
}
