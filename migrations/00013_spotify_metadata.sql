-- +goose Up
-- +goose StatementBegin
ALTER TABLE tracks
    ADD COLUMN track_number integer,
    ADD COLUMN disc_number  integer;
ALTER TABLE albums
    ADD COLUMN album_type             text,        -- album | single | compilation
    ADD COLUMN total_tracks           integer,
    ADD COLUMN release_date_precision text,        -- year | month | day
    ADD COLUMN upc                    text,
    ADD COLUMN copyrights             jsonb,       -- [{"text":"…","type":"C"|"P"}]; NULL = never fetched
    ADD COLUMN details_fetched_at     timestamptz; -- last GET /albums/{id} (also refused ones)
CREATE INDEX albums_details_fetched_idx ON albums (details_fetched_at NULLS FIRST);
ALTER TABLE artists
    ADD COLUMN genres            text[],           -- NULL = unknown, '{}' = Spotify lists none
    ADD COLUMN genres_fetched_at timestamptz;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
ALTER TABLE artists DROP COLUMN IF EXISTS genres_fetched_at, DROP COLUMN IF EXISTS genres;
DROP INDEX IF EXISTS albums_details_fetched_idx;
ALTER TABLE albums DROP COLUMN IF EXISTS details_fetched_at, DROP COLUMN IF EXISTS copyrights,
    DROP COLUMN IF EXISTS upc, DROP COLUMN IF EXISTS release_date_precision,
    DROP COLUMN IF EXISTS total_tracks, DROP COLUMN IF EXISTS album_type;
ALTER TABLE tracks DROP COLUMN IF EXISTS disc_number, DROP COLUMN IF EXISTS track_number;
-- +goose StatementEnd
