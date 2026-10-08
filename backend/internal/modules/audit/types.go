// Package audit owns durable business audit events.
package audit

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/modura-dev/modura/backend/internal/modules/identity"
)

// ErrNotFound hides whether an event exists outside the queried scope.
var ErrNotFound = errors.New("audit event not found")

// Event is immutable evidence for a completed tenant business write.
type Event struct {
	ID            string
	ActorID       identity.UserID
	TenantID      identity.TenantID
	Action        string
	Resource      string
	ResourceID    string
	Reason        string
	CorrelationID string
	OccurredAt    time.Time
	BeforeState   json.RawMessage
	AfterState    json.RawMessage
}

// PlatformEvent is immutable evidence for a completed global platform write.
// It is deliberately separate from Event so a missing tenant cannot be
// mistaken for an incompletely scoped tenant operation.
type PlatformEvent struct {
	ID            string
	ActorID       string
	Action        string
	Resource      string
	ResourceID    string
	Reason        string
	CorrelationID string
	OccurredAt    time.Time
	BeforeState   json.RawMessage
	AfterState    json.RawMessage
}

// TenantScopedPlatformEvent is immutable evidence for a platform
// administrator's write to exactly one existing tenant.
type TenantScopedPlatformEvent struct {
	ID            string
	ActorID       string
	TenantID      identity.TenantID
	Action        string
	Resource      string
	ResourceID    string
	Reason        string
	CorrelationID string
	OccurredAt    time.Time
	BeforeState   json.RawMessage
	AfterState    json.RawMessage
}

// Query is a bounded tenant audit search.
type Query struct {
	TenantID identity.TenantID
	Action   string
	Resource string
	Limit    int
	Offset   int
}

// PlatformQuery is a bounded global audit search for platform administrators.
// An empty TenantID lists events across all tenants.
type PlatformQuery struct {
	TenantID identity.TenantID
	Action   string
	Resource string
	Limit    int
	Offset   int
}

// Record is the redacted audit projection returned to authorized readers.
type Record struct {
	ID            string
	ActorType     string
	ActorID       string
	TenantID      identity.TenantID
	Action        string
	Resource      string
	ResourceID    string
	Reason        string
	Result        string
	CorrelationID string
	OccurredAt    time.Time
	BeforeState   json.RawMessage
	AfterState    json.RawMessage
}
