package identity

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

type publicTestStore struct {
	PublicStore
	enrollment        ConsumerEnrollment
	delivery          PublicDelivery
	created           bool
	account           PublicAccount
	allow             bool
	keys              [][32]byte
	eventErr          error
	queued            int
	lease             MailLease
	sent              bool
	completionPurpose string
	purgeCalls        int
}

func (f *publicTestStore) PurgePublicIdentity(context.Context, time.Time) error {
	f.purgeCalls++
	return nil
}
func (f *publicTestStore) ResolveCommunity(context.Context) (TenantID, error) {
	return "018bcfe5-6800-7000-8000-000000000101", nil
}
func (f *publicTestStore) LockCommunity(context.Context, pgx.Tx, TenantID) error { return nil }
func (f *publicTestStore) CreateConsumer(_ context.Context, _ pgx.Tx, e ConsumerEnrollment) (bool, error) {
	f.enrollment = e
	f.account = PublicAccount{ID: e.UserID, Email: e.Email, Status: "pending_email", SecurityVersion: 1}
	return f.created, nil
}
func (f *publicTestStore) ConsumerByEmail(context.Context, pgx.Tx, TenantID, string) (PublicAccount, error) {
	return f.account, nil
}
func (f *publicTestStore) QueuePublicDelivery(_ context.Context, _ pgx.Tx, d PublicDelivery) error {
	f.delivery = d
	f.queued++
	return nil
}
func (f *publicTestStore) PublicEvent(context.Context, pgx.Tx, TenantID, UserID, string, string, time.Time) error {
	return f.eventErr
}
func (f *publicTestStore) LimitPublic(_ context.Context, _ pgx.Tx, _ TenantID, keys [][32]byte, _ []int32, _ time.Time, _ time.Duration) (bool, error) {
	f.keys = keys
	return f.allow, nil
}
func (f *publicTestStore) BootstrapCommunity(_ context.Context, _ pgx.Tx, id TenantID, _ time.Time) (TenantID, bool, error) {
	return id, true, nil
}
func (f *publicTestStore) CompletePublicToken(_ context.Context, _ pgx.Tx, _ TenantID, _ [32]byte, purpose, _ string, _ time.Time) (UserID, error) {
	f.completionPurpose = purpose
	return f.account.ID, nil
}
func (f *publicTestStore) LeaseMail(context.Context, time.Time) (MailLease, error) {
	return f.lease, nil
}
func (f *publicTestStore) FinishMail(_ context.Context, _ MailLease, sent bool, _ time.Time) error {
	f.sent = sent
	return nil
}

type publicTestTx struct{ commits int }

func (t *publicTestTx) WithinTransaction(_ context.Context, work func(pgx.Tx) error) error {
	err := work(nil)
	if err == nil {
		t.commits++
	}
	return err
}

type publicTestChallenge struct {
	calls int
	err   error
}

func (c *publicTestChallenge) Verify(context.Context, string, string) error { c.calls++; return c.err }

type publicTestMail struct {
	message PublicMail
	err     error
}

func (m *publicTestMail) Send(_ context.Context, message PublicMail) error {
	m.message = message
	return m.err
}

type publicCoreStore struct{ Store }

func publicFixture(t *testing.T) (*PublicService, *publicTestStore, *publicTestTx, *publicTestChallenge, *publicTestMail) {
	t.Helper()
	now := time.Unix(1700000000, 0).UTC()
	counter := 0
	newID := func(time.Time) (string, error) {
		counter++
		return []string{"018bcfe5-6800-7000-8000-000000000201", "018bcfe5-6800-7000-8000-000000000202", "018bcfe5-6800-7000-8000-000000000203"}[(counter-1)%3], nil
	}
	core, err := NewService(publicCoreStore{}, AccessTokenSigner{}, AccessTokenVerifier{}, PasswordParameters{Memory: 32, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}, time.Hour, func() time.Time { return now }, newID, func() (string, error) { return "a-body-only-secret-with-at-least-32-bytes", nil })
	if err != nil {
		t.Fatal(err)
	}
	store := &publicTestStore{created: true, allow: true}
	tx := &publicTestTx{}
	captcha := &publicTestChallenge{}
	mail := &publicTestMail{}
	s, err := NewPublicService(store, tx, core, captcha, mail, PublicOptions{Enabled: true, EncryptionKey: bytes.Repeat([]byte{7}, 32), VerificationLifetime: time.Hour, ResetLifetime: 15 * time.Minute, RateWindow: time.Hour, GlobalLimit: 100, NetworkLimit: 20, AccountLimit: 10})
	if err != nil {
		t.Fatal(err)
	}
	return s, store, tx, captcha, mail
}
func TestPublicRegistrationEncryptsDeliveryAndHashesPrivateBudgets(t *testing.T) {
	s, store, _, _, mail := publicFixture(t)
	request := PublicRequest{Email: " Person@Example.org ", Password: "a sufficiently long password", Challenge: "proof", Network: "192.0.2.1", CorrelationID: "public-test"}
	if err := s.Request(context.Background(), "register", request); err != nil {
		t.Fatal(err)
	}
	if store.enrollment.Email != "person@example.org" || store.enrollment.PasswordHash == request.Password || store.delivery.Purpose != "email_verification" || store.delivery.SecurityVersion != 1 {
		t.Fatal("unvalidated enrollment or delivery")
	}
	if bytes.Contains(store.delivery.Payload, []byte(request.Password)) || bytes.Contains(store.delivery.Payload, []byte("person@example.org")) || bytes.Contains(store.delivery.Payload, []byte("a-body-only-secret")) {
		t.Fatal("plaintext persisted")
	}
	if len(store.keys) != 3 || store.keys[0] == store.keys[1] || store.keys[1] == store.keys[2] {
		t.Fatal("budgets not separated")
	}
	store.lease = MailLease{ID: store.delivery.MailID, TenantID: store.delivery.TenantID, Payload: store.delivery.Payload, Attempts: 1}
	if found, err := s.DeliverOne(context.Background()); err != nil || !found || !store.sent || mail.message.Email != "person@example.org" {
		t.Fatal("delivery failed", err)
	}
	store.lease.ID = "wrong-message-id"
	if _, err := s.DeliverOne(context.Background()); err == nil || store.sent {
		t.Fatal("ciphertext not bound to lease")
	}
}
func TestPublicRejectsAbuseAndDuplicateCredentialChanges(t *testing.T) {
	s, store, _, captcha, _ := publicFixture(t)
	request := PublicRequest{Email: "person@example.org", Password: "sufficient password", Challenge: "proof", Network: "192.0.2.1", CorrelationID: "public-test"}
	store.allow = false
	if err := s.Request(context.Background(), "register", request); !errors.Is(err, ErrPublicLimited) || captcha.calls != 0 {
		t.Fatal("exhausted budget passed CAPTCHA")
	}
	store.allow = true
	captcha.err = errors.New("replayed")
	if err := s.Request(context.Background(), "register", request); !errors.Is(err, ErrPublicChallenge) || store.queued != 0 {
		t.Fatal("replayed proof accepted")
	}
	captcha.err = nil
	store.created = false
	if err := s.Request(context.Background(), "register", request); err != nil || store.queued != 0 {
		t.Fatal("duplicate registration mailed or changed credentials")
	}
	store.created = true
	store.eventErr = errors.New("audit unavailable")
	if err := s.Request(context.Background(), "register", request); !errors.Is(err, ErrPublicAcceptedFailure) {
		t.Fatal("account-dependent failure leaked")
	}
}
func TestPublicCompletionPurposeAndDisabledConfiguration(t *testing.T) {
	s, store, tx, _, _ := publicFixture(t)
	request := PublicRequest{Challenge: "proof", Network: "192.0.2.1", CorrelationID: "public-test"}
	if err := s.Complete(context.Background(), "email_verification", "a-body-only-secret-with-at-least-32-bytes", "", request); err != nil || store.completionPurpose != "email_verification" {
		t.Fatal(err)
	}
	store.eventErr = errors.New("audit unavailable")
	before := tx.commits
	if err := s.Bootstrap(context.Background()); err == nil || tx.commits != before {
		t.Fatal("unaudited bootstrap committed")
	}
	s.options.Enabled = false
	if err := s.Maintain(context.Background()); err != nil || store.purgeCalls != 1 {
		t.Fatal("disabling enrollment stopped retention cleanup")
	}
	if err := s.Request(context.Background(), "register", PublicRequest{}); !errors.Is(err, ErrPublicUnavailable) {
		t.Fatal("disabled enrollment accepted")
	}
	if err := s.Complete(context.Background(), "invitation", "valid-code-does-not-grant-invitation", "", request); !errors.Is(err, ErrPublicInvalid) {
		t.Fatal("purpose expansion allowed")
	}
}
func TestPublicCiphertextUsesIndependentAuthenticatedEncryption(t *testing.T) {
	s, store, _, _, _ := publicFixture(t)
	request := PublicRequest{Email: "person@example.org", Password: "sufficient password", Challenge: "proof", Network: "192.0.2.1", CorrelationID: "public-test"}
	if err := s.Request(context.Background(), "register", request); err != nil {
		t.Fatal(err)
	}
	block, _ := aes.NewCipher(bytes.Repeat([]byte{7}, 32))
	gcm, _ := cipher.NewGCM(block)
	payload := store.delivery.Payload
	raw, err := gcm.Open(nil, payload[:gcm.NonceSize()], payload[gcm.NonceSize():], []byte(string(store.delivery.TenantID)+":"+store.delivery.MailID))
	if err != nil {
		t.Fatal(err)
	}
	var message PublicMail
	if json.Unmarshal(raw, &message) != nil || HashOpaqueToken(message.Code) != store.delivery.TokenHash {
		t.Fatal("mail/code mismatch")
	}
}
