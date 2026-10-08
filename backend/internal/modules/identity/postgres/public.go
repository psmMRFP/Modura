package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/modura-dev/modura/backend/internal/modules/identity"
	identitydb "github.com/modura-dev/modura/backend/internal/modules/identity/postgres/db"
	"github.com/modura-dev/modura/backend/internal/platform/identifier"
)

// BootstrapCommunity refuses to adopt an unrelated tenant with the reserved slug.
func (s *Store) BootstrapCommunity(ctx context.Context, tx pgx.Tx, id identity.TenantID, now time.Time) (identity.TenantID, bool, error) {
	q := identitydb.New(tx)
	if err := q.LockCommunityBootstrap(ctx); err != nil {
		return "", false, err
	}
	row, err := q.CommunityBinding(ctx)
	if err == nil {
		if row.Slug != "community" || (row.Status != "active" && row.Status != "suspended") {
			return "", false, identity.ErrInactiveTenant
		}
		return identity.TenantID(row.ID), false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", false, err
	}
	if err := q.InsertCommunityTenant(ctx, identitydb.InsertCommunityTenantParams{ID: string(id), CreatedAt: now}); err != nil {
		return "", false, fmt.Errorf("reserved community tenant could not be created")
	}
	if err := q.InsertCommunityBinding(ctx, identitydb.InsertCommunityBindingParams{TenantID: string(id), CreatedAt: now}); err != nil {
		return "", false, err
	}
	return id, true, nil
}

// ResolveCommunity revalidates the reserved identity and lifecycle for every flow.
func (s *Store) ResolveCommunity(ctx context.Context) (identity.TenantID, error) {
	row, err := identitydb.New(s.pool).CommunityBinding(ctx)
	if err != nil || row.Slug != "community" || row.Status != "active" {
		return "", identity.ErrInactiveTenant
	}
	return identity.TenantID(row.ID), nil
}

// LockCommunity prevents suspension racing a consumer write.
func (s *Store) LockCommunity(ctx context.Context, tx pgx.Tx, id identity.TenantID) error {
	_, err := identitydb.New(tx).LockPublicIdentityTenant(ctx, string(id))
	if err != nil {
		return identity.ErrInactiveTenant
	}
	return nil
}

// CreateConsumer assigns no administrator roles or invitations.
func (s *Store) CreateConsumer(ctx context.Context, tx pgx.Tx, e identity.ConsumerEnrollment) (bool, error) {
	n, err := identitydb.New(tx).InsertConsumer(ctx, identitydb.InsertConsumerParams{ID: string(e.UserID), TenantID: string(e.TenantID), Username: "member-" + string(e.UserID), Email: textValid(e.Email), PasswordHash: textValid(e.PasswordHash), CreatedAt: e.Now})
	return n == 1, err
}

// ConsumerByEmail reads only consumer state inside the supplied tenant.
func (s *Store) ConsumerByEmail(ctx context.Context, tx pgx.Tx, t identity.TenantID, email string) (identity.PublicAccount, error) {
	r, err := identitydb.New(tx).ConsumerByEmail(ctx, identitydb.ConsumerByEmailParams{TenantID: string(t), NormalizedEmail: textValid(email)})
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.PublicAccount{}, identity.ErrUserNotFound
	}
	if err != nil {
		return identity.PublicAccount{}, err
	}
	return identity.PublicAccount{ID: identity.UserID(r.ID), Email: r.NormalizedEmail.String, Status: r.Status, SecurityVersion: r.SecurityVersion}, nil
}

// QueuePublicDelivery commits code invalidation and encrypted delivery atomically.
func (s *Store) QueuePublicDelivery(ctx context.Context, tx pgx.Tx, d identity.PublicDelivery) error {
	q := identitydb.New(tx)
	if err := q.ConsumeTenantUserOneTimeTokens(ctx, identitydb.ConsumeTenantUserOneTimeTokensParams{TenantID: string(d.TenantID), UserID: string(d.UserID), ConsumedAt: tsValid(d.Now)}); err != nil {
		return err
	}
	if err := q.CancelIdentityMail(ctx, identitydb.CancelIdentityMailParams{TenantID: string(d.TenantID), UserID: string(d.UserID)}); err != nil {
		return err
	}
	if err := q.InsertPublicIdentityToken(ctx, identitydb.InsertPublicIdentityTokenParams{ID: d.TokenID, TenantID: string(d.TenantID), UserID: string(d.UserID), Purpose: d.Purpose, TokenHash: d.TokenHash[:], CreatedAt: d.Now, ExpiresAt: d.ExpiresAt, BoundEmail: textValid(d.Email), BoundSecurityVersion: pgtype.Int8{Int64: d.SecurityVersion, Valid: true}}); err != nil {
		return err
	}
	return q.InsertIdentityMail(ctx, identitydb.InsertIdentityMailParams{ID: d.MailID, TenantID: string(d.TenantID), UserID: string(d.UserID), TokenID: d.TokenID, EncryptedPayload: d.Payload, CreatedAt: d.Now, ExpiresAt: d.ExpiresAt})
}

// CompletePublicToken binds email, purpose, security version, tenant and single use.
func (s *Store) CompletePublicToken(ctx context.Context, tx pgx.Tx, t identity.TenantID, hash [32]byte, purpose, password string, now time.Time) (identity.UserID, error) {
	q := identitydb.New(tx)
	r, err := q.LockPublicIdentityToken(ctx, identitydb.LockPublicIdentityTokenParams{TenantID: string(t), TokenHash: hash[:], Purpose: purpose})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", identity.ErrInvalidToken
	}
	if err != nil {
		return "", err
	}
	if !r.ExpiresAt.After(now) {
		return "", identity.ErrExpiredToken
	}
	if purpose == "email_verification" && r.Status == "pending_email" {
		err = q.VerifyConsumerEmail(ctx, identitydb.VerifyConsumerEmailParams{TenantID: string(t), ID: r.UserID, EmailVerifiedAt: tsValid(now)})
	} else if purpose == "password_reset" && r.Status == "active" {
		err = q.ApplyPasswordReset(ctx, identitydb.ApplyPasswordResetParams{TenantID: string(t), ID: r.UserID, PasswordHash: textValid(password), UpdatedAt: now})
	} else {
		return "", identity.ErrInvalidToken
	}
	if err != nil {
		return "", err
	}
	if err := q.ConsumeTenantUserOneTimeTokens(ctx, identitydb.ConsumeTenantUserOneTimeTokensParams{TenantID: string(t), UserID: r.UserID, ConsumedAt: tsValid(now)}); err != nil {
		return "", err
	}
	if err := q.RevokeAllUserSessions(ctx, identitydb.RevokeAllUserSessionsParams{TenantID: string(t), UserID: r.UserID, RevokedAt: tsValid(now), RevocationReason: textValid(purpose)}); err != nil {
		return "", err
	}
	if err := q.CancelIdentityMail(ctx, identitydb.CancelIdentityMailParams{TenantID: string(t), UserID: r.UserID}); err != nil {
		return "", err
	}
	return identity.UserID(r.UserID), nil
}

// PublicEvent records transactional evidence without personal content.
func (s *Store) PublicEvent(ctx context.Context, tx pgx.Tx, t identity.TenantID, u identity.UserID, action, correlation string, now time.Time) error {
	id, err := identifier.NewUUIDv7(now, nil)
	if err != nil {
		return err
	}
	actorKind, resource, resourceID := "anonymous_consumer", "identity.user", string(u)
	if u == "" {
		actorKind, resource, resourceID = "system", "identity.tenant", string(t)
	}
	return identitydb.New(tx).InsertPublicIdentityEvent(ctx, identitydb.InsertPublicIdentityEventParams{ActorKind: actorKind, Resource: resource, ResourceID: resourceID, Reason: "consumer identity lifecycle", ID: string(id), TenantID: string(t), UserID: pgUUIDOpt(string(u)), Action: action, Result: "success", CorrelationID: correlation, OccurredAt: now})
}

// LimitPublic consumes account-neutral HMAC budgets even for rejected requests.
func (s *Store) LimitPublic(ctx context.Context, tx pgx.Tx, t identity.TenantID, keys [][32]byte, limits []int32, now time.Time, window time.Duration) (bool, error) {
	q := identitydb.New(tx)
	allowed := true
	for i, key := range keys {
		n, err := q.ConsumePublicIdentityLimit(ctx, identitydb.ConsumePublicIdentityLimitParams{TenantID: string(t), KeyHash: key[:], Now: now, Cutoff: now.Add(-window)})
		if err != nil {
			return false, err
		}
		if n > limits[i] {
			allowed = false
		}
	}
	return allowed, nil
}

// LeaseMail uses atomic SKIP LOCKED, with no transaction spanning SMTP.
func (s *Store) LeaseMail(ctx context.Context, now time.Time) (identity.MailLease, error) {
	q := identitydb.New(s.pool)
	r, err := q.LeaseIdentityMail(ctx, identitydb.LeaseIdentityMailParams{Now: now, Until: now.Add(time.Minute)})
	if err != nil {
		return identity.MailLease{}, err
	}
	return identity.MailLease{ID: r.ID, TenantID: identity.TenantID(r.TenantID), Payload: r.EncryptedPayload, Attempts: r.Attempts}, nil
}

// FinishMail deletes payloads after delivery or schedules bounded retries.
func (s *Store) FinishMail(ctx context.Context, l identity.MailLease, sent bool, now time.Time) error {
	q := identitydb.New(s.pool)
	if sent || l.Attempts >= 5 {
		return q.FinishIdentityMail(ctx, identitydb.FinishIdentityMailParams{ID: l.ID, TenantID: string(l.TenantID), Attempts: l.Attempts})
	}
	return q.RetryIdentityMail(ctx, identitydb.RetryIdentityMailParams{ID: l.ID, TenantID: string(l.TenantID), Attempts: l.Attempts, AvailableAt: now.Add(time.Duration(l.Attempts*l.Attempts) * time.Minute)})
}

// PurgePublicIdentity enforces expiry even when public enrollment is disabled.
func (s *Store) PurgePublicIdentity(ctx context.Context, now time.Time) error {
	q := identitydb.New(s.pool)
	exists, err := q.PublicIdentitySchemaExists(ctx)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if err := q.PurgeIdentityMail(ctx, now); err != nil {
		return err
	}
	if err := q.PurgePublicIdentityLimits(ctx, now.Add(-24*time.Hour)); err != nil {
		return err
	}
	return q.PurgeExpiredConsumerTokens(ctx, now.Add(-24*time.Hour))
}
