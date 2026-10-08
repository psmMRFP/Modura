-- owner: places
-- Concurrent operators can edit/publish the same place: reject stale writes.
ALTER TABLE wheretolive.places ADD COLUMN version bigint NOT NULL DEFAULT 1 CHECK (version > 0);
