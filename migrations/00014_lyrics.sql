-- +goose Up
-- +goose StatementBegin

-- Lyrics from LRCLIB (admin and MCP only; never served by /api/v1).
-- search_tsv folds accents and case with search_fold (00010): no extension.
-- A later change to search_fold must also recompute the stored vectors
-- (UPDATE lyrics SET plain_lyrics = plain_lyrics).
CREATE TABLE lyrics (
    track_id        text        PRIMARY KEY REFERENCES tracks (id) ON DELETE CASCADE,
    status          text        NOT NULL CHECK (status IN ('found', 'instrumental', 'not_found', 'error')),
    source          text        NOT NULL DEFAULT 'lrclib',
    lrclib_id       bigint,
    plain_lyrics    text,
    synced_lyrics   text,                                -- LRC: "[mm:ss.xx] line"
    attempts        integer     NOT NULL DEFAULT 0,      -- failed lookups since the last success
    last_error      text,
    fetched_at      timestamptz NOT NULL DEFAULT now(),  -- last lookup
    next_attempt_at timestamptz,                         -- NULL for found / instrumental (terminal)
    search_tsv      tsvector GENERATED ALWAYS AS
                    (to_tsvector('simple', search_fold(COALESCE(plain_lyrics, '')))) STORED
);
CREATE INDEX lyrics_search_idx ON lyrics USING gin (search_tsv);
CREATE INDEX lyrics_retry_idx ON lyrics (next_attempt_at) WHERE status IN ('not_found', 'error');

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS lyrics;
-- +goose StatementEnd
