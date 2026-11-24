-- +goose Up
-- +goose StatementBegin

-- Claude translations of found lyrics into English and Vietnamese.
-- source_hash is md5 of the lyrics that were translated (synced, 0x1f, plain):
-- when the lyrics change, the row is stale and is translated again.
-- Synced lyrics: en_lines / vi_lines hold one entry per line of the parsed LRC
-- (internal/lrc.Parse), "" for empty lines. Plain lyrics: en_text / vi_text.
-- A NULL language means the song is already in that language.
-- The table is also the work queue: a 'pending' row waits for a worker;
-- claimed_by / claimed_until is a lease so API replicas never translate the
-- same song twice, and a crashed worker's claim expires. requested_at puts a
-- re-queued song at the front.
CREATE TABLE lyrics_translations (
    track_id        text        PRIMARY KEY REFERENCES lyrics (track_id) ON DELETE CASCADE,
    status          text        NOT NULL CHECK (status IN ('pending', 'done', 'skipped', 'refused', 'error')),
    source_hash     text        NOT NULL,
    kind            text        CHECK (kind IN ('synced', 'plain')),
    source_language text,                                -- ISO 639-1, or 'und'
    en_lines        text[],
    vi_lines        text[],
    en_text         text,
    vi_text         text,
    model           text,                                -- the model that answered
    input_tokens    integer     NOT NULL DEFAULT 0,
    output_tokens   integer     NOT NULL DEFAULT 0,
    attempts        integer     NOT NULL DEFAULT 0,      -- failures in a row with this status
    last_error      text,
    translated_at   timestamptz,
    updated_at      timestamptz NOT NULL DEFAULT now(),
    next_attempt_at timestamptz,                         -- only for 'error'; NULL = given up
    requested_at    timestamptz,                         -- re-queued by an admin: goes first
    claim_requested_at timestamptz,                      -- requested_at taken by the current claim
    claimed_by      text,                                -- worker holding the lease
    claimed_until   timestamptz                          -- lease end; NULL or past = free
);
CREATE INDEX lyrics_translations_queue_idx ON lyrics_translations (status, next_attempt_at);

-- Claude calls per UTC day, for the daily and monthly caps (survives restarts).
CREATE TABLE translation_usage (
    day           date    PRIMARY KEY,
    calls         integer NOT NULL DEFAULT 0,
    input_tokens  bigint  NOT NULL DEFAULT 0,
    output_tokens bigint  NOT NULL DEFAULT 0
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS translation_usage;
DROP TABLE IF EXISTS lyrics_translations;
-- +goose StatementEnd
