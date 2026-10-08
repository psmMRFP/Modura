// Package postgres reads the places-owned published projection using sqlc.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/modura-dev/modura/backend/internal/modules/places"
	placesdb "github.com/modura-dev/modura/backend/internal/modules/places/postgres/db"
)

// Store accepts a caller-supplied transaction-capable database handle.
type Store struct{ queries *placesdb.Queries }

// New binds the published read repository to a pool, connection or transaction.
func New(db placesdb.DBTX) Store { return Store{queries: placesdb.New(db)} }

// SearchPublic never reads the unfiltered catalogue, including matching aliases.
func (s Store) SearchPublic(ctx context.Context, query places.Query) ([]places.Place, error) {
	prefix := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query.Search) + "%"
	rows, err := s.queries.SearchPublicPlaces(ctx, placesdb.SearchPublicPlacesParams{Locale: query.Locale, Search: query.Search, Prefix: prefix, PageLimit: int32(query.Limit), PageOffset: int32(query.Offset)})
	if err != nil {
		return nil, fmt.Errorf("query places: %w", err)
	}
	result := make([]places.Place, 0, len(rows))
	for _, row := range rows {
		result = append(result, convert(placesdb.GetPublicPlaceRow(row)))
	}
	return result, nil
}

// GetPublic hides drafts, future publications and unpublished ancestors.
func (s Store) GetPublic(ctx context.Context, slug, locale string) (places.Place, error) {
	row, err := s.queries.GetPublicPlace(ctx, placesdb.GetPublicPlaceParams{Slug: slug, Locale: locale})
	if errors.Is(err, pgx.ErrNoRows) {
		return places.Place{}, places.ErrNotFound
	}
	if err != nil {
		return places.Place{}, fmt.Errorf("query place: %w", err)
	}
	return convert(row), nil
}

func convert(row placesdb.GetPublicPlaceRow) places.Place {
	p := places.Place{ID: row.ID, Slug: row.Slug, Name: row.Name, DisplayName: row.DisplayName, Type: row.Type, CountryCode: row.CountryCode, Languages: row.Languages, CoverageLevel: int(row.CoverageLevel), PublishedAt: row.PublishedAt.Time.UTC()}
	if p.Languages == nil {
		p.Languages = []string{}
	}
	if row.ParentID.Valid {
		id := uuid.UUID(row.ParentID.Bytes).String()
		p.ParentID = &id
	}
	if row.Timezone.Valid {
		p.Timezone = &row.Timezone.String
	}
	if row.Currency.Valid {
		p.Currency = &row.Currency.String
	}
	if row.Latitude.Valid {
		p.Latitude = &row.Latitude.Float64
	}
	if row.Longitude.Valid {
		p.Longitude = &row.Longitude.Float64
	}
	return p
}
