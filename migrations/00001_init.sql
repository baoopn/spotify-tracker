-- +goose Up
-- +goose StatementBegin

CREATE OR REPLACE FUNCTION set_updated_at() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    NEW.updated_at := now();
    RETURN NEW;
END;
$$;

-- ─── Catalog ──────────────────────────────────────────────────────────────────
-- Spotify IDs are used as primary keys.

CREATE TABLE artists (
    id           text        PRIMARY KEY,
    name         text        NOT NULL,
    uri          text,
    external_url text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE albums (
    id              text        PRIMARY KEY,
    name            text        NOT NULL,
    release_date    text,       -- Spotify precision varies: YYYY, YYYY-MM or YYYY-MM-DD
    cover_image_url text,
    uri             text,
    external_url    text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE tracks (
    id           text        PRIMARY KEY,
    name         text        NOT NULL,
    uri          text,
    isrc         text,
    duration_ms  integer     NOT NULL CHECK (duration_ms >= 0),
    explicit     boolean     NOT NULL DEFAULT false,
    external_url text,
    album_id     text        REFERENCES albums (id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX tracks_album_id_idx ON tracks (album_id);
CREATE INDEX tracks_isrc_idx ON tracks (isrc) WHERE isrc IS NOT NULL;

CREATE TABLE track_artists (
    track_id  text     NOT NULL REFERENCES tracks (id) ON DELETE CASCADE,
    artist_id text     NOT NULL REFERENCES artists (id) ON DELETE CASCADE,
    position  smallint NOT NULL DEFAULT 0,
    PRIMARY KEY (track_id, artist_id)
);
CREATE INDEX track_artists_artist_id_idx ON track_artists (artist_id);

-- ─── Listening activity ───────────────────────────────────────────────────────

-- Singleton row holding the latest playback state.
CREATE TABLE current_playback (
    id          boolean     PRIMARY KEY DEFAULT true CHECK (id),
    track_id    text        REFERENCES tracks (id) ON DELETE SET NULL,
    is_playing  boolean     NOT NULL DEFAULT false,
    progress_ms integer,
    fetched_at  timestamptz NOT NULL,
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE listening_history (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    track_id     text        NOT NULL REFERENCES tracks (id) ON DELETE CASCADE,
    played_at    timestamptz NOT NULL,
    duration_ms  integer,
    replay_count integer     NOT NULL DEFAULT 1 CHECK (replay_count >= 1),
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT listening_history_track_played_uniq UNIQUE (track_id, played_at)
);
CREATE INDEX listening_history_played_at_idx ON listening_history (played_at DESC);

-- ─── Enrichment ───────────────────────────────────────────────────────────────

CREATE TABLE track_features (
    track_id         text        PRIMARY KEY REFERENCES tracks (id) ON DELETE CASCADE,
    isrc             text,
    status           text        NOT NULL DEFAULT 'pending'
                                 CHECK (status IN ('pending', 'enriched', 'unavailable', 'failed')),
    source           text,
    acousticness     double precision,
    danceability     double precision,
    energy           double precision,
    instrumentalness double precision,
    key              smallint CHECK (key BETWEEN -1 AND 11),
    liveness         double precision,
    loudness         double precision,
    mode             smallint CHECK (mode IN (0, 1)),
    speechiness      double precision,
    tempo            double precision,
    time_signature   smallint,
    valence          double precision,
    genres           jsonb,      -- [{"root": "pop", "sub": ["dance pop"]}]
    attempts         integer     NOT NULL DEFAULT 0,
    last_error       text,
    next_attempt_at  timestamptz NOT NULL DEFAULT now(),
    enriched_at      timestamptz,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX track_features_pending_idx ON track_features (next_attempt_at)
    WHERE status = 'pending';

CREATE TRIGGER artists_updated_at BEFORE UPDATE ON artists
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER albums_updated_at BEFORE UPDATE ON albums
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER tracks_updated_at BEFORE UPDATE ON tracks
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER current_playback_updated_at BEFORE UPDATE ON current_playback
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER track_features_updated_at BEFORE UPDATE ON track_features
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS track_features;
DROP TABLE IF EXISTS listening_history;
DROP TABLE IF EXISTS current_playback;
DROP TABLE IF EXISTS track_artists;
DROP TABLE IF EXISTS tracks;
DROP TABLE IF EXISTS albums;
DROP TABLE IF EXISTS artists;
DROP FUNCTION IF EXISTS set_updated_at();
-- +goose StatementEnd
