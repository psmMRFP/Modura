package settings

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/audit"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/identity"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/platformadmin"
)

var stableCode = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9._-]{0,62}[a-z0-9])?$`)

// Store persists settings inside explicit tenant and transaction boundaries.
type Store interface {
	ListEffectiveDictionaries(context.Context, identity.TenantID) ([]Dictionary, error)
	ReplaceTenantDictionary(context.Context, pgx.Tx, DictionaryWrite) (Dictionary, Dictionary, error)
	DeleteTenantDictionary(context.Context, pgx.Tx, identity.TenantID, string, int64) (Dictionary, error)
	ListEffectiveConfigurations(context.Context, identity.TenantID) ([]Configuration, error)
	PutTenantConfiguration(context.Context, pgx.Tx, ConfigurationWrite) (Configuration, Configuration, error)
	ListGlobalDictionaries(context.Context) ([]Dictionary, error)
	ReplaceGlobalDictionary(context.Context, pgx.Tx, GlobalDictionaryWrite) (Dictionary, Dictionary, error)
	ListGlobalConfigurations(context.Context) ([]Configuration, error)
	PutGlobalConfiguration(context.Context, pgx.Tx, GlobalConfigurationWrite) (Configuration, Configuration, error)
}

// Transactor runs one application-owned PostgreSQL transaction.
type Transactor interface {
	WithinTransaction(context.Context, func(pgx.Tx) error) error
}

// Auditor records settings writes inside their business transaction.
type Auditor interface {
	RecordTenantWrite(context.Context, pgx.Tx, audit.Event) error
	RecordPlatformWrite(context.Context, pgx.Tx, audit.PlatformEvent) error
}

// ListGlobalDictionaries returns the platform-owned dictionary catalogue.
func (s *Service) ListGlobalDictionaries(ctx context.Context, actor platformadmin.Actor) ([]Dictionary, error) {
	if !validPlatformActor(actor) {
		return nil, fmt.Errorf("invalid platform settings actor")
	}
	return s.store.ListGlobalDictionaries(ctx)
}

// ReplaceGlobalDictionary creates or replaces a complete global dictionary.
func (s *Service) ReplaceGlobalDictionary(ctx context.Context, write PlatformWriteContext, code, name string, expectedVersion int64, items []DictionaryItem) (Dictionary, error) {
	code = strings.ToLower(strings.TrimSpace(code))
	name = strings.TrimSpace(name)
	if !validPlatformWrite(write) || !stableCode.MatchString(code) || name == "" || len(name) > 128 || expectedVersion < 0 || len(items) > 500 {
		return Dictionary{}, fmt.Errorf("invalid global dictionary")
	}
	now := s.now().UTC()
	id, err := s.newID(now)
	if err != nil {
		return Dictionary{}, fmt.Errorf("generate dictionary ID: %w", err)
	}
	desired := GlobalDictionaryWrite{ID: id, Code: code, Name: name, ExpectedVersion: expectedVersion, OccurredAt: now, Items: make([]DictionaryItem, len(items))}
	seen := make(map[string]struct{}, len(items))
	for i, item := range items {
		item.Code, item.Label = strings.ToLower(strings.TrimSpace(item.Code)), strings.TrimSpace(item.Label)
		if !stableCode.MatchString(item.Code) || item.Label == "" || len(item.Label) > 128 {
			return Dictionary{}, fmt.Errorf("invalid dictionary item")
		}
		if _, exists := seen[item.Code]; exists {
			return Dictionary{}, fmt.Errorf("duplicate dictionary item")
		}
		seen[item.Code] = struct{}{}
		item.ID, err = s.newID(now)
		if err != nil {
			return Dictionary{}, fmt.Errorf("generate dictionary item ID: %w", err)
		}
		desired.Items[i] = item
	}
	var before, after Dictionary
	err = s.transactions.WithinTransaction(ctx, func(tx pgx.Tx) error {
		var storeErr error
		before, after, storeErr = s.store.ReplaceGlobalDictionary(ctx, tx, desired)
		if storeErr != nil {
			return storeErr
		}
		return s.auditPlatform(ctx, tx, write, now, "settings.global_dictionary.replaced", "global_dictionary", after.ID, before, after)
	})
	if err != nil {
		return Dictionary{}, fmt.Errorf("replace global dictionary: %w", err)
	}
	return after, nil
}

// ListGlobalConfigurations returns definitions with their global defaults.
func (s *Service) ListGlobalConfigurations(ctx context.Context, actor platformadmin.Actor) ([]Configuration, error) {
	if !validPlatformActor(actor) {
		return nil, fmt.Errorf("invalid platform settings actor")
	}
	return s.store.ListGlobalConfigurations(ctx)
}

// PutGlobalConfiguration creates or updates a non-secret definition and default.
func (s *Service) PutGlobalConfiguration(ctx context.Context, write PlatformWriteContext, key, name, valueType string, tenantOverridable bool, expectedVersion int64, value json.RawMessage) (Configuration, error) {
	key, name, valueType = strings.ToLower(strings.TrimSpace(key)), strings.TrimSpace(name), strings.ToLower(strings.TrimSpace(valueType))
	if !validPlatformWrite(write) || !stableCode.MatchString(key) || name == "" || len(name) > 128 || !slices.Contains([]string{"string", "boolean", "integer", "json"}, valueType) || expectedVersion < 0 || !json.Valid(value) || len(value) > 64*1024 || !validConfigurationValue(valueType, value) {
		return Configuration{}, fmt.Errorf("invalid global configuration")
	}
	now := s.now().UTC()
	id, err := s.newID(now)
	if err != nil {
		return Configuration{}, fmt.Errorf("generate configuration ID: %w", err)
	}
	desired := GlobalConfigurationWrite{ID: id, Key: key, Name: name, ValueType: valueType, TenantOverridable: tenantOverridable, ExpectedVersion: expectedVersion, Value: slices.Clone(value), OccurredAt: now}
	var before, after Configuration
	err = s.transactions.WithinTransaction(ctx, func(tx pgx.Tx) error {
		var storeErr error
		before, after, storeErr = s.store.PutGlobalConfiguration(ctx, tx, desired)
		if storeErr != nil {
			return storeErr
		}
		return s.auditPlatform(ctx, tx, write, now, "settings.global_configuration.updated", "global_configuration", after.ID, before, after)
	})
	if err != nil {
		return Configuration{}, fmt.Errorf("put global configuration: %w", err)
	}
	return after, nil
}

func (s *Service) auditPlatform(ctx context.Context, tx pgx.Tx, write PlatformWriteContext, now time.Time, action, resource, resourceID string, before, after any) error {
	beforeJSON, err := json.Marshal(before)
	if err != nil {
		return err
	}
	afterJSON, err := json.Marshal(after)
	if err != nil {
		return err
	}
	return s.auditor.RecordPlatformWrite(ctx, tx, audit.PlatformEvent{ActorID: string(write.Actor.AdministratorID), Action: action, Resource: resource, ResourceID: resourceID, Reason: write.Reason, CorrelationID: write.CorrelationID, OccurredAt: now, BeforeState: beforeJSON, AfterState: afterJSON})
}

func validPlatformActor(actor platformadmin.Actor) bool {
	return actor.AdministratorID != "" && actor.SessionID != ""
}
func validPlatformWrite(write PlatformWriteContext) bool {
	return validPlatformActor(write.Actor) && strings.TrimSpace(write.Reason) != "" && len(strings.TrimSpace(write.Reason)) <= 500 && strings.TrimSpace(write.CorrelationID) != ""
}

func validConfigurationValue(valueType string, value json.RawMessage) bool {
	var decoded any
	decoder := json.NewDecoder(strings.NewReader(string(value)))
	decoder.UseNumber()
	if decoder.Decode(&decoded) != nil {
		return false
	}
	switch valueType {
	case "string":
		_, ok := decoded.(string)
		return ok
	case "boolean":
		_, ok := decoded.(bool)
		return ok
	case "integer":
		n, ok := decoded.(json.Number)
		if !ok {
			return false
		}
		_, err := n.Int64()
		return err == nil
	case "json":
		return true
	}
	return false
}

// Service implements tenant settings use cases.
type Service struct {
	store        Store
	transactions Transactor
	auditor      Auditor
	now          func() time.Time
	newID        func(time.Time) (string, error)
}

// NewService constructs the settings service.
func NewService(store Store, transactions Transactor, auditor Auditor, now func() time.Time, newID func(time.Time) (string, error)) (*Service, error) {
	if store == nil || transactions == nil || auditor == nil || now == nil || newID == nil {
		return nil, fmt.Errorf("invalid settings service configuration")
	}
	return &Service{store: store, transactions: transactions, auditor: auditor, now: now, newID: newID}, nil
}

// ListDictionaries returns effective tenant-over-global dictionary types.
func (s *Service) ListDictionaries(ctx context.Context, actor identity.Actor) ([]Dictionary, error) {
	if !validActor(actor) {
		return nil, fmt.Errorf("invalid settings actor")
	}
	return s.store.ListEffectiveDictionaries(ctx, actor.TenantID)
}

// ReplaceDictionary creates or replaces a complete tenant dictionary.
func (s *Service) ReplaceDictionary(ctx context.Context, write WriteContext, code, name string, expectedVersion int64, items []DictionaryItem) (Dictionary, error) {
	code = strings.ToLower(strings.TrimSpace(code))
	name = strings.TrimSpace(name)
	if !validWrite(write) || !stableCode.MatchString(code) || name == "" || len(name) > 128 || expectedVersion < 0 || len(items) > 500 {
		return Dictionary{}, fmt.Errorf("invalid dictionary")
	}
	now := s.now().UTC()
	typeID, err := s.newID(now)
	if err != nil {
		return Dictionary{}, fmt.Errorf("generate dictionary ID: %w", err)
	}
	desired := DictionaryWrite{ID: typeID, TenantID: write.Actor.TenantID, Code: code, Name: name, ExpectedVersion: expectedVersion, OccurredAt: now, Items: make([]DictionaryItem, len(items))}
	seen := make(map[string]struct{}, len(items))
	for i, item := range items {
		item.Code = strings.ToLower(strings.TrimSpace(item.Code))
		item.Label = strings.TrimSpace(item.Label)
		if !stableCode.MatchString(item.Code) || item.Label == "" || len(item.Label) > 128 {
			return Dictionary{}, fmt.Errorf("invalid dictionary item")
		}
		if _, exists := seen[item.Code]; exists {
			return Dictionary{}, fmt.Errorf("duplicate dictionary item")
		}
		seen[item.Code] = struct{}{}
		item.ID, err = s.newID(now)
		if err != nil {
			return Dictionary{}, fmt.Errorf("generate dictionary item ID: %w", err)
		}
		desired.Items[i] = item
	}
	var before, after Dictionary
	err = s.transactions.WithinTransaction(ctx, func(tx pgx.Tx) error {
		var err error
		before, after, err = s.store.ReplaceTenantDictionary(ctx, tx, desired)
		if err != nil {
			return err
		}
		return s.audit(ctx, tx, write, now, "settings.dictionary.replaced", "dictionary", after.ID, before, after)
	})
	if err != nil {
		return Dictionary{}, fmt.Errorf("replace dictionary: %w", err)
	}
	return after, nil
}

// DeleteDictionary removes only a tenant-owned override/type.
func (s *Service) DeleteDictionary(ctx context.Context, write WriteContext, code string, expectedVersion int64) error {
	code = strings.ToLower(strings.TrimSpace(code))
	if !validWrite(write) || !stableCode.MatchString(code) || expectedVersion < 1 {
		return fmt.Errorf("invalid dictionary delete")
	}
	now := s.now().UTC()
	return s.transactions.WithinTransaction(ctx, func(tx pgx.Tx) error {
		before, err := s.store.DeleteTenantDictionary(ctx, tx, write.Actor.TenantID, code, expectedVersion)
		if err != nil {
			return err
		}
		return s.audit(ctx, tx, write, now, "settings.dictionary.deleted", "dictionary", before.ID, before, nil)
	})
}

// ListConfigurations returns effective non-secret configuration values.
func (s *Service) ListConfigurations(ctx context.Context, actor identity.Actor) ([]Configuration, error) {
	if !validActor(actor) {
		return nil, fmt.Errorf("invalid settings actor")
	}
	return s.store.ListEffectiveConfigurations(ctx, actor.TenantID)
}

// PutConfiguration creates or replaces one eligible tenant override.
func (s *Service) PutConfiguration(ctx context.Context, write WriteContext, key string, expectedVersion int64, value json.RawMessage) (Configuration, error) {
	key = strings.ToLower(strings.TrimSpace(key))
	if !validWrite(write) || !stableCode.MatchString(key) || expectedVersion < 0 || !json.Valid(value) || len(value) > 64*1024 {
		return Configuration{}, fmt.Errorf("invalid configuration")
	}
	now := s.now().UTC()
	var before, after Configuration
	err := s.transactions.WithinTransaction(ctx, func(tx pgx.Tx) error {
		var err error
		before, after, err = s.store.PutTenantConfiguration(ctx, tx, ConfigurationWrite{TenantID: write.Actor.TenantID, Key: key, ExpectedVersion: expectedVersion, Value: slices.Clone(value), OccurredAt: now})
		if err != nil {
			return err
		}
		return s.audit(ctx, tx, write, now, "settings.configuration.updated", "configuration", after.ID, before, after)
	})
	if err != nil {
		return Configuration{}, fmt.Errorf("put configuration: %w", err)
	}
	return after, nil
}

func (s *Service) audit(ctx context.Context, tx pgx.Tx, write WriteContext, now time.Time, action, resource, resourceID string, before, after any) error {
	beforeJSON, err := json.Marshal(before)
	if err != nil {
		return err
	}
	afterJSON, err := json.Marshal(after)
	if err != nil {
		return err
	}
	return s.auditor.RecordTenantWrite(ctx, tx, audit.Event{ActorID: write.Actor.UserID, TenantID: write.Actor.TenantID, Action: action, Resource: resource, ResourceID: resourceID, Reason: "authorized tenant settings request", CorrelationID: write.CorrelationID, OccurredAt: now, BeforeState: beforeJSON, AfterState: afterJSON})
}

func validActor(actor identity.Actor) bool {
	return actor.TenantID != "" && actor.UserID != "" && actor.SessionID != ""
}

func validWrite(write WriteContext) bool {
	return validActor(write.Actor) && strings.TrimSpace(write.CorrelationID) != ""
}
