package identity

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

var (
	// ErrPublicUnavailable keeps unconfigured public flows closed.
	ErrPublicUnavailable = errors.New("public identity unavailable")
	// ErrPublicInvalid rejects malformed public identity input.
	ErrPublicInvalid = errors.New("invalid public identity input")
	// ErrPublicLimited rejects exhausted anonymous budgets.
	ErrPublicLimited = errors.New("public identity request limited")
	// ErrPublicChallenge rejects invalid or replayed proof of human interaction.
	ErrPublicChallenge = errors.New("public identity challenge failed")
	// ErrPublicAcceptedFailure masks account-dependent persistence failures.
	ErrPublicAcceptedFailure = errors.New("public identity request accepted with internal failure")
)

// PublicRequest is an untrusted enrollment or delivery request, without tenant selection.
type PublicRequest struct{ Email, Password, Challenge, Network, CorrelationID string }

// ConsumerEnrollment carries already validated credentials into a transaction.
type ConsumerEnrollment struct {
	TenantID            TenantID
	UserID              UserID
	Email, PasswordHash string
	Now                 time.Time
}

// PublicDelivery binds a single-use secret and encrypted mail to the same transaction.
type PublicDelivery struct {
	TenantID                        TenantID
	UserID                          UserID
	TokenID, MailID, Purpose, Email string
	SecurityVersion                 int64
	TokenHash                       [32]byte
	Payload                         []byte
	Now, ExpiresAt                  time.Time
}

// PublicAccount is minimal locked delivery eligibility, never returned publicly.
type PublicAccount struct {
	ID              UserID
	Email, Status   string
	SecurityVersion int64
}

// MailLease identifies one leased encrypted message.
type MailLease struct {
	ID       string
	TenantID TenantID
	Payload  []byte
	Attempts int32
}

// PublicStore owns consumer writes under explicit tenant and transaction scope.
type PublicStore interface {
	BootstrapCommunity(context.Context, pgx.Tx, TenantID, time.Time) (TenantID, bool, error)
	ResolveCommunity(context.Context) (TenantID, error)
	LockCommunity(context.Context, pgx.Tx, TenantID) error
	CreateConsumer(context.Context, pgx.Tx, ConsumerEnrollment) (bool, error)
	ConsumerByEmail(context.Context, pgx.Tx, TenantID, string) (PublicAccount, error)
	QueuePublicDelivery(context.Context, pgx.Tx, PublicDelivery) error
	CompletePublicToken(context.Context, pgx.Tx, TenantID, [32]byte, string, string, time.Time) (UserID, error)
	PublicEvent(context.Context, pgx.Tx, TenantID, UserID, string, string, time.Time) error
	LimitPublic(context.Context, pgx.Tx, TenantID, [][32]byte, []int32, time.Time, time.Duration) (bool, error)
	PurgePublicIdentity(context.Context, time.Time) error
	LeaseMail(context.Context, time.Time) (MailLease, error)
	FinishMail(context.Context, MailLease, bool, time.Time) error
}

// ChallengeVerifier is the server-side, replay-resistant CAPTCHA boundary.
type ChallengeVerifier interface {
	Verify(context.Context, string, string) error
}

// MailSender delivers one plaintext message without returning provider diagnostics.
type MailSender interface {
	Send(context.Context, PublicMail) error
}

// PublicMail is decrypted only in the bounded delivery worker.
type PublicMail struct{ Email, Code, Purpose string }

// PublicOptions are deployment-only privacy and abuse controls.
type PublicOptions struct {
	Enabled                                         bool
	EncryptionKey                                   []byte
	VerificationLifetime, ResetLifetime, RateWindow time.Duration
	GlobalLimit, NetworkLimit, AccountLimit         int32
}

// PublicService adds community-only enrollment and sessions without tenant administration.
type PublicService struct {
	store        PublicStore
	transactions Transactor
	core         *Service
	challenge    ChallengeVerifier
	sender       MailSender
	options      PublicOptions
	aead         cipher.AEAD
}

// NewPublicService requires every dependency before enabling anonymous writes.
func NewPublicService(store PublicStore, transactions Transactor, core *Service, challenge ChallengeVerifier, sender MailSender, options PublicOptions) (*PublicService, error) {
	if store == nil || transactions == nil || core == nil {
		return nil, fmt.Errorf("invalid public identity dependencies")
	}
	s := &PublicService{store: store, transactions: transactions, core: core, challenge: challenge, sender: sender, options: options}
	if !options.Enabled {
		return s, nil
	}
	if challenge == nil || sender == nil || len(options.EncryptionKey) != 32 || options.VerificationLifetime <= 0 || options.ResetLifetime <= 0 || options.RateWindow <= 0 || options.GlobalLimit <= 0 || options.NetworkLimit <= 0 || options.AccountLimit <= 0 {
		return nil, fmt.Errorf("invalid public identity deployment configuration")
	}
	block, err := aes.NewCipher(options.EncryptionKey)
	if err != nil {
		return nil, err
	}
	s.aead, err = cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// Enabled reports configuration availability, not production delivery certification.
func (s *PublicService) Enabled() bool { return s != nil && s.options.Enabled }

// Available reports both configuration and the current community lifecycle.
func (s *PublicService) Available(ctx context.Context) bool {
	if !s.Enabled() {
		return false
	}
	_, err := s.store.ResolveCommunity(ctx)
	return err == nil
}

// Bootstrap creates the reserved consumer tenant once; it grants no roles or invitations.
func (s *PublicService) Bootstrap(ctx context.Context) error {
	if !s.Enabled() {
		return ErrPublicUnavailable
	}
	now := s.core.now().UTC()
	id, err := s.core.newID(now)
	if err != nil {
		return err
	}
	return s.transactions.WithinTransaction(ctx, func(tx pgx.Tx) error {
		tenant, created, err := s.store.BootstrapCommunity(ctx, tx, TenantID(id), now)
		if err != nil {
			return err
		}
		if created {
			return s.store.PublicEvent(ctx, tx, tenant, "", "community_bootstrapped", "startup-community", now)
		}
		return nil
	})
}

func publicEmail(input string) (string, error) {
	email := NormalizeLogin(input)
	if len(email) > 254 || strings.ContainsAny(email, "\r\n") {
		return "", ErrPublicInvalid
	}
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email || !strings.Contains(email, "@") {
		return "", ErrPublicInvalid
	}
	return email, nil
}

func (s *PublicService) guard(ctx context.Context, request PublicRequest) (TenantID, error) {
	if !s.Enabled() {
		return "", ErrPublicUnavailable
	}
	if request.CorrelationID == "" || len(request.CorrelationID) > 128 || request.Network == "" || request.Challenge == "" || len(request.Challenge) > 2048 {
		return "", ErrPublicInvalid
	}
	tenant, err := s.store.ResolveCommunity(ctx)
	if err != nil {
		return "", ErrPublicUnavailable
	}
	hashes := make([][32]byte, 3)
	for i, key := range []string{"global", "network:" + request.Network, "account:" + request.Email} {
		mac := hmac.New(sha256.New, s.options.EncryptionKey)
		_, _ = mac.Write([]byte("public-limit:" + key))
		copy(hashes[i][:], mac.Sum(nil))
	}
	allowed := false
	err = s.transactions.WithinTransaction(ctx, func(tx pgx.Tx) error {
		var limitErr error
		allowed, limitErr = s.store.LimitPublic(ctx, tx, tenant, hashes, []int32{s.options.GlobalLimit, s.options.NetworkLimit, s.options.AccountLimit}, s.core.now().UTC(), s.options.RateWindow)
		return limitErr
	})
	if err != nil {
		return "", ErrPublicUnavailable
	}
	if !allowed {
		return "", ErrPublicLimited
	}
	if err := s.challenge.Verify(ctx, request.Challenge, request.Network); err != nil {
		return "", ErrPublicChallenge
	}
	return tenant, nil
}

// Request handles register, resend and recovery with the same non-enumerating outcome.
func (s *PublicService) Request(ctx context.Context, operation string, request PublicRequest) error {
	if !s.Enabled() {
		return ErrPublicUnavailable
	}
	email, err := publicEmail(request.Email)
	if err != nil {
		return err
	}
	request.Email = email
	if operation != "register" && operation != "resend" && operation != "recover" {
		return ErrPublicInvalid
	}
	if operation == "register" && (utf8.RuneCountInString(request.Password) < 12 || utf8.RuneCountInString(request.Password) > 1024 || !utf8.ValidString(request.Password)) {
		return ErrInvalidPassword
	}
	tenant, err := s.guard(ctx, request)
	if err != nil {
		return err
	}
	passwordHash := ""
	if operation == "register" {
		passwordHash, err = HashPassword(request.Password, s.core.password)
		if err != nil {
			return err
		}
	}
	now := s.core.now().UTC()
	ids := make([]string, 3)
	for i := range ids {
		ids[i], err = s.core.newID(now)
		if err != nil {
			return ErrPublicAcceptedFailure
		}
	}
	secret, err := s.core.newSecret()
	if err != nil {
		return ErrPublicAcceptedFailure
	}
	err = s.transactions.WithinTransaction(ctx, func(tx pgx.Tx) error {
		if err := s.store.LockCommunity(ctx, tx, tenant); err != nil {
			return err
		}
		if operation == "register" {
			created, err := s.store.CreateConsumer(ctx, tx, ConsumerEnrollment{TenantID: tenant, UserID: UserID(ids[0]), Email: email, PasswordHash: passwordHash, Now: now})
			if err != nil {
				return err
			}
			// A repeated registration never changes an existing credential or sends unsolicited mail.
			if !created {
				return nil
			}
			if err := s.store.PublicEvent(ctx, tx, tenant, UserID(ids[0]), "consumer_registered", request.CorrelationID, now); err != nil {
				return err
			}
		}
		account, err := s.store.ConsumerByEmail(ctx, tx, tenant, email)
		if errors.Is(err, ErrUserNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		purpose, lifetime := "email_verification", s.options.VerificationLifetime
		if operation == "recover" {
			purpose, lifetime = "password_reset", s.options.ResetLifetime
			if account.Status != "active" {
				return nil
			}
		} else if account.Status != "pending_email" {
			return nil
		}
		raw, err := json.Marshal(PublicMail{Email: email, Code: secret, Purpose: purpose})
		if err != nil {
			return err
		}
		nonce := make([]byte, s.aead.NonceSize())
		if _, err = rand.Read(nonce); err != nil {
			return err
		}
		aad := []byte(string(tenant) + ":" + ids[2])
		payload := s.aead.Seal(nonce, nonce, raw, aad)
		if err := s.store.QueuePublicDelivery(ctx, tx, PublicDelivery{TenantID: tenant, UserID: account.ID, TokenID: ids[1], MailID: ids[2], Purpose: purpose, Email: email, SecurityVersion: account.SecurityVersion, TokenHash: HashOpaqueToken(secret), Payload: payload, Now: now, ExpiresAt: now.Add(lifetime)}); err != nil {
			return err
		}
		return s.store.PublicEvent(ctx, tx, tenant, account.ID, "mail_queued_"+purpose, request.CorrelationID, now)
	})
	if err != nil {
		return ErrPublicAcceptedFailure
	}
	return nil
}

// Complete verifies an email or resets a password using a body-only single-use code.
func (s *PublicService) Complete(ctx context.Context, purpose, code, password string, request PublicRequest) error {
	if purpose != "email_verification" && purpose != "password_reset" {
		return ErrPublicInvalid
	}
	if len(code) < 32 || len(code) > 128 {
		return ErrInvalidToken
	}
	request.Email = fmt.Sprintf("completion:%x", HashOpaqueToken(code)) // Per-code budgets, without trusting a tenant supplied by the client.
	tenant, err := s.guard(ctx, request)
	if err != nil {
		return err
	}
	hash := ""
	if purpose == "password_reset" {
		if utf8.RuneCountInString(password) > 1024 {
			return ErrInvalidPassword
		}
		hash, err = HashPassword(password, s.core.password)
		if err != nil {
			return err
		}
	}
	now := s.core.now().UTC()
	return s.transactions.WithinTransaction(ctx, func(tx pgx.Tx) error {
		if err := s.store.LockCommunity(ctx, tx, tenant); err != nil {
			return err
		}
		user, err := s.store.CompletePublicToken(ctx, tx, tenant, HashOpaqueToken(code), purpose, hash, now)
		if err != nil {
			return err
		}
		return s.store.PublicEvent(ctx, tx, tenant, user, "completed_"+purpose, request.CorrelationID, now)
	})
}

// Login establishes only a consumer session in the server-resolved community.
func (s *PublicService) Login(ctx context.Context, request PublicRequest) (Tokens, error) {
	email, err := publicEmail(request.Email)
	if err != nil {
		return Tokens{}, ErrInvalidCredentials
	}
	request.Email = email
	if utf8.RuneCountInString(request.Password) > 1024 {
		return Tokens{}, ErrInvalidCredentials
	}
	if _, err = s.guard(ctx, request); err != nil {
		return Tokens{}, err
	}
	return s.core.Login(ctx, "community", email, request.Password, request.CorrelationID)
}

// Refresh rotates only consumer refresh tokens through the consumer store.
func (s *PublicService) Refresh(ctx context.Context, token, correlation string) (Tokens, error) {
	if !s.Enabled() {
		return Tokens{}, ErrPublicUnavailable
	}
	if _, err := s.store.ResolveCommunity(ctx); err != nil {
		return Tokens{}, ErrPublicUnavailable
	}
	return s.core.Refresh(ctx, token, correlation)
}

// Authenticate validates the distinct consumer audience and current community binding.
func (s *PublicService) Authenticate(ctx context.Context, token string) (Actor, error) {
	if !s.Enabled() {
		return Actor{}, ErrPublicUnavailable
	}
	tenant, err := s.store.ResolveCommunity(ctx)
	if err != nil {
		return Actor{}, ErrInvalidToken
	}
	actor, err := s.core.AuthenticateAccess(ctx, token)
	if err != nil || actor.TenantID != tenant {
		return Actor{}, ErrInvalidToken
	}
	return actor, nil
}

// Profile returns only self-service consumer fields.
func (s *PublicService) Profile(ctx context.Context, actor Actor) (Profile, error) {
	return s.core.Profile(ctx, actor)
}

// Logout revokes one verified consumer session.
func (s *PublicService) Logout(ctx context.Context, actor Actor, correlation string) error {
	return s.core.Logout(ctx, actor, correlation)
}

// DeliverOne leases outside SMTP, so no business transaction waits for the network.
func (s *PublicService) DeliverOne(ctx context.Context) (bool, error) {
	if !s.Enabled() {
		return false, ErrPublicUnavailable
	}
	lease, err := s.store.LeaseMail(ctx, s.core.now().UTC())
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("lease identity mail failed")
	}
	if len(lease.Payload) < s.aead.NonceSize() {
		if err := s.store.FinishMail(ctx, lease, false, s.core.now().UTC()); err != nil {
			return true, fmt.Errorf("finish identity mail failed")
		}
		return true, fmt.Errorf("invalid encrypted identity mail")
	}
	nonce := lease.Payload[:s.aead.NonceSize()]
	raw, err := s.aead.Open(nil, nonce, lease.Payload[s.aead.NonceSize():], []byte(string(lease.TenantID)+":"+lease.ID))
	var message PublicMail
	if err == nil {
		err = json.Unmarshal(raw, &message)
	}
	if err == nil {
		err = s.sender.Send(ctx, message)
	}
	if finishErr := s.store.FinishMail(ctx, lease, err == nil, s.core.now().UTC()); finishErr != nil {
		return true, fmt.Errorf("finish identity mail failed")
	}
	if err != nil {
		return true, fmt.Errorf("identity mail delivery failed")
	}
	return true, nil
}

// Maintain removes expired payloads and budgets independently of account availability.
func (s *PublicService) Maintain(ctx context.Context) error {
	return s.store.PurgePublicIdentity(ctx, s.core.now().UTC())
}
