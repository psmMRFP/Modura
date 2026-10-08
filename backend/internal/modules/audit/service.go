package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/modura-dev/modura/backend/internal/modules/identity"
)

// Store persists audit-owned records inside caller-owned transactions.
type Store interface {
	Record(context.Context, pgx.Tx, Event) error
	RecordPlatform(context.Context, pgx.Tx, PlatformEvent) error
	RecordTenantScopedPlatform(context.Context, pgx.Tx, TenantScopedPlatformEvent) error
}

// RecordPlatformWrite records successful global-administrator activity in the supplied transaction.
func (s *Service) RecordPlatformWrite(ctx context.Context, tx pgx.Tx, event PlatformEvent) error {
	event.ActorID = strings.TrimSpace(event.ActorID)
	event.Action = strings.TrimSpace(event.Action)
	event.Resource = strings.TrimSpace(event.Resource)
	event.ResourceID = strings.TrimSpace(event.ResourceID)
	event.Reason = strings.TrimSpace(event.Reason)
	event.CorrelationID = strings.TrimSpace(event.CorrelationID)
	if tx == nil || event.ActorID == "" || event.Action == "" || event.Resource == "" || event.ResourceID == "" || event.Reason == "" || event.CorrelationID == "" || event.OccurredAt.IsZero() {
		return fmt.Errorf("invalid platform audit event")
	}
	if (len(event.BeforeState) > 0 && !json.Valid(event.BeforeState)) || (len(event.AfterState) > 0 && !json.Valid(event.AfterState)) {
		return fmt.Errorf("invalid platform audit state")
	}
	id, err := s.newID(event.OccurredAt)
	if err != nil {
		return fmt.Errorf("generate audit event ID: %w", err)
	}
	event.ID = id
	if err := s.store.RecordPlatform(ctx, tx, event); err != nil {
		return fmt.Errorf("record platform audit event: %w", err)
	}
	return nil
}

// Service creates validated durable audit evidence.
type Service struct {
	store   Store
	newID   func(time.Time) (string, error)
	queries QueryStore
}

// QueryStore loads immutable audit projections.
type QueryStore interface {
	List(context.Context, Query) ([]Record, error)
	Get(context.Context, identity.TenantID, string) (Record, error)
	ListPlatform(context.Context, PlatformQuery) ([]Record, error)
}

// NewService constructs the audit application service.
func NewService(store Store, newID func(time.Time) (string, error)) (*Service, error) {
	if store == nil || newID == nil {
		return nil, fmt.Errorf("invalid audit service configuration")
	}
	return &Service{store: store, newID: newID}, nil
}

// EnableQueries configures the read side without changing transactional writes.
func (s *Service) EnableQueries(store QueryStore) error {
	if store == nil {
		return fmt.Errorf("invalid audit query configuration")
	}
	s.queries = store
	return nil
}

// List returns a bounded, redacted audit page for the verified tenant actor.
func (s *Service) List(ctx context.Context, tenantID identity.TenantID, action, resource string, limit, offset int) ([]Record, error) {
	action = strings.TrimSpace(action)
	resource = strings.TrimSpace(resource)
	if tenantID == "" || s.queries == nil || limit < 1 || limit > 100 || offset < 0 || len(action) > 128 || len(resource) > 128 {
		return nil, fmt.Errorf("invalid audit query")
	}
	records, err := s.queries.List(ctx, Query{TenantID: tenantID, Action: action, Resource: resource, Limit: limit, Offset: offset})
	if err != nil {
		return nil, fmt.Errorf("list audit events: %w", err)
	}
	for i := range records {
		records[i].BeforeState = redactState(records[i].BeforeState)
		records[i].AfterState = redactState(records[i].AfterState)
	}
	return records, nil
}

// Get returns one redacted audit event only within the verified tenant.
func (s *Service) Get(ctx context.Context, tenantID identity.TenantID, eventID string) (Record, error) {
	eventID = strings.TrimSpace(eventID)
	if tenantID == "" || s.queries == nil || eventID == "" || len(eventID) > 128 {
		return Record{}, fmt.Errorf("invalid audit lookup")
	}
	record, err := s.queries.Get(ctx, tenantID, eventID)
	if err != nil {
		return Record{}, err
	}
	record.BeforeState = redactState(record.BeforeState)
	record.AfterState = redactState(record.AfterState)
	return record, nil
}

// ListPlatform returns a bounded redacted audit page across tenants for
// platform administrators. An empty tenant filter lists platform events too.
func (s *Service) ListPlatform(ctx context.Context, query PlatformQuery) ([]Record, error) {
	query.Action = strings.TrimSpace(query.Action)
	query.Resource = strings.TrimSpace(query.Resource)
	if s.queries == nil || query.Limit < 1 || query.Limit > 100 || query.Offset < 0 || len(query.Action) > 128 || len(query.Resource) > 128 {
		return nil, fmt.Errorf("invalid platform audit query")
	}
	records, err := s.queries.ListPlatform(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list platform audit events: %w", err)
	}
	for i := range records {
		records[i].BeforeState = redactState(records[i].BeforeState)
		records[i].AfterState = redactState(records[i].AfterState)
	}
	return records, nil
}

func redactState(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return json.RawMessage(`"[redacted-invalid-state]"`)
	}
	redactValue(value)
	redacted, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`"[redacted]"`)
	}
	return redacted
}

func redactValue(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if sensitiveKey(key) {
				typed[key] = "[redacted]"
				continue
			}
			redactValue(child)
		}
	case []any:
		for _, child := range typed {
			redactValue(child)
		}
	}
}

func sensitiveKey(key string) bool {
	normalized := strings.ToLower(key)
	for _, fragment := range []string{"password", "secret", "token", "credential", "authorization", "cookie", "session"} {
		if strings.Contains(normalized, fragment) {
			return true
		}
	}
	return false
}

// RecordTenantScopedPlatformWrite records platform-administrator activity on
// one existing tenant in the supplied transaction.
func (s *Service) RecordTenantScopedPlatformWrite(ctx context.Context, tx pgx.Tx, event TenantScopedPlatformEvent) error {
	event.ActorID = strings.TrimSpace(event.ActorID)
	event.Action = strings.TrimSpace(event.Action)
	event.Resource = strings.TrimSpace(event.Resource)
	event.ResourceID = strings.TrimSpace(event.ResourceID)
	event.Reason = strings.TrimSpace(event.Reason)
	event.CorrelationID = strings.TrimSpace(event.CorrelationID)
	if tx == nil || event.ActorID == "" || event.TenantID == "" || event.Action == "" || event.Resource == "" || event.ResourceID == "" || event.Reason == "" || event.CorrelationID == "" || event.OccurredAt.IsZero() {
		return fmt.Errorf("invalid tenant-scoped platform audit event")
	}
	if (len(event.BeforeState) > 0 && !json.Valid(event.BeforeState)) || (len(event.AfterState) > 0 && !json.Valid(event.AfterState)) {
		return fmt.Errorf("invalid tenant-scoped platform audit state")
	}
	id, err := s.newID(event.OccurredAt)
	if err != nil {
		return fmt.Errorf("generate audit event ID: %w", err)
	}
	event.ID = id
	if err := s.store.RecordTenantScopedPlatform(ctx, tx, event); err != nil {
		return fmt.Errorf("record tenant-scoped platform audit event: %w", err)
	}
	return nil
}

// RecordTenantWrite records successful tenant-user activity in the supplied transaction.
func (s *Service) RecordTenantWrite(ctx context.Context, tx pgx.Tx, event Event) error {
	event.Action = strings.TrimSpace(event.Action)
	event.Resource = strings.TrimSpace(event.Resource)
	event.ResourceID = strings.TrimSpace(event.ResourceID)
	event.Reason = strings.TrimSpace(event.Reason)
	event.CorrelationID = strings.TrimSpace(event.CorrelationID)
	if tx == nil || event.ActorID == "" || event.TenantID == "" || event.Action == "" || event.Resource == "" || event.ResourceID == "" || event.Reason == "" || event.CorrelationID == "" || event.OccurredAt.IsZero() {
		return fmt.Errorf("invalid tenant audit event")
	}
	if (len(event.BeforeState) > 0 && !json.Valid(event.BeforeState)) || (len(event.AfterState) > 0 && !json.Valid(event.AfterState)) {
		return fmt.Errorf("invalid tenant audit state")
	}
	id, err := s.newID(event.OccurredAt)
	if err != nil {
		return fmt.Errorf("generate audit event ID: %w", err)
	}
	event.ID = id
	if err := s.store.Record(ctx, tx, event); err != nil {
		return fmt.Errorf("record tenant audit event: %w", err)
	}
	return nil
}
