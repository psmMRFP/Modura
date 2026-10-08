-- owner: places
-- Concurrent operators can edit/publish the same place: reject stale writes.
ALTER TABLE modura.places ADD COLUMN version bigint NOT NULL DEFAULT 1 CHECK (version > 0);
