-- +goose Up
-- +goose StatementBegin

-- The current (unfinished) period's top playlist is published right away and
-- kept up to date ("live"); it is finalized in place when the period ends.
-- refreshed_at is the last time a live playlist was re-ranked (or finalized).
ALTER TABLE top_playlists
    ADD COLUMN live boolean NOT NULL DEFAULT false,
    ADD COLUMN refreshed_at timestamptz;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE top_playlists DROP COLUMN IF EXISTS refreshed_at, DROP COLUMN IF EXISTS live;
-- +goose StatementEnd
