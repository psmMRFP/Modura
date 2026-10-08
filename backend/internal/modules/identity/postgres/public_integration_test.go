package postgres

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/identity"
	"github.com/psmMRFP/WhereToLive/backend/internal/platform/database"
	"github.com/psmMRFP/WhereToLive/backend/internal/platform/identifier"
)

type integrationChallenge struct{}

func (integrationChallenge) Verify(context.Context, string, string) error { return nil }

type integrationMailbox struct{ message identity.PublicMail }

func (m *integrationMailbox) Send(_ context.Context, message identity.PublicMail) error {
	m.message = message
	return nil
}
func consumerService(t *testing.T, store *Store, transactions identity.Transactor, mail *integrationMailbox, now time.Time) *identity.PublicService {
	t.Helper()
	signer, err := identity.NewAccessTokenSigner("test", "wheretolive-community", "test", []byte(strings.Repeat("x", 32)), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	verifier := identity.NewAccessTokenVerifier("test", "wheretolive-community", map[string][]byte{"test": []byte(strings.Repeat("x", 32))}, 0)
	core, err := identity.NewService(store, signer, verifier, identity.DefaultPasswordParameters(), time.Hour, func() time.Time { return now }, func(now time.Time) (string, error) { id, err := identifier.NewUUIDv7(now, nil); return string(id), err }, func() (string, error) { return identity.NewOpaqueToken(32) })
	if err != nil {
		t.Fatal(err)
	}
	service, err := identity.NewPublicService(store, transactions, core, integrationChallenge{}, mail, identity.PublicOptions{Enabled: true, EncryptionKey: []byte(strings.Repeat("y", 32)), VerificationLifetime: time.Hour, ResetLifetime: 15 * time.Minute, RateWindow: time.Hour, GlobalLimit: 1000, NetworkLimit: 100, AccountLimit: 50})
	if err != nil {
		t.Fatal(err)
	}
	return service
}
func TestPublicIdentityRegistrationVerificationRecoveryAndSessionIsolation(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	now := time.Now().UTC()
	store := NewConsumer(pool)
	transactions := database.NewTransactor(pool)
	mail := &integrationMailbox{}
	service := consumerService(t, store, transactions, mail, now)
	if err := service.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	tenant, err := store.ResolveCommunity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	failures := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); failures <- service.Bootstrap(ctx) }()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	request := identity.PublicRequest{Email: "Person@Example.org", Password: "correct password for test", Challenge: "proof", Network: "192.0.2.1", CorrelationID: "public-integration"}
	if err := service.Request(ctx, "register", request); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Login(ctx, request); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Fatal("pending account logged in")
	}
	if _, err := service.DeliverOne(ctx); err != nil {
		t.Fatal(err)
	}
	verification := mail.message.Code
	if verification == "" {
		t.Fatal("no verification delivery")
	}
	if err := service.Complete(ctx, "password_reset", verification, "new password for test", request); !errors.Is(err, identity.ErrInvalidToken) {
		t.Fatal("wrong purpose accepted")
	}
	if err := service.Complete(ctx, "email_verification", verification, "", request); err != nil {
		t.Fatal(err)
	}
	if err := service.Complete(ctx, "email_verification", verification, "", request); !errors.Is(err, identity.ErrInvalidToken) {
		t.Fatal("code replay accepted")
	}
	tokens, err := service.Login(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := service.Authenticate(ctx, tokens.AccessToken)
	if err != nil || actor.TenantID != tenant {
		t.Fatal("consumer session missing", err)
	}
	ordinary := New(pool)
	if _, err := ordinary.FindActiveAccount(ctx, "community", "person@example.org"); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Fatal("consumer accepted by tenant login")
	}
	presented := identity.HashOpaqueToken(tokens.RefreshToken)
	if _, err := ordinary.RotateSession(ctx, presented, identity.HashOpaqueToken("wrong realm"), now, now.Add(time.Hour), "realm-test"); !errors.Is(err, identity.ErrInvalidToken) {
		t.Fatal("consumer refresh accepted by tenant realm")
	}
	if err := ordinary.ValidateSession(ctx, identity.Session{ID: actor.SessionID, TenantID: tenant, UserID: actor.UserID, SecurityVersion: 2}, now); !errors.Is(err, identity.ErrInvalidToken) {
		t.Fatal("consumer session accepted in tenant realm")
	}
	refreshed, err := service.Refresh(ctx, tokens.RefreshToken, "consumer-refresh")
	if err != nil {
		t.Fatal("wrong realm altered valid consumer token", err)
	}
	request.Password = "attacker replacement password"
	if err := service.Request(ctx, "register", request); err != nil {
		t.Fatal(err)
	}
	request.Password = "correct password for test"
	if _, err := service.Login(ctx, request); err != nil {
		t.Fatal("duplicate registration replaced password")
	}
	if err := service.Request(ctx, "recover", request); err != nil {
		t.Fatal(err)
	}
	if _, err := service.DeliverOne(ctx); err != nil {
		t.Fatal(err)
	}
	resetCode := mail.message.Code
	if err := service.Complete(ctx, "password_reset", resetCode, "replacement password for test", request); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Authenticate(ctx, refreshed.AccessToken); !errors.Is(err, identity.ErrInvalidToken) {
		t.Fatal("reset left old sessions active")
	}
	request.Password = "replacement password for test"
	if _, err := service.Login(ctx, request); err != nil {
		t.Fatal(err)
	}
	var roles, queue, events int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM wheretolive.user_roles WHERE tenant_id=$1", tenant).Scan(&roles); err != nil || roles != 0 {
		t.Fatal("consumer received roles")
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM wheretolive.identity_mail_queue WHERE tenant_id=$1", tenant).Scan(&queue); err != nil || queue != 0 {
		t.Fatal("mail plaintext lifecycle incomplete")
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM wheretolive.public_identity_events WHERE tenant_id=$1", tenant).Scan(&events); err != nil || events < 5 {
		t.Fatal("missing audit")
	}
	if _, err := pool.Exec(ctx, "UPDATE wheretolive.tenants SET status='suspended' WHERE id=$1", tenant); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Login(ctx, request); !errors.Is(err, identity.ErrPublicUnavailable) {
		t.Fatal("suspended community logged in")
	}
}
func TestPublicIdentityRejectsForeignTenantCodesAndFailedAudit(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	now := time.Now().UTC()
	store := NewConsumer(pool)
	tx := database.NewTransactor(pool)
	mail := &integrationMailbox{}
	service := consumerService(t, store, tx, mail, now)
	if err := service.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	request := identity.PublicRequest{Email: "person@example.org", Password: "correct password for test", Challenge: "proof", Network: "192.0.2.1", CorrelationID: "public-test"}
	if err := service.Request(ctx, "register", request); err != nil {
		t.Fatal(err)
	}
	if _, err := service.DeliverOne(ctx); err != nil {
		t.Fatal(err)
	}
	foreign := identity.TenantID("018bcfe5-6800-7000-8000-000000000777")
	if err := tx.WithinTransaction(ctx, func(transaction pgx.Tx) error {
		_, err := store.CompletePublicToken(ctx, transaction, foreign, identity.HashOpaqueToken(mail.message.Code), "email_verification", "", now)
		return err
	}); !errors.Is(err, identity.ErrInvalidToken) {
		t.Fatal("foreign tenant consumed code")
	}
	// Deliberately make transactional audit fail; enrollment must roll back with its mail.
	if _, err := pool.Exec(ctx, "ALTER TABLE wheretolive.public_identity_events ADD CONSTRAINT reject_registration CHECK(action <> 'consumer_registered') NOT VALID"); err != nil {
		t.Fatal(err)
	}
	request.Email = "rollback@example.org"
	if err := service.Request(ctx, "register", request); !errors.Is(err, identity.ErrPublicAcceptedFailure) {
		t.Fatal("audit failure response exposed")
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM wheretolive.users WHERE normalized_email=$1", request.Email).Scan(&count); err != nil || count != 0 {
		t.Fatal("failed audit persisted consumer")
	}
}
