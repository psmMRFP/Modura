-- name: SearchPublicPlaces :many
SELECT p.*, coalesce(a.name, p.name)::text AS display_name
FROM wheretolive.public_places p
LEFT JOIN wheretolive.place_aliases a ON a.place_id = p.id AND a.locale = sqlc.arg(locale) AND a.preferred
WHERE sqlc.arg(search)::text = ''
   OR p.normalized_name LIKE sqlc.arg(prefix)::text ESCAPE '\'
   OR p.slug LIKE sqlc.arg(prefix)::text ESCAPE '\'
   OR p.normalized_name % sqlc.arg(search)::text
   OR EXISTS (
       SELECT 1 FROM wheretolive.place_aliases alias WHERE alias.place_id = p.id
       AND (alias.normalized_name LIKE sqlc.arg(prefix)::text ESCAPE '\' OR alias.normalized_name % sqlc.arg(search)::text)
   )
ORDER BY CASE WHEN p.slug = sqlc.arg(search) OR p.normalized_name = sqlc.arg(search)
    OR EXISTS (SELECT 1 FROM wheretolive.place_aliases exact WHERE exact.place_id = p.id AND exact.normalized_name = sqlc.arg(search))
    THEN 0 ELSE 1 END, p.slug
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: GetPublicPlace :one
SELECT p.*, coalesce(a.name, p.name)::text AS display_name
FROM wheretolive.public_places p
LEFT JOIN wheretolive.place_aliases a ON a.place_id = p.id AND a.locale = sqlc.arg(locale) AND a.preferred
WHERE p.slug = sqlc.arg(slug);

-- name: ListManagedPlaces :many
SELECT p.* FROM wheretolive.places p
WHERE (sqlc.arg(search)::text = '' OR p.normalized_name LIKE sqlc.arg(prefix)::text ESCAPE '\'
   OR p.slug LIKE sqlc.arg(prefix)::text ESCAPE '\'
   OR EXISTS (SELECT 1 FROM wheretolive.place_aliases a WHERE a.place_id=p.id
       AND a.normalized_name LIKE sqlc.arg(prefix)::text ESCAPE '\'))
 AND (sqlc.arg(country_code)::text = '' OR p.country_code = sqlc.arg(country_code))
 AND (sqlc.narg(coverage_level)::smallint IS NULL OR p.coverage_level = sqlc.narg(coverage_level))
 AND (sqlc.arg(publication)::text = ''
      OR (sqlc.arg(publication) = 'draft' AND p.published_at IS NULL)
      OR (sqlc.arg(publication) = 'published' AND p.published_at IS NOT NULL))
ORDER BY p.slug LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: GetManagedPlace :one
SELECT * FROM wheretolive.places WHERE id = $1;

-- name: LockManagedPlace :one
SELECT * FROM wheretolive.places WHERE id = $1 FOR UPDATE;

-- name: ListPlaceAliases :many
SELECT locale, name, preferred FROM wheretolive.place_aliases WHERE place_id=$1 ORDER BY locale, normalized_name;

-- name: InsertManagedPlace :one
INSERT INTO wheretolive.places (id,slug,name,normalized_name,type,parent_id,country_code,timezone,latitude,longitude,currency,languages,created_at,updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$13) RETURNING *;

-- name: UpdateManagedPlace :one
UPDATE wheretolive.places SET name=$2,normalized_name=$3,timezone=$4,latitude=$5,longitude=$6,currency=$7,languages=$8,updated_at=$9,version=version+1
WHERE id=$1 AND version=$10 RETURNING *;

-- name: SetPlacePublication :one
UPDATE wheretolive.places SET published_at=$2,coverage_level=CASE WHEN $2::timestamptz IS NOT NULL THEN greatest(coverage_level,1) ELSE coverage_level END,updated_at=$3,version=version+1
WHERE id=$1 AND version=$4 RETURNING *;

-- name: ClearPlaceAliases :exec
DELETE FROM wheretolive.place_aliases WHERE place_id=$1;

-- name: InsertPlaceAlias :exec
INSERT INTO wheretolive.place_aliases (place_id,locale,name,normalized_name,preferred) VALUES ($1,$2,$3,$4,$5);

-- name: LockPlacePublicationTree :exec
SELECT pg_advisory_xact_lock(1297040470);

-- name: HasUnpublishedAncestor :one
WITH RECURSIVE ancestors AS (
  SELECT root.parent_id FROM wheretolive.places root WHERE root.id=sqlc.arg(place_id)
  UNION ALL
  SELECT p.parent_id FROM wheretolive.places p JOIN ancestors a ON p.id=a.parent_id
)
SELECT EXISTS (SELECT 1 FROM ancestors a JOIN wheretolive.places p ON p.id=a.parent_id WHERE p.published_at IS NULL OR p.published_at > sqlc.arg(now)::timestamptz);
