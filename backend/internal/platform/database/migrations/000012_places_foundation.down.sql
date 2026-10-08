-- owner: places
DROP VIEW modura.public_places;
DROP TABLE modura.place_aliases;
DROP TABLE modura.places;
DROP FUNCTION modura.validate_place_hierarchy();
-- Keep pg_trgm: other applications may use the shared extension.
