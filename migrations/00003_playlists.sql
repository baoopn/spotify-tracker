-- +goose Up
-- +goose StatementBegin

-- Where each play came from (Spotify "context"): playlist, album, artist…
ALTER TABLE listening_history
    ADD COLUMN context_type text,
    ADD COLUMN context_id   text;
CREATE INDEX listening_history_context_idx ON listening_history (context_type, context_id, played_at DESC)
    WHERE context_id IS NOT NULL;

-- Playlist metadata fetched from GET /playlists/{id}. `status` is
-- 'unavailable' when Spotify refuses the lookup (e.g. Spotify-generated
-- mixes for Development Mode apps, or private playlists without scope).
CREATE TABLE playlists (
    id              text        PRIMARY KEY,
    status          text        NOT NULL DEFAULT 'ok' CHECK (status IN ('ok', 'unavailable')),
    name            text,
    description     text,
    owner_id        text,
    owner_name      text,
    cover_image_url text,
    external_url    text,
    uri             text,
    public          boolean,
    collaborative   boolean,
    tracks_total    integer,
    snapshot_id     text,
    fetched_at      timestamptz NOT NULL DEFAULT now(),
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER playlists_updated_at BEFORE UPDATE ON playlists
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS playlists;
DROP INDEX IF EXISTS listening_history_context_idx;
ALTER TABLE listening_history DROP COLUMN IF EXISTS context_id, DROP COLUMN IF EXISTS context_type;
-- +goose StatementEnd
