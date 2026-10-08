// Package postgres persists settings-owned data in PostgreSQL.
package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/identity"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/settings"
)

// Store is the PostgreSQL settings repository.
type Store struct{ pool *pgxpool.Pool }

// New constructs a settings store.
func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// ListGlobalDictionaries returns platform-owned dictionary types and items.
func (s *Store) ListGlobalDictionaries(ctx context.Context) ([]settings.Dictionary, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, code, name, version FROM wheretolive.global_dictionary_types ORDER BY code`)
	if err != nil {
		return nil, fmt.Errorf("query global dictionaries: %w", err)
	}
	defer rows.Close()
	var dictionaries []settings.Dictionary
	for rows.Next() {
		var dictionary settings.Dictionary
		if err := rows.Scan(&dictionary.ID, &dictionary.Code, &dictionary.Name, &dictionary.Version); err != nil {
			return nil, fmt.Errorf("scan global dictionary: %w", err)
		}
		dictionary.Source = "global"
		dictionaries = append(dictionaries, dictionary)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range dictionaries {
		items, err := s.listGlobalDictionaryItems(ctx, s.pool, dictionaries[i].ID)
		if err != nil {
			return nil, err
		}
		dictionaries[i].Items = items
	}
	return dictionaries, nil
}

// ReplaceGlobalDictionary writes one complete versioned global dictionary.
func (s *Store) ReplaceGlobalDictionary(ctx context.Context, tx pgx.Tx, desired settings.GlobalDictionaryWrite) (settings.Dictionary, settings.Dictionary, error) {
	before, err := loadGlobalDictionary(ctx, tx, desired.Code, true)
	if err != nil && !errors.Is(err, settings.ErrNotFound) {
		return settings.Dictionary{}, settings.Dictionary{}, err
	}
	if desired.ExpectedVersion == 0 {
		if err == nil {
			return settings.Dictionary{}, settings.Dictionary{}, settings.ErrConflict
		}
		if _, err = tx.Exec(ctx, `INSERT INTO wheretolive.global_dictionary_types (id, code, name, version, created_at, updated_at) VALUES ($1, $2, $3, 1, $4, $4)`, desired.ID, desired.Code, desired.Name, desired.OccurredAt); err != nil {
			return settings.Dictionary{}, settings.Dictionary{}, fmt.Errorf("insert global dictionary: %w", err)
		}
		before = settings.Dictionary{}
	} else {
		if errors.Is(err, settings.ErrNotFound) {
			return settings.Dictionary{}, settings.Dictionary{}, settings.ErrNotFound
		}
		if before.Version != desired.ExpectedVersion {
			return settings.Dictionary{}, settings.Dictionary{}, settings.ErrConflict
		}
		desired.ID = before.ID
		if _, err = tx.Exec(ctx, `DELETE FROM wheretolive.global_dictionary_items WHERE dictionary_type_id = $1`, desired.ID); err != nil {
			return settings.Dictionary{}, settings.Dictionary{}, fmt.Errorf("delete global dictionary items: %w", err)
		}
		if _, err = tx.Exec(ctx, `UPDATE wheretolive.global_dictionary_types SET name = $2, version = version + 1, updated_at = $3 WHERE id = $1`, desired.ID, desired.Name, desired.OccurredAt); err != nil {
			return settings.Dictionary{}, settings.Dictionary{}, fmt.Errorf("update global dictionary: %w", err)
		}
	}
	for _, item := range desired.Items {
		if _, err = tx.Exec(ctx, `INSERT INTO wheretolive.global_dictionary_items (id, dictionary_type_id, code, label, sort_order, enabled, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $7)`, item.ID, desired.ID, item.Code, item.Label, item.SortOrder, item.Enabled, desired.OccurredAt); err != nil {
			return settings.Dictionary{}, settings.Dictionary{}, fmt.Errorf("insert global dictionary item: %w", err)
		}
	}
	after, err := loadGlobalDictionary(ctx, tx, desired.Code, false)
	return before, after, err
}

// ListGlobalConfigurations returns definitions joined to their required defaults.
func (s *Store) ListGlobalConfigurations(ctx context.Context) ([]settings.Configuration, error) {
	rows, err := s.pool.Query(ctx, `SELECT d.id, d.key, d.name, d.value_type, d.tenant_overridable, g.version, g.value FROM wheretolive.configuration_definitions d JOIN wheretolive.global_configuration_values g ON g.key = d.key ORDER BY d.key`)
	if err != nil {
		return nil, fmt.Errorf("query global configurations: %w", err)
	}
	defer rows.Close()
	var configurations []settings.Configuration
	for rows.Next() {
		var configuration settings.Configuration
		if err := rows.Scan(&configuration.ID, &configuration.Key, &configuration.Name, &configuration.ValueType, &configuration.TenantOverridable, &configuration.Version, &configuration.Value); err != nil {
			return nil, fmt.Errorf("scan global configuration: %w", err)
		}
		configuration.Source = "global"
		configurations = append(configurations, configuration)
	}
	return configurations, rows.Err()
}

// PutGlobalConfiguration writes a definition and its global value atomically.
func (*Store) PutGlobalConfiguration(ctx context.Context, tx pgx.Tx, desired settings.GlobalConfigurationWrite) (settings.Configuration, settings.Configuration, error) {
	var before settings.Configuration
	err := tx.QueryRow(ctx, `SELECT d.id, d.key, d.name, d.value_type, d.tenant_overridable, g.version, g.value FROM wheretolive.configuration_definitions d JOIN wheretolive.global_configuration_values g ON g.key = d.key WHERE d.key = $1 FOR UPDATE OF d, g`, desired.Key).Scan(&before.ID, &before.Key, &before.Name, &before.ValueType, &before.TenantOverridable, &before.Version, &before.Value)
	if errors.Is(err, pgx.ErrNoRows) {
		if desired.ExpectedVersion != 0 {
			return settings.Configuration{}, settings.Configuration{}, settings.ErrNotFound
		}
		if _, err = tx.Exec(ctx, `INSERT INTO wheretolive.configuration_definitions (id, key, name, value_type, tenant_overridable, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $6)`, desired.ID, desired.Key, desired.Name, desired.ValueType, desired.TenantOverridable, desired.OccurredAt); err != nil {
			return settings.Configuration{}, settings.Configuration{}, fmt.Errorf("insert configuration definition: %w", err)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO wheretolive.global_configuration_values (key, value, version, created_at, updated_at) VALUES ($1, $2, 1, $3, $3)`, desired.Key, string(desired.Value), desired.OccurredAt); err != nil {
			return settings.Configuration{}, settings.Configuration{}, fmt.Errorf("insert global configuration value: %w", err)
		}
		before = settings.Configuration{}
	} else if err != nil {
		return settings.Configuration{}, settings.Configuration{}, fmt.Errorf("query global configuration: %w", err)
	} else {
		if before.Version != desired.ExpectedVersion {
			return settings.Configuration{}, settings.Configuration{}, settings.ErrConflict
		}
		if before.ValueType != desired.ValueType {
			var overrides bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM wheretolive.tenant_configuration_values WHERE key = $1)`, desired.Key).Scan(&overrides); err != nil {
				return settings.Configuration{}, settings.Configuration{}, err
			}
			if overrides {
				return settings.Configuration{}, settings.Configuration{}, settings.ErrConflict
			}
		}
		if _, err = tx.Exec(ctx, `UPDATE wheretolive.configuration_definitions SET name = $2, value_type = $3, tenant_overridable = $4, updated_at = $5 WHERE key = $1`, desired.Key, desired.Name, desired.ValueType, desired.TenantOverridable, desired.OccurredAt); err != nil {
			return settings.Configuration{}, settings.Configuration{}, fmt.Errorf("update configuration definition: %w", err)
		}
		if _, err = tx.Exec(ctx, `UPDATE wheretolive.global_configuration_values SET value = $2, version = version + 1, updated_at = $3 WHERE key = $1`, desired.Key, string(desired.Value), desired.OccurredAt); err != nil {
			return settings.Configuration{}, settings.Configuration{}, fmt.Errorf("update global configuration value: %w", err)
		}
		desired.ID = before.ID
	}
	after := settings.Configuration{ID: desired.ID, Key: desired.Key, Name: desired.Name, ValueType: desired.ValueType, TenantOverridable: desired.TenantOverridable, Source: "global", Version: desired.ExpectedVersion + 1, Value: append(json.RawMessage(nil), desired.Value...)}
	before.Source = "global"
	return before, after, nil
}

type dictionaryQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func (*Store) listGlobalDictionaryItems(ctx context.Context, queryer dictionaryQuerier, id string) ([]settings.DictionaryItem, error) {
	rows, err := queryer.Query(ctx, `SELECT id, code, label, sort_order, enabled FROM wheretolive.global_dictionary_items WHERE dictionary_type_id = $1 ORDER BY sort_order, code, id`, id)
	if err != nil {
		return nil, fmt.Errorf("query global dictionary items: %w", err)
	}
	defer rows.Close()
	var items []settings.DictionaryItem
	for rows.Next() {
		var item settings.DictionaryItem
		if err := rows.Scan(&item.ID, &item.Code, &item.Label, &item.SortOrder, &item.Enabled); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func loadGlobalDictionary(ctx context.Context, tx pgx.Tx, code string, lock bool) (settings.Dictionary, error) {
	query := `SELECT id, code, name, version FROM wheretolive.global_dictionary_types WHERE code = $1`
	if lock {
		query += ` FOR UPDATE`
	}
	var dictionary settings.Dictionary
	if err := tx.QueryRow(ctx, query, code).Scan(&dictionary.ID, &dictionary.Code, &dictionary.Name, &dictionary.Version); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return settings.Dictionary{}, settings.ErrNotFound
		}
		return settings.Dictionary{}, err
	}
	dictionary.Source = "global"
	store := Store{}
	items, err := store.listGlobalDictionaryItems(ctx, tx, dictionary.ID)
	dictionary.Items = items
	return dictionary, err
}

// ListEffectiveDictionaries applies whole-type tenant-over-global precedence.
func (s *Store) ListEffectiveDictionaries(ctx context.Context, tenantID identity.TenantID) ([]settings.Dictionary, error) {
	rows, err := s.pool.Query(ctx, `
SELECT id, code, name, 'tenant', version FROM wheretolive.tenant_dictionary_types WHERE tenant_id = $1
UNION ALL
SELECT g.id, g.code, g.name, 'global', g.version FROM wheretolive.global_dictionary_types g
WHERE NOT EXISTS (SELECT 1 FROM wheretolive.tenant_dictionary_types t WHERE t.tenant_id = $1 AND t.code = g.code)
ORDER BY code`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("query effective dictionaries: %w", err)
	}
	defer rows.Close()
	var dictionaries []settings.Dictionary
	for rows.Next() {
		var dictionary settings.Dictionary
		if err := rows.Scan(&dictionary.ID, &dictionary.Code, &dictionary.Name, &dictionary.Source, &dictionary.Version); err != nil {
			return nil, fmt.Errorf("scan effective dictionary: %w", err)
		}
		dictionaries = append(dictionaries, dictionary)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate effective dictionaries: %w", err)
	}
	for i := range dictionaries {
		items, err := s.listDictionaryItems(ctx, tenantID, dictionaries[i])
		if err != nil {
			return nil, err
		}
		dictionaries[i].Items = items
	}
	return dictionaries, nil
}

func (s *Store) listDictionaryItems(ctx context.Context, tenantID identity.TenantID, dictionary settings.Dictionary) ([]settings.DictionaryItem, error) {
	var rows pgx.Rows
	var err error
	if dictionary.Source == "tenant" {
		rows, err = s.pool.Query(ctx, `SELECT id, code, label, sort_order, enabled FROM wheretolive.tenant_dictionary_items WHERE tenant_id = $1 AND dictionary_type_id = $2 ORDER BY sort_order, code, id`, tenantID, dictionary.ID)
	} else {
		rows, err = s.pool.Query(ctx, `SELECT id, code, label, sort_order, enabled FROM wheretolive.global_dictionary_items WHERE dictionary_type_id = $1 ORDER BY sort_order, code, id`, dictionary.ID)
	}
	if err != nil {
		return nil, fmt.Errorf("query dictionary items: %w", err)
	}
	defer rows.Close()
	items := make([]settings.DictionaryItem, 0)
	for rows.Next() {
		var item settings.DictionaryItem
		if err := rows.Scan(&item.ID, &item.Code, &item.Label, &item.SortOrder, &item.Enabled); err != nil {
			return nil, fmt.Errorf("scan dictionary item: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// ReplaceTenantDictionary creates or replaces a whole tenant-owned type.
func (s *Store) ReplaceTenantDictionary(ctx context.Context, tx pgx.Tx, desired settings.DictionaryWrite) (settings.Dictionary, settings.Dictionary, error) {
	before, err := loadTenantDictionary(ctx, tx, desired.TenantID, desired.Code, true)
	if err != nil && !errors.Is(err, settings.ErrNotFound) {
		return settings.Dictionary{}, settings.Dictionary{}, err
	}
	if desired.ExpectedVersion == 0 {
		if err == nil {
			return settings.Dictionary{}, settings.Dictionary{}, settings.ErrConflict
		}
		_, err = tx.Exec(ctx, `INSERT INTO wheretolive.tenant_dictionary_types (id, tenant_id, code, name, version, created_at, updated_at) VALUES ($1, $2, $3, $4, 1, $5, $5)`, desired.ID, desired.TenantID, desired.Code, desired.Name, desired.OccurredAt)
		if err != nil {
			return settings.Dictionary{}, settings.Dictionary{}, fmt.Errorf("insert tenant dictionary: %w", err)
		}
		before = settings.Dictionary{}
	} else {
		if errors.Is(err, settings.ErrNotFound) {
			return settings.Dictionary{}, settings.Dictionary{}, settings.ErrNotFound
		}
		if before.Version != desired.ExpectedVersion {
			return settings.Dictionary{}, settings.Dictionary{}, settings.ErrConflict
		}
		desired.ID = before.ID
		if _, err := tx.Exec(ctx, `DELETE FROM wheretolive.tenant_dictionary_items WHERE tenant_id = $1 AND dictionary_type_id = $2`, desired.TenantID, desired.ID); err != nil {
			return settings.Dictionary{}, settings.Dictionary{}, fmt.Errorf("delete tenant dictionary items: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE wheretolive.tenant_dictionary_types SET name = $3, version = version + 1, updated_at = $4 WHERE tenant_id = $1 AND id = $2`, desired.TenantID, desired.ID, desired.Name, desired.OccurredAt); err != nil {
			return settings.Dictionary{}, settings.Dictionary{}, fmt.Errorf("update tenant dictionary: %w", err)
		}
	}
	for _, item := range desired.Items {
		if _, err := tx.Exec(ctx, `INSERT INTO wheretolive.tenant_dictionary_items (id, tenant_id, dictionary_type_id, code, label, sort_order, enabled, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8)`, item.ID, desired.TenantID, desired.ID, item.Code, item.Label, item.SortOrder, item.Enabled, desired.OccurredAt); err != nil {
			return settings.Dictionary{}, settings.Dictionary{}, fmt.Errorf("insert tenant dictionary item: %w", err)
		}
	}
	after, err := loadTenantDictionary(ctx, tx, desired.TenantID, desired.Code, false)
	return before, after, err
}

// DeleteTenantDictionary deletes a version-matched tenant type only.
func (*Store) DeleteTenantDictionary(ctx context.Context, tx pgx.Tx, tenantID identity.TenantID, code string, expectedVersion int64) (settings.Dictionary, error) {
	before, err := loadTenantDictionary(ctx, tx, tenantID, code, true)
	if err != nil {
		return settings.Dictionary{}, err
	}
	if before.Version != expectedVersion {
		return settings.Dictionary{}, settings.ErrConflict
	}
	if _, err := tx.Exec(ctx, `DELETE FROM wheretolive.tenant_dictionary_types WHERE tenant_id = $1 AND id = $2`, tenantID, before.ID); err != nil {
		return settings.Dictionary{}, fmt.Errorf("delete tenant dictionary: %w", err)
	}
	return before, nil
}

func loadTenantDictionary(ctx context.Context, tx pgx.Tx, tenantID identity.TenantID, code string, lock bool) (settings.Dictionary, error) {
	query := `SELECT id, code, name, version FROM wheretolive.tenant_dictionary_types WHERE tenant_id = $1 AND code = $2`
	if lock {
		query += ` FOR UPDATE`
	}
	var dictionary settings.Dictionary
	if err := tx.QueryRow(ctx, query, tenantID, code).Scan(&dictionary.ID, &dictionary.Code, &dictionary.Name, &dictionary.Version); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return settings.Dictionary{}, settings.ErrNotFound
		}
		return settings.Dictionary{}, fmt.Errorf("query tenant dictionary: %w", err)
	}
	dictionary.Source = "tenant"
	rows, err := tx.Query(ctx, `SELECT id, code, label, sort_order, enabled FROM wheretolive.tenant_dictionary_items WHERE tenant_id = $1 AND dictionary_type_id = $2 ORDER BY sort_order, code, id`, tenantID, dictionary.ID)
	if err != nil {
		return settings.Dictionary{}, fmt.Errorf("query tenant dictionary items: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item settings.DictionaryItem
		if err := rows.Scan(&item.ID, &item.Code, &item.Label, &item.SortOrder, &item.Enabled); err != nil {
			return settings.Dictionary{}, fmt.Errorf("scan tenant dictionary item: %w", err)
		}
		dictionary.Items = append(dictionary.Items, item)
	}
	return dictionary, rows.Err()
}

// ListEffectiveConfigurations applies eligible tenant-over-global precedence.
func (s *Store) ListEffectiveConfigurations(ctx context.Context, tenantID identity.TenantID) ([]settings.Configuration, error) {
	rows, err := s.pool.Query(ctx, `SELECT d.id, d.key, d.name, d.value_type, d.tenant_overridable, CASE WHEN t.key IS NOT NULL THEN 'tenant' ELSE 'global' END, COALESCE(t.version, g.version), COALESCE(t.value, g.value) FROM wheretolive.configuration_definitions d JOIN wheretolive.global_configuration_values g ON g.key = d.key LEFT JOIN wheretolive.tenant_configuration_values t ON t.tenant_id = $1 AND t.key = d.key AND d.tenant_overridable ORDER BY d.key`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("query effective configurations: %w", err)
	}
	defer rows.Close()
	var configurations []settings.Configuration
	for rows.Next() {
		var item settings.Configuration
		if err := rows.Scan(&item.ID, &item.Key, &item.Name, &item.ValueType, &item.TenantOverridable, &item.Source, &item.Version, &item.Value); err != nil {
			return nil, fmt.Errorf("scan effective configuration: %w", err)
		}
		configurations = append(configurations, item)
	}
	return configurations, rows.Err()
}

// PutTenantConfiguration writes one versioned eligible tenant override.
func (*Store) PutTenantConfiguration(ctx context.Context, tx pgx.Tx, desired settings.ConfigurationWrite) (settings.Configuration, settings.Configuration, error) {
	var definition settings.Configuration
	if err := tx.QueryRow(ctx, `SELECT id, key, name, value_type, tenant_overridable FROM wheretolive.configuration_definitions WHERE key = $1`, desired.Key).Scan(&definition.ID, &definition.Key, &definition.Name, &definition.ValueType, &definition.TenantOverridable); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return settings.Configuration{}, settings.Configuration{}, settings.ErrNotFound
		}
		return settings.Configuration{}, settings.Configuration{}, fmt.Errorf("query configuration definition: %w", err)
	}
	if !definition.TenantOverridable {
		return settings.Configuration{}, settings.Configuration{}, settings.ErrNotOverridable
	}
	if !validValueType(definition.ValueType, desired.Value) {
		return settings.Configuration{}, settings.Configuration{}, fmt.Errorf("configuration value type mismatch")
	}
	var before settings.Configuration
	err := tx.QueryRow(ctx, `SELECT version, value FROM wheretolive.tenant_configuration_values WHERE tenant_id = $1 AND key = $2 FOR UPDATE`, desired.TenantID, desired.Key).Scan(&before.Version, &before.Value)
	if errors.Is(err, pgx.ErrNoRows) {
		if desired.ExpectedVersion != 0 {
			return settings.Configuration{}, settings.Configuration{}, settings.ErrNotFound
		}
		_, err = tx.Exec(ctx, `INSERT INTO wheretolive.tenant_configuration_values (tenant_id, key, value, version, created_at, updated_at) VALUES ($1, $2, $3, 1, $4, $4)`, desired.TenantID, desired.Key, string(desired.Value), desired.OccurredAt)
		if err != nil {
			return settings.Configuration{}, settings.Configuration{}, fmt.Errorf("insert tenant configuration: %w", err)
		}
	} else if err != nil {
		return settings.Configuration{}, settings.Configuration{}, fmt.Errorf("query tenant configuration: %w", err)
	} else {
		if before.Version != desired.ExpectedVersion {
			return settings.Configuration{}, settings.Configuration{}, settings.ErrConflict
		}
		if _, err := tx.Exec(ctx, `UPDATE wheretolive.tenant_configuration_values SET value = $3, version = version + 1, updated_at = $4 WHERE tenant_id = $1 AND key = $2`, desired.TenantID, desired.Key, string(desired.Value), desired.OccurredAt); err != nil {
			return settings.Configuration{}, settings.Configuration{}, fmt.Errorf("update tenant configuration: %w", err)
		}
	}
	after := definition
	after.Source = "tenant"
	after.Value = append(json.RawMessage(nil), desired.Value...)
	after.Version = desired.ExpectedVersion + 1
	before.ID, before.Key, before.Name, before.ValueType, before.TenantOverridable, before.Source = definition.ID, definition.Key, definition.Name, definition.ValueType, true, "tenant"
	return before, after, nil
}

func validValueType(valueType string, value json.RawMessage) bool {
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
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
		number, ok := decoded.(json.Number)
		if !ok {
			return false
		}
		_, err := number.Int64()
		return err == nil
	case "json":
		return true
	default:
		return false
	}
}
