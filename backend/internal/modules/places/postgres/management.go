package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/modura-dev/modura/backend/internal/modules/places"
	placesdb "github.com/modura-dev/modura/backend/internal/modules/places/postgres/db"
)

// ListManaged reads the private platform catalogue; aliases follow owner-local queries.
func (s Store) ListManaged(ctx context.Context, query places.Query) ([]places.Entry, error) {
	prefix := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query.Search) + "%"
	rows, err := s.queries.ListManagedPlaces(ctx, placesdb.ListManagedPlacesParams{Search: query.Search, Prefix: prefix, PageLimit: int32(query.Limit), PageOffset: int32(query.Offset)})
	if err != nil {
		return nil, fmt.Errorf("list place catalogue: %w", err)
	}
	result := make([]places.Entry, 0, len(rows))
	for _, row := range rows {
		entry, err := entryWithAliases(ctx, s.queries, row)
		if err != nil {
			return nil, err
		}
		result = append(result, entry)
	}
	return result, nil
}

// GetManaged returns the private entry and its names.
func (s Store) GetManaged(ctx context.Context, id string) (places.Entry, error) {
	row, err := s.queries.GetManagedPlace(ctx, id)
	if err != nil {
		return places.Entry{}, mapError(err)
	}
	return entryWithAliases(ctx, s.queries, row)
}

// CreateManaged accepts the caller transaction and never publishes automatically.
func (Store) CreateManaged(ctx context.Context, tx pgx.Tx, entry places.Entry) (places.Entry, error) {
	q := placesdb.New(tx)
	if err := q.LockPlacePublicationTree(ctx); err != nil {
		return places.Entry{}, err
	}
	row, err := q.InsertManagedPlace(ctx, placesdb.InsertManagedPlaceParams{ID: entry.ID, Slug: entry.Slug, Name: entry.Name, NormalizedName: places.NormalizeName(entry.Name), Type: entry.Type, ParentID: nullableUUID(entry.ParentID), CountryCode: entry.CountryCode, Timezone: nullableText(entry.Timezone), Latitude: nullableFloat(entry.Latitude), Longitude: nullableFloat(entry.Longitude), Currency: nullableText(entry.Currency), Languages: entry.Languages, CreatedAt: entry.CreatedAt})
	if err != nil {
		return places.Entry{}, mapError(err)
	}
	if err := replaceAliases(ctx, q, entry.ID, entry.Aliases); err != nil {
		return places.Entry{}, err
	}
	return entryWithAliases(ctx, q, row)
}

// UpdateManaged checks the version under a row lock and atomically replaces aliases.
func (Store) UpdateManaged(ctx context.Context, tx pgx.Tx, id string, version int64, details places.Details, now time.Time) (places.Entry, places.Entry, error) {
	q := placesdb.New(tx)
	before, err := lockedEntry(ctx, q, id, version)
	if err != nil {
		return places.Entry{}, places.Entry{}, err
	}
	row, err := q.UpdateManagedPlace(ctx, placesdb.UpdateManagedPlaceParams{ID: id, Name: details.Name, NormalizedName: places.NormalizeName(details.Name), Timezone: nullableText(details.Timezone), Latitude: nullableFloat(details.Latitude), Longitude: nullableFloat(details.Longitude), Currency: nullableText(details.Currency), Languages: details.Languages, UpdatedAt: now, Version: version})
	if err != nil {
		return places.Entry{}, places.Entry{}, mapError(err)
	}
	if err := replaceAliases(ctx, q, id, details.Aliases); err != nil {
		return places.Entry{}, places.Entry{}, err
	}
	after, err := entryWithAliases(ctx, q, row)
	return before, after, err
}

// PublishManaged serializes ancestor publication changes before locking the entry.
func (Store) PublishManaged(ctx context.Context, tx pgx.Tx, id string, version int64, published bool, now time.Time) (places.Entry, places.Entry, error) {
	q := placesdb.New(tx)
	if err := q.LockPlacePublicationTree(ctx); err != nil {
		return places.Entry{}, places.Entry{}, err
	}
	before, err := lockedEntry(ctx, q, id, version)
	if err != nil {
		return places.Entry{}, places.Entry{}, err
	}
	var publication pgtype.Timestamptz
	if published {
		privateAncestor, err := q.HasUnpublishedAncestor(ctx, placesdb.HasUnpublishedAncestorParams{PlaceID: id, Now: now})
		if err != nil {
			return places.Entry{}, places.Entry{}, err
		}
		if privateAncestor {
			return places.Entry{}, places.Entry{}, places.ErrConflict
		}
		if before.PublishedAt != nil {
			publication = pgtype.Timestamptz{Time: *before.PublishedAt, Valid: true}
		} else {
			publication = pgtype.Timestamptz{Time: now, Valid: true}
		}
	}
	row, err := q.SetPlacePublication(ctx, placesdb.SetPlacePublicationParams{ID: id, PublishedAt: publication, UpdatedAt: now, Version: version})
	if err != nil {
		return places.Entry{}, places.Entry{}, mapError(err)
	}
	after, err := entryWithAliases(ctx, q, row)
	return before, after, err
}
func lockedEntry(ctx context.Context, q *placesdb.Queries, id string, version int64) (places.Entry, error) {
	row, err := q.LockManagedPlace(ctx, id)
	if err != nil {
		return places.Entry{}, mapError(err)
	}
	if row.Version != version {
		return places.Entry{}, places.ErrConflict
	}
	return entryWithAliases(ctx, q, row)
}
func replaceAliases(ctx context.Context, q *placesdb.Queries, id string, aliases []places.Alias) error {
	if err := q.ClearPlaceAliases(ctx, id); err != nil {
		return err
	}
	for _, alias := range aliases {
		if err := q.InsertPlaceAlias(ctx, placesdb.InsertPlaceAliasParams{PlaceID: id, Locale: alias.Locale, Name: alias.Name, NormalizedName: places.NormalizeName(alias.Name), Preferred: alias.Preferred}); err != nil {
			return mapError(err)
		}
	}
	return nil
}
func entryWithAliases(ctx context.Context, q *placesdb.Queries, row placesdb.ModuraPlace) (places.Entry, error) {
	entry := places.Entry{ID: row.ID, Slug: row.Slug, Type: row.Type, CountryCode: row.CountryCode, Version: row.Version, CoverageLevel: int(row.CoverageLevel), CreatedAt: row.CreatedAt.UTC(), UpdatedAt: row.UpdatedAt.UTC(), Details: places.Details{Name: row.Name, Languages: row.Languages, Aliases: []places.Alias{}}}
	if entry.Languages == nil {
		entry.Languages = []string{}
	}
	if row.ParentID.Valid {
		id := uuid.UUID(row.ParentID.Bytes).String()
		entry.ParentID = &id
	}
	if row.Timezone.Valid {
		entry.Timezone = &row.Timezone.String
	}
	if row.Currency.Valid {
		entry.Currency = &row.Currency.String
	}
	if row.Latitude.Valid {
		entry.Latitude = &row.Latitude.Float64
	}
	if row.Longitude.Valid {
		entry.Longitude = &row.Longitude.Float64
	}
	if row.PublishedAt.Valid {
		timestamp := row.PublishedAt.Time.UTC()
		entry.PublishedAt = &timestamp
	}
	aliases, err := q.ListPlaceAliases(ctx, row.ID)
	if err != nil {
		return places.Entry{}, err
	}
	for _, alias := range aliases {
		entry.Aliases = append(entry.Aliases, places.Alias{Locale: alias.Locale, Name: alias.Name, Preferred: alias.Preferred})
	}
	return entry, nil
}
func nullableUUID(value *string) pgtype.UUID {
	if value == nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: uuid.MustParse(*value), Valid: true}
}
func nullableText(value *string) pgtype.Text {
	if value == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *value, Valid: true}
}
func nullableFloat(value *float64) pgtype.Float8 {
	if value == nil {
		return pgtype.Float8{}
	}
	return pgtype.Float8{Float64: *value, Valid: true}
}
func mapError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return places.ErrNotFound
	}
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) {
		switch pgError.Code {
		case "23505":
			return places.ErrConflict
		case "23503", "23514":
			return places.ErrInvalidPlace
		}
	}
	return fmt.Errorf("place persistence: %w", err)
}
