-- +goose Up
-- +goose StatementBegin

-- Auto-generated "Top 50" Spotify playlists, one per completed week, month
-- and year (period boundaries in TOP_PLAYLISTS_TIME_ZONE). The row is the
-- generator's state machine: pending → created | empty, with waiting_auth
-- (missing playlist-modify-public scope) and failed (retried with backoff).
CREATE TABLE top_playlists (
    period              text        NOT NULL CHECK (period IN ('week', 'month', 'year')),
    period_start        timestamptz NOT NULL,
    period_end          timestamptz NOT NULL,
    time_zone           text        NOT NULL,
    name                text        NOT NULL,
    status              text        NOT NULL CHECK (status IN ('pending', 'created', 'empty', 'waiting_auth', 'failed')),
    spotify_playlist_id text,
    spotify_url         text,
    track_count         integer     NOT NULL DEFAULT 0,
    total_plays         bigint      NOT NULL DEFAULT 0,
    attempts            integer     NOT NULL DEFAULT 0,
    last_error          text,
    next_attempt_at     timestamptz,
    published_at        timestamptz,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (period, period_start)
);
CREATE TRIGGER top_playlists_updated_at BEFORE UPDATE ON top_playlists
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE INDEX top_playlists_published_idx ON top_playlists (period, period_start DESC) WHERE status = 'created';

-- Snapshot of the ranked tracks a playlist was generated from.
CREATE TABLE top_playlist_tracks (
    period       text        NOT NULL,
    period_start timestamptz NOT NULL,
    position     smallint    NOT NULL CHECK (position >= 1),
    track_id     text        NOT NULL REFERENCES tracks (id) ON DELETE CASCADE,
    plays        bigint      NOT NULL,
    PRIMARY KEY (period, period_start, position),
    FOREIGN KEY (period, period_start) REFERENCES top_playlists (period, period_start) ON DELETE CASCADE
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS top_playlist_tracks;
DROP TABLE IF EXISTS top_playlists;
-- +goose StatementEnd
