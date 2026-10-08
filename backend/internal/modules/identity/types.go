// Package identity owns tenants, local users, credentials, and login sessions.
package identity

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"
)

var (
	// ErrInvalidCredentials deliberately hides which login input was invalid.
	ErrInvalidCredentials = errors.New("invalid tenant or credentials")
	// ErrInactiveTenant means the resolved tenant cannot establish sessions.
	ErrInactiveTenant = errors.New("tenant is not active")
	// ErrInactiveUser means the local account cannot establish sessions.
	ErrInactiveUser = errors.New("user is not active")
	// ErrInvalidToken means a token is unknown, malformed, or revoked.
	ErrInvalidToken = errors.New("invalid token")
	// ErrExpiredToken means a valid token is outside its lifetime.
	ErrExpiredToken = errors.New("expired token")
	// ErrRefreshReuse means a previously consumed refresh token was presented.
	ErrRefreshReuse = errors.New("refresh token reuse detected")
	// ErrInvalidPassword means a proposed password violates the password policy.
	ErrInvalidPassword = errors.New("invalid password")
	// ErrUserNotFound hides whether an account belongs to a different tenant.
	ErrUserNotFound = errors.New("tenant user not found")
	// ErrTenantNotFound means a tenant lookup by explicit identifier missed.
	ErrTenantNotFound = errors.New("tenant not found")
	// ErrInvalidTenantTransition means the tenant is not in the required state.
	ErrInvalidTenantTransition = errors.New("invalid tenant lifecycle transition")
	// ErrAccountLocked means credential throttling locked this login pair.
	ErrAccountLocked = errors.New("account temporarily locked")
)

// SecurityEventType classifies privacy-conscious authentication evidence.
// Security events never carry credentials, login strings, network addresses,
// or user agents.
type SecurityEventType string

const (
	// SecurityEventLoginFailed marks the first failure of a throttling window.
	SecurityEventLoginFailed SecurityEventType = "login_failed"
	// SecurityEventLoginLocked marks a throttling lockout being triggered.
	SecurityEventLoginLocked SecurityEventType = "login_locked"
	// SecurityEventRefreshReplayDetected marks a replayed refresh token.
	SecurityEventRefreshReplayDetected SecurityEventType = "refresh_replay_detected"
	// SecurityEventPasswordChanged marks a credential replacement.
	SecurityEventPasswordChanged SecurityEventType = "password_changed"
	// SecurityEventSessionsRevoked marks session revocations.
	SecurityEventSessionsRevoked SecurityEventType = "sessions_revoked"
)

// TenantID is a verified tenant identifier.
type TenantID string

// UserID identifies a user inside a tenant.
type UserID string

// SessionID identifies a server-side authentication session.
type SessionID string

// OneTimePurpose classifies a single-use identity token.
type OneTimePurpose string

const (
	// PurposeInvitation establishes the first credential for an invited user.
	PurposeInvitation OneTimePurpose = "invitation"
	// PurposePasswordReset replaces a credential after account recovery.
	PurposePasswordReset OneTimePurpose = "password_reset"
)

// Actor is the verified tenant, user, and session scope of a request.
type Actor struct {
	TenantID  TenantID
	UserID    UserID
	SessionID SessionID
}

// AccountAuditEvent is the audit evidence for a tenant account lifecycle write.
// It deliberately mirrors audit-owned events without importing the audit module.
type AccountAuditEvent struct {
	Actor         Actor
	TargetUserID  UserID
	Action        string
	Resource      string
	ResourceID    string
	Reason        string
	CorrelationID string
	OccurredAt    time.Time
	BeforeState   json.RawMessage
	AfterState    json.RawMessage
}

// NormalizeLogin canonicalizes a tenant slug, username, or email for lookup.
func NormalizeLogin(value string) string {
	return strings.Map(unicode.ToLower, strings.TrimSpace(value))
}
