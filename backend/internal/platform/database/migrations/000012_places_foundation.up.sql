-- owner: places
-- All tables in this migration are explicitly global/platform-owned (ADR 0005).
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE modura.places (
    id uuid PRIMARY KEY CHECK (substring(id::text from 15 for 1) = '7' AND substring(id::text from 20 for 1) IN ('8', '9', 'a', 'b')),
    slug text NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$' AND length(slug) <= 120),
    name text NOT NULL CHECK (btrim(name) <> '' AND length(name) <= 200),
    normalized_name text NOT NULL CHECK (btrim(normalized_name) <> ''),
    type text NOT NULL CHECK (type IN ('country', 'region', 'island', 'city', 'district')),
    parent_id uuid,
    country_code text NOT NULL CHECK (country_code ~ '^[A-Z]{2}$'),
    timezone text,
    latitude double precision CHECK (latitude BETWEEN -90 AND 90),
    longitude double precision CHECK (longitude BETWEEN -180 AND 180),
    currency text CHECK (currency ~ '^[A-Z]{3}$'),
    languages text[] NOT NULL DEFAULT '{}',
    coverage_level smallint NOT NULL DEFAULT 0 CHECK (coverage_level BETWEEN 0 AND 3),
    published_at timestamptz,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (id, country_code),
    FOREIGN KEY (parent_id, country_code) REFERENCES modura.places (id, country_code),
    CHECK ((type = 'country') = (parent_id IS NULL)),
    CHECK (parent_id IS NULL OR parent_id <> id),
    CHECK ((latitude IS NULL) = (longitude IS NULL)),
    CHECK (timezone IS NULL OR btrim(timezone) <> ''),
    CHECK (published_at IS NULL OR coverage_level > 0)
);
CREATE UNIQUE INDEX places_country_unique ON modura.places (country_code) WHERE type = 'country';
CREATE INDEX places_name_trgm ON modura.places USING gin (normalized_name gin_trgm_ops);
CREATE INDEX places_parent ON modura.places (parent_id);

-- Strict type ordering makes cycles impossible, including concurrent changes.
-- Parent and child rows are locked/checked on type changes to preserve the graph.
CREATE FUNCTION modura.validate_place_hierarchy() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE parent_type text;
BEGIN
    -- Serialize structural changes, not public reads, across the small place tree.
    PERFORM pg_advisory_xact_lock(1297040470);
    IF NEW.parent_id IS NOT NULL THEN
        SELECT type INTO parent_type FROM modura.places WHERE id = NEW.parent_id FOR SHARE;
        IF parent_type IS NULL OR NOT (
            (NEW.type = 'region' AND parent_type = 'country') OR
            (NEW.type = 'island' AND parent_type IN ('country', 'region')) OR
            (NEW.type = 'city' AND parent_type IN ('country', 'region', 'island')) OR
            (NEW.type = 'district' AND parent_type = 'city')
        ) THEN
            RAISE EXCEPTION 'invalid place parent type' USING ERRCODE = '23514';
        END IF;
    END IF;
    IF EXISTS (
        SELECT 1 FROM modura.places child WHERE child.parent_id = NEW.id AND NOT (
            (child.type = 'region' AND NEW.type = 'country') OR
            (child.type = 'island' AND NEW.type IN ('country', 'region')) OR
            (child.type = 'city' AND NEW.type IN ('country', 'region', 'island')) OR
            (child.type = 'district' AND NEW.type = 'city')
        )
    ) THEN
        RAISE EXCEPTION 'invalid place child type' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER place_hierarchy BEFORE INSERT OR UPDATE OF type, parent_id ON modura.places
    FOR EACH ROW EXECUTE FUNCTION modura.validate_place_hierarchy();

CREATE TABLE modura.place_aliases (
    place_id uuid NOT NULL REFERENCES modura.places(id) ON DELETE CASCADE,
    locale text NOT NULL CHECK (locale ~ '^[A-Za-z]{2,8}(-[A-Za-z0-9]{1,8})*$'),
    name text NOT NULL CHECK (btrim(name) <> '' AND length(name) <= 200),
    normalized_name text NOT NULL CHECK (btrim(normalized_name) <> ''),
    preferred boolean NOT NULL DEFAULT false,
    PRIMARY KEY (place_id, locale, normalized_name)
);
CREATE UNIQUE INDEX place_alias_preferred ON modura.place_aliases (place_id, locale) WHERE preferred;
CREATE INDEX place_alias_name_trgm ON modura.place_aliases USING gin (normalized_name gin_trgm_ops);

-- Even a published child stays private if any ancestor is unpublished.
CREATE VIEW modura.public_places AS
WITH RECURSIVE visible AS (
    SELECT p.* FROM modura.places p WHERE parent_id IS NULL AND published_at <= now()
    UNION ALL
    SELECT p.* FROM modura.places p JOIN visible parent ON parent.id = p.parent_id
    WHERE p.published_at <= now()
)
SELECT * FROM visible;
