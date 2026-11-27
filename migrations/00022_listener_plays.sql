-- +goose Up
-- +goose StatementBegin
-- What each invited listener heard at /listen. One row per listener per play of a song
-- (a new row when the same song plays again after a gap). Only rows with counted = true
-- show in a history.
CREATE TABLE listener_plays (
    id            bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    listener_id   bigint      NOT NULL REFERENCES listeners (id) ON DELETE CASCADE,
    track_id      text        NOT NULL REFERENCES tracks (id) ON DELETE CASCADE,
    started_at    timestamptz NOT NULL DEFAULT now(),
    last_heard_at timestamptz NOT NULL DEFAULT now(),
    heard_ms      integer     NOT NULL DEFAULT 0 CHECK (heard_ms >= 0),
    counted       boolean     NOT NULL DEFAULT false
);
CREATE INDEX listener_plays_history_idx ON listener_plays (listener_id, started_at DESC, id DESC) WHERE counted;
CREATE INDEX listener_plays_open_idx ON listener_plays (listener_id, track_id, last_heard_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE listener_plays;
-- +goose StatementEnd
