// Package feedback owns restricted intake and human processing. It exposes no
// anonymous submission, tenant-owned account cases, or public demand signal.
package feedback

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/audit"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/places"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/platformadmin"
)

var (
	// ErrInvalid rejects malformed intake and incomplete outcomes.
	ErrInvalid = errors.New("invalid feedback")
	// ErrDenied rejects an unverified platform actor.
	ErrDenied = errors.New("feedback access denied")
	// ErrNotFound hides missing intake records.
	ErrNotFound = errors.New("feedback not found")
	// ErrConflict rejects stale writes and illegal transitions.
	ErrConflict = errors.New("feedback conflict")
)

// Category comes from the module's active catalogue, not a frontend enum.
type Category struct{ Key, Label string }

// Entry is a restricted global staff record; category and message stay original.
type Entry struct {
	ID, Category, Title, Message, Status string
	PlaceID                              *string
	Outcome                              *string
	Version                              int64
	CreatedAt, UpdatedAt                 time.Time
}

// Query carries bounded filters without free SQL fragments.
type Query struct {
	Status, Category string
	Limit, Offset    int
}

// Page has a stable ordering and an optional next offset.
type Page struct {
	Items      []Entry
	NextOffset *int
}

// Create records manual intake only, without account association.
type Create struct {
	Category, Title, Message string
	PlaceID                  *string
}

// WriteContext supplies a verified platform actor and audit reason.
type WriteContext struct {
	Actor                 platformadmin.Actor
	Reason, CorrelationID string
}

// Review changes processing state, never the original submission.
type Review struct {
	Version int64
	Status  string
	Outcome *string
}

// Store accepts application-owned transactions.
type Store interface {
	Categories(context.Context) ([]Category, error)
	List(context.Context, Query) ([]Entry, error)
	Insert(context.Context, pgx.Tx, Entry) (Entry, error)
	Lock(context.Context, pgx.Tx, string) (Entry, error)
	Update(context.Context, pgx.Tx, Entry) (Entry, error)
}

// Transactor owns the write and its audit as one unit.
type Transactor interface {
	WithinTransaction(context.Context, func(pgx.Tx) error) error
}

// Auditor records only processing metadata; original text is excluded.
type Auditor interface {
	RecordPlatformWrite(context.Context, pgx.Tx, audit.PlatformEvent) error
}

// PlaceReader verifies links through the places application API.
type PlaceReader interface {
	Get(context.Context, platformadmin.Actor, string) (places.Entry, error)
}

// Service holds explicit intake dependencies.
type Service struct {
	store  Store
	tx     Transactor
	audit  Auditor
	places PlaceReader
	now    func() time.Time
	newID  func(time.Time) (string, error)
}

// NewService wires the restricted intake workflow.
func NewService(store Store, tx Transactor, auditor Auditor, placeReader PlaceReader, now func() time.Time, newID func(time.Time) (string, error)) (*Service, error) {
	if store == nil || tx == nil || auditor == nil || placeReader == nil || now == nil || newID == nil {
		return nil, fmt.Errorf("invalid feedback dependencies")
	}
	return &Service{store, tx, auditor, placeReader, now, newID}, nil
}
func validID(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id.Version() == 7 && id.Variant() == uuid.RFC4122
}
func validActor(a platformadmin.Actor) bool {
	return validID(string(a.AdministratorID)) && validID(string(a.SessionID))
}
func text(value string, maximum int) bool {
	return utf8.ValidString(value) && strings.TrimSpace(value) != "" && utf8.RuneCountInString(value) <= maximum && !strings.ContainsRune(value, 0)
}

var categoryKey = regexp.MustCompile(`^[a-z][a-z0-9_]{0,49}$`)

func validStatus(status string) bool {
	return status == "open" || status == "in_review" || status == "resolved" || status == "dismissed"
}
func validateWrite(w WriteContext) error {
	if !validActor(w.Actor) {
		return ErrDenied
	}
	if !text(w.Reason, 500) || !text(w.CorrelationID, 128) {
		return ErrInvalid
	}
	return nil
}

// Categories lists the active staff catalogue to verified operators only.
func (s *Service) Categories(ctx context.Context, a platformadmin.Actor) ([]Category, error) {
	if !validActor(a) {
		return nil, ErrDenied
	}
	return s.store.Categories(ctx)
}

// List reads only restricted staff intake, without publishing its contents.
func (s *Service) List(ctx context.Context, a platformadmin.Actor, q Query) (Page, error) {
	if !validActor(a) {
		return Page{}, ErrDenied
	}
	if q.Limit < 1 || q.Limit > 50 || q.Offset < 0 || q.Offset > 10000 || (q.Status != "" && !validStatus(q.Status)) || (q.Category != "" && !categoryKey.MatchString(q.Category)) {
		return Page{}, ErrInvalid
	}
	limit := q.Limit
	q.Limit++
	entries, err := s.store.List(ctx, q)
	if err != nil {
		return Page{}, fmt.Errorf("list feedback: %w", err)
	}
	if entries == nil {
		entries = []Entry{}
	}
	page := Page{Items: entries}
	if len(entries) > limit {
		next := q.Offset + limit
		page.NextOffset = &next
		page.Items = entries[:limit]
	}
	return page, nil
}

// Create preserves original classification and records a transactional audit.
func (s *Service) Create(ctx context.Context, w WriteContext, input Create) (Entry, error) {
	if err := validateWrite(w); err != nil {
		return Entry{}, err
	}
	input.Title = strings.TrimSpace(input.Title)
	input.Message = strings.TrimSpace(input.Message)
	if !categoryKey.MatchString(input.Category) || !text(input.Title, 200) || !text(input.Message, 5000) || (input.PlaceID != nil && !validID(*input.PlaceID)) {
		return Entry{}, ErrInvalid
	}
	if input.PlaceID != nil {
		if _, err := s.places.Get(ctx, w.Actor, *input.PlaceID); err != nil {
			if errors.Is(err, places.ErrNotFound) || errors.Is(err, places.ErrInvalidPlace) {
				return Entry{}, ErrInvalid
			}
			return Entry{}, fmt.Errorf("verify feedback place: %w", err)
		}
	}
	now := s.now().UTC()
	id, err := s.newID(now)
	if err != nil {
		return Entry{}, fmt.Errorf("generate feedback ID: %w", err)
	}
	if !validID(id) {
		return Entry{}, fmt.Errorf("invalid generated feedback ID")
	}
	desired := Entry{ID: id, Category: input.Category, Title: input.Title, Message: input.Message, PlaceID: input.PlaceID, Status: "open", Version: 1, CreatedAt: now, UpdatedAt: now}
	var result Entry
	err = s.tx.WithinTransaction(ctx, func(tx pgx.Tx) error {
		var err error
		result, err = s.store.Insert(ctx, tx, desired)
		if err != nil {
			return err
		}
		return s.record(ctx, tx, w, "feedback.recorded", nil, result)
	})
	return result, err
}

// CanTransition is the fixed processing state machine, independent of labels.
func CanTransition(from, to string) bool {
	switch from {
	case "open":
		return to == "in_review" || to == "dismissed"
	case "in_review":
		return to == "resolved" || to == "dismissed"
	case "resolved", "dismissed":
		return to == "in_review"
	default:
		return false
	}
}

// Review requires an actual outcome for closing and clears it on reopening.
func (s *Service) Review(ctx context.Context, w WriteContext, id string, input Review) (Entry, error) {
	if err := validateWrite(w); err != nil {
		return Entry{}, err
	}
	terminal := input.Status == "resolved" || input.Status == "dismissed"
	if !validID(id) || input.Version < 1 || !validStatus(input.Status) || terminal != (input.Outcome != nil) || (input.Outcome != nil && !text(*input.Outcome, 2000)) {
		return Entry{}, ErrInvalid
	}
	var result Entry
	err := s.tx.WithinTransaction(ctx, func(tx pgx.Tx) error {
		before, err := s.store.Lock(ctx, tx, id)
		if err != nil {
			return err
		}
		if before.Version != input.Version || !CanTransition(before.Status, input.Status) {
			return ErrConflict
		}
		desired := before
		desired.Status = input.Status
		desired.Outcome = input.Outcome
		desired.UpdatedAt = s.now().UTC()
		result, err = s.store.Update(ctx, tx, desired)
		if err != nil {
			return err
		}
		return s.record(ctx, tx, w, "feedback.reviewed", &before, result)
	})
	return result, err
}
func auditState(e Entry) json.RawMessage {
	// Exclude title, body, and outcome: they may contain personal information.
	data, _ := json.Marshal(struct {
		Status   string `json:"status"`
		Version  int64  `json:"version"`
		Category string `json:"category"`
	}{e.Status, e.Version, e.Category})
	return data
}
func (s *Service) record(ctx context.Context, tx pgx.Tx, w WriteContext, action string, before *Entry, after Entry) error {
	var previous json.RawMessage
	if before != nil {
		previous = auditState(*before)
	}
	return s.audit.RecordPlatformWrite(ctx, tx, audit.PlatformEvent{ActorID: string(w.Actor.AdministratorID), Action: action, Resource: "feedback", ResourceID: after.ID, Reason: w.Reason, CorrelationID: w.CorrelationID, OccurredAt: after.UpdatedAt, BeforeState: previous, AfterState: auditState(after)})
}
