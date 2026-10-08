-- owner: places
DROP VIEW wheretolive.public_places;
DROP TABLE wheretolive.place_aliases;
DROP TABLE wheretolive.places;
DROP FUNCTION wheretolive.validate_place_hierarchy();
-- Keep pg_trgm: other applications may use the shared extension.
