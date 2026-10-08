package places

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
	_ "time/tzdata" // Embed IANA data so validation is consistent in minimal deployments.
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/modura-dev/modura/backend/internal/modules/audit"
	"github.com/modura-dev/modura/backend/internal/modules/platformadmin"
	"golang.org/x/text/currency"
	"golang.org/x/text/language"
)

var (
	// ErrInvalidPlace rejects invalid geography, metadata or write evidence.
	ErrInvalidPlace = errors.New("invalid place")
	// ErrConflict rejects stale versions, duplicate slugs or unpublished ancestors.
	ErrConflict = errors.New("place conflict")
	// ErrDenied rejects a missing verified platform principal.
	ErrDenied = errors.New("platform place access denied")
)

// Alias is a localized name; at most one preferred alias exists per locale.
type Alias struct {
	Locale    string
	Name      string
	Preferred bool
}

// Details is the editable catalogue metadata, independent of stable geography.
type Details struct {
	Name      string
	Timezone  *string
	Latitude  *float64
	Longitude *float64
	Currency  *string
	Languages []string
	Aliases   []Alias
}

// Entry includes private draft state and its concurrent-edit version.
type Entry struct {
	ID          string
	Slug        string
	Type        string
	ParentID    *string
	CountryCode string
	Details
	Version       int64
	CoverageLevel int
	PublishedAt   *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Create describes stable geography and initial metadata; creation is always draft.
type Create struct {
	Slug        string
	Type        string
	ParentID    *string
	CountryCode string
	Details
}

// WriteContext supplies the verified platform actor and audit evidence.
type WriteContext struct {
	Actor         platformadmin.Actor
	Reason        string
	CorrelationID string
}

// ManagedPage is a bounded platform catalogue page including drafts.
type ManagedPage struct {
	Items      []Entry
	NextOffset *int
}

// ManagementStore persists changes in the application's transaction.
type ManagementStore interface {
	ListManaged(context.Context, Query) ([]Entry, error)
	GetManaged(context.Context, string) (Entry, error)
	CreateManaged(context.Context, pgx.Tx, Entry) (Entry, error)
	UpdateManaged(context.Context, pgx.Tx, string, int64, Details, time.Time) (Entry, Entry, error)
	PublishManaged(context.Context, pgx.Tx, string, int64, bool, time.Time) (Entry, Entry, error)
}

// Transactor owns transaction lifecycle around catalogue and audit writes.
type Transactor interface {
	WithinTransaction(context.Context, func(pgx.Tx) error) error
}

// PlatformAuditor records evidence in the same transaction as the place write.
type PlatformAuditor interface {
	RecordPlatformWrite(context.Context, pgx.Tx, audit.PlatformEvent) error
}

// Management provides platform-only catalogue operations.
type Management struct {
	store        ManagementStore
	transactions Transactor
	auditor      PlatformAuditor
	now          func() time.Time
	newID        func(time.Time) (string, error)
}

// NewManagement requires explicit persistence, transaction, audit, clock and ID dependencies.
func NewManagement(store ManagementStore, transactions Transactor, auditor PlatformAuditor, now func() time.Time, newID func(time.Time) (string, error)) (*Management, error) {
	if store == nil || transactions == nil || auditor == nil || now == nil || newID == nil {
		return nil, fmt.Errorf("invalid place management configuration")
	}
	return &Management{store, transactions, auditor, now, newID}, nil
}
func validActor(actor platformadmin.Actor) bool {
	return validID(string(actor.AdministratorID)) && validID(string(actor.SessionID))
}
func validID(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id.Version() == 7 && id.Variant() == uuid.RFC4122
}
func validateWrite(write WriteContext) error {
	if !validActor(write.Actor) {
		return ErrDenied
	}
	if !validText(write.Reason, 500) || !validText(write.CorrelationID, 128) {
		return ErrInvalidPlace
	}
	return nil
}
func validText(text string, maximum int) bool {
	return utf8.ValidString(text) && strings.TrimSpace(text) != "" && utf8.RuneCountInString(text) <= maximum && !strings.ContainsRune(text, 0)
}
func normalizeDetails(details Details) (Details, error) {
	details.Name = strings.TrimSpace(details.Name)
	if !validText(details.Name, 200) || len(details.Aliases) > 100 || len(details.Languages) > 20 {
		return Details{}, ErrInvalidPlace
	}
	if details.Timezone != nil {
		zone := *details.Timezone
		if zone == "Local" || strings.TrimSpace(zone) != zone || len(zone) > 128 {
			return Details{}, ErrInvalidPlace
		}
		if _, err := time.LoadLocation(zone); err != nil {
			return Details{}, ErrInvalidPlace
		}
	}
	if details.Currency != nil {
		code := *details.Currency
		if len(code) != 3 || code != strings.ToUpper(code) {
			return Details{}, ErrInvalidPlace
		}
		if _, err := currency.ParseISO(code); err != nil {
			return Details{}, ErrInvalidPlace
		}
	}
	if (details.Latitude == nil) != (details.Longitude == nil) {
		return Details{}, ErrInvalidPlace
	}
	if details.Latitude != nil && (math.IsNaN(*details.Latitude) || math.IsInf(*details.Latitude, 0) || math.IsNaN(*details.Longitude) || math.IsInf(*details.Longitude, 0) || *details.Latitude < -90 || *details.Latitude > 90 || *details.Longitude < -180 || *details.Longitude > 180) {
		return Details{}, ErrInvalidPlace
	}
	details.Languages = slices.Clone(details.Languages)
	if details.Languages == nil {
		details.Languages = []string{}
	}
	languages := map[string]bool{}
	for i, code := range details.Languages {
		tag, err := language.Parse(code)
		if err != nil || tag == language.Und || len(code) > 35 || strings.Contains(code, "_") {
			return Details{}, ErrInvalidPlace
		}
		details.Languages[i] = tag.String()
		if languages[tag.String()] {
			return Details{}, ErrInvalidPlace
		}
		languages[tag.String()] = true
	}
	details.Aliases = slices.Clone(details.Aliases)
	if details.Aliases == nil {
		details.Aliases = []Alias{}
	}
	aliases, preferred := map[string]bool{}, map[string]bool{}
	for i, alias := range details.Aliases {
		tag, err := language.Parse(alias.Locale)
		if err != nil || tag == language.Und || len(alias.Locale) > 35 || strings.Contains(alias.Locale, "_") || !validText(alias.Name, 200) {
			return Details{}, ErrInvalidPlace
		}
		alias.Locale = tag.String()
		alias.Name = strings.TrimSpace(alias.Name)
		key := alias.Locale + "/" + NormalizeName(alias.Name)
		if aliases[key] || (alias.Preferred && preferred[alias.Locale]) {
			return Details{}, ErrInvalidPlace
		}
		aliases[key] = true
		if alias.Preferred {
			preferred[alias.Locale] = true
		}
		details.Aliases[i] = alias
	}
	return details, nil
}

// List returns drafts and published entries only to verified platform administrators.
func (s *Management) List(ctx context.Context, actor platformadmin.Actor, query Query) (ManagedPage, error) {
	if !validActor(actor) {
		return ManagedPage{}, ErrDenied
	}
	if query.Limit < 1 || query.Limit > 50 || query.Offset < 0 || query.Offset > 10000 || utf8.RuneCountInString(query.Search) > 120 || !utf8.ValidString(query.Search) {
		return ManagedPage{}, ErrInvalidQuery
	}
	query.Search = NormalizeName(query.Search)
	limit := query.Limit
	query.Limit++
	items, err := s.store.ListManaged(ctx, query)
	if err != nil {
		return ManagedPage{}, fmt.Errorf("list managed places: %w", err)
	}
	page := ManagedPage{Items: items}
	if page.Items == nil {
		page.Items = []Entry{}
	}
	if len(items) > limit {
		next := query.Offset + limit
		page.NextOffset = &next
		page.Items = items[:limit]
	}
	return page, nil
}

// Get returns an entry with aliases, including unpublished state.
func (s *Management) Get(ctx context.Context, actor platformadmin.Actor, id string) (Entry, error) {
	if !validActor(actor) {
		return Entry{}, ErrDenied
	}
	if !validID(id) {
		return Entry{}, ErrInvalidPlace
	}
	return s.store.GetManaged(ctx, id)
}

// Create inserts a draft with stable geography and mandatory transactional audit.
func (s *Management) Create(ctx context.Context, write WriteContext, input Create) (Entry, error) {
	if err := validateWrite(write); err != nil {
		return Entry{}, err
	}
	region, err := language.ParseRegion(input.CountryCode)
	if err != nil || len(input.CountryCode) != 2 || !region.IsCountry() || region.String() != input.CountryCode || len(input.Slug) > 120 || !stableSlug.MatchString(input.Slug) || !slices.Contains([]string{"country", "region", "island", "city", "district"}, input.Type) || ((input.Type == "country") != (input.ParentID == nil)) || (input.ParentID != nil && !validID(*input.ParentID)) {
		return Entry{}, ErrInvalidPlace
	}
	details, err := normalizeDetails(input.Details)
	if err != nil {
		return Entry{}, err
	}
	now := s.now().UTC()
	id, err := s.newID(now)
	if err != nil {
		return Entry{}, fmt.Errorf("generate place ID: %w", err)
	}
	if !validID(id) {
		return Entry{}, fmt.Errorf("generated invalid place ID")
	}
	desired := Entry{ID: id, Slug: input.Slug, Type: input.Type, ParentID: input.ParentID, CountryCode: input.CountryCode, Details: details, CreatedAt: now, UpdatedAt: now}
	var result Entry
	err = s.transactions.WithinTransaction(ctx, func(tx pgx.Tx) error {
		var err error
		result, err = s.store.CreateManaged(ctx, tx, desired)
		if err != nil {
			return err
		}
		return s.record(ctx, tx, write, "places.created", nil, result)
	})
	return result, err
}

// Update changes metadata only; stable slug and geography are not client-editable.
func (s *Management) Update(ctx context.Context, write WriteContext, id string, version int64, details Details) (Entry, error) {
	if err := validateWrite(write); err != nil {
		return Entry{}, err
	}
	if !validID(id) || version < 1 {
		return Entry{}, ErrInvalidPlace
	}
	normalized, err := normalizeDetails(details)
	if err != nil {
		return Entry{}, err
	}
	return s.change(ctx, write, "places.updated", func(tx pgx.Tx) (Entry, Entry, error) {
		return s.store.UpdateManaged(ctx, tx, id, version, normalized, s.now().UTC())
	})
}

// Publish toggles publication with a stale-edit guard and ancestor checks.
func (s *Management) Publish(ctx context.Context, write WriteContext, id string, version int64, published bool) (Entry, error) {
	if err := validateWrite(write); err != nil {
		return Entry{}, err
	}
	if !validID(id) || version < 1 {
		return Entry{}, ErrInvalidPlace
	}
	return s.change(ctx, write, "places.publication_changed", func(tx pgx.Tx) (Entry, Entry, error) {
		return s.store.PublishManaged(ctx, tx, id, version, published, s.now().UTC())
	})
}
func (s *Management) change(ctx context.Context, write WriteContext, action string, work func(pgx.Tx) (Entry, Entry, error)) (Entry, error) {
	var result Entry
	err := s.transactions.WithinTransaction(ctx, func(tx pgx.Tx) error {
		before, after, err := work(tx)
		if err != nil {
			return err
		}
		result = after
		return s.record(ctx, tx, write, action, before, after)
	})
	return result, err
}
func (s *Management) record(ctx context.Context, tx pgx.Tx, write WriteContext, action string, before any, after Entry) error {
	beforeJSON, err := json.Marshal(before)
	if err != nil {
		return err
	}
	afterJSON, err := json.Marshal(after)
	if err != nil {
		return err
	}
	return s.auditor.RecordPlatformWrite(ctx, tx, audit.PlatformEvent{ActorID: string(write.Actor.AdministratorID), Action: action, Resource: "place", ResourceID: after.ID, Reason: write.Reason, CorrelationID: write.CorrelationID, OccurredAt: after.UpdatedAt, BeforeState: beforeJSON, AfterState: afterJSON})
}
