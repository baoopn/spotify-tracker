-- +goose Up
-- +goose StatementBegin
-- The listen-along audio files found in the bucket, one per track
-- ({S3_PREFIX}{track_id}.{ext}). Filled by the bucket sync and by audio
-- lookups. No foreign key: a file may exist for a track not in the catalog
-- yet. When a track has files with several extensions, the first in
-- LISTEN_AUDIO_EXTENSIONS order is kept.
CREATE TABLE track_audio (
    track_id      text        PRIMARY KEY,
    ext           text        NOT NULL,
    size_bytes    bigint      NOT NULL CHECK (size_bytes >= 0),
    content_type  text,
    etag          text,
    last_modified timestamptz,
    first_seen_at timestamptz NOT NULL DEFAULT now(),
    last_seen_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX track_audio_last_modified_idx ON track_audio (last_modified DESC NULLS LAST);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE track_audio;
-- +goose StatementEnd
