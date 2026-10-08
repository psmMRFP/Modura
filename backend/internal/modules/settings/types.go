// Package settings owns dictionaries and non-secret application configuration.
package settings

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/psmMRFP/WhereToLive/backend/internal/modules/identity"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/platformadmin"
)

var (
	// ErrNotFound hides settings outside the verified owner scope.
	ErrNotFound = errors.New("settings resource not found")
	// ErrConflict means an expected version is stale.
	ErrConflict = errors.New("settings version conflict")
	// ErrNotOverridable means a tenant attempted to replace a global-only value.
	ErrNotOverridable = errors.New("configuration is not tenant overridable")
)

// DictionaryItem is one stable dictionary option.
type DictionaryItem struct {
	ID        string
	Code      string
	Label     string
	SortOrder int
	Enabled   bool
}

// Dictionary is an effective global or tenant-owned dictionary type.
type Dictionary struct {
	ID      string
	Code    string
	Name    string
	Source  string
	Version int64
	Items   []DictionaryItem
}

// Configuration is one effective non-secret setting.
type Configuration struct {
	ID                string
	Key               string
	Name              string
	ValueType         string
	TenantOverridable bool
	Source            string
	Version           int64
	Value             json.RawMessage
}

// WriteContext carries verified evidence for tenant settings changes.
type WriteContext struct {
	Actor         identity.Actor
	CorrelationID string
}

// DictionaryWrite is the desired state of a tenant dictionary.
type DictionaryWrite struct {
	ID              string
	TenantID        identity.TenantID
	Code            string
	Name            string
	ExpectedVersion int64
	Items           []DictionaryItem
	OccurredAt      time.Time
}

// ConfigurationWrite is a tenant override desired state.
type ConfigurationWrite struct {
	TenantID        identity.TenantID
	Key             string
	ExpectedVersion int64
	Value           json.RawMessage
	OccurredAt      time.Time
}

// PlatformWriteContext carries verified evidence for a global settings change.
type PlatformWriteContext struct {
	Actor         platformadmin.Actor
	Reason        string
	CorrelationID string
}

// GlobalDictionaryWrite is the desired complete global dictionary state.
type GlobalDictionaryWrite struct {
	ID              string
	Code            string
	Name            string
	ExpectedVersion int64
	Items           []DictionaryItem
	OccurredAt      time.Time
}

// GlobalConfigurationWrite is a definition and its required global value.
type GlobalConfigurationWrite struct {
	ID                string
	Key               string
	Name              string
	ValueType         string
	TenantOverridable bool
	ExpectedVersion   int64
	Value             json.RawMessage
	OccurredAt        time.Time
}
