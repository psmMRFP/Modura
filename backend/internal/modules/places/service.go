// Package places owns the global geography catalogue and its public projection.
package places

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

var (
	// ErrInvalidQuery rejects malformed or unbounded public input.
	ErrInvalidQuery = errors.New("invalid place query")
	// ErrNotFound hides the existence of unpublished records.
	ErrNotFound = errors.New("place not found")
	stableSlug  = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
)

// Place is a published, localized view; missing metadata is explicitly absent.
type Place struct {
	ID            string
	Slug          string
	Name          string
	DisplayName   string
	Type          string
	ParentID      *string
	CountryCode   string
	Timezone      *string
	Latitude      *float64
	Longitude     *float64
	Currency      *string
	Languages     []string
	CoverageLevel int
	PublishedAt   time.Time
}

// Query is bounded public search input. Locale changes display, not search scope.
type Query struct {
	Search string
	Locale string
	Limit  int
	Offset int
}

// Page is a bounded result with an optional continuation offset.
type Page struct {
	Items      []Place
	NextOffset *int
}

// Store reads only the explicitly published global projection.
type Store interface {
	SearchPublic(context.Context, Query) ([]Place, error)
	GetPublic(context.Context, string, string) (Place, error)
}

// Service exposes anonymous reads without depending on identity or HTTP.
type Service struct{ store Store }

// NewService creates the public catalogue service.
func NewService(store Store) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("place store is required")
	}
	return &Service{store: store}, nil
}

// NormalizeName is used for stored names and query text, including Unicode width.
func NormalizeName(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(norm.NFKC.String(value)), " "))
}

// Search returns a stable page and does not expose unpublished records.
func (s *Service) Search(ctx context.Context, query Query) (Page, error) {
	if !validLocale(query.Locale) || query.Limit < 1 || query.Limit > 50 || query.Offset < 0 || query.Offset > 10000 || !utf8.ValidString(query.Search) || utf8.RuneCountInString(query.Search) > 120 {
		return Page{}, ErrInvalidQuery
	}
	query.Search = NormalizeName(query.Search)
	requestedLimit := query.Limit
	query.Limit++ // Fetch one extra row to distinguish the final page without a count.
	items, err := s.store.SearchPublic(ctx, query)
	if err != nil {
		return Page{}, fmt.Errorf("search public places: %w", err)
	}
	page := Page{Items: items}
	if page.Items == nil {
		page.Items = []Place{}
	}
	if len(items) > requestedLimit {
		next := query.Offset + requestedLimit
		page.NextOffset = &next
		page.Items = items[:requestedLimit]
	}
	return page, nil
}

// Get resolves a stable slug; drafts and nonexistent places have the same error.
func (s *Service) Get(ctx context.Context, slug, locale string) (Place, error) {
	if len(slug) > 120 || !stableSlug.MatchString(slug) || !validLocale(locale) {
		return Place{}, ErrInvalidQuery
	}
	place, err := s.store.GetPublic(ctx, slug, locale)
	if err != nil {
		return Place{}, fmt.Errorf("get public place: %w", err)
	}
	return place, nil
}

func validLocale(locale string) bool {
	return slices.Contains([]string{"en", "zh-CN", "de", "fr", "es"}, locale)
}
