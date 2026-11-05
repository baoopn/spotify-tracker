-- +goose Up
-- +goose StatementBegin

-- Artist profile images from GET /artists/{id}, filled in the background by
-- the API's artist-image refresher. image_fetched_at records every lookup
-- (also ones that found no image) so artists are not re-fetched constantly.
ALTER TABLE artists
    ADD COLUMN image_url        text,
    ADD COLUMN image_fetched_at timestamptz;
CREATE INDEX artists_image_fetched_idx ON artists (image_fetched_at NULLS FIRST);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS artists_image_fetched_idx;
ALTER TABLE artists DROP COLUMN IF EXISTS image_fetched_at, DROP COLUMN IF EXISTS image_url;
-- +goose StatementEnd
