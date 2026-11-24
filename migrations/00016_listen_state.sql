-- +goose Up
-- +goose StatementBegin

-- The listen in progress, used to count plays (tracker.Step). A single row,
-- read with SELECT ... FOR UPDATE in the same transaction that writes
-- history and current_playback, so load, decide and save are atomic.
-- track_id '' means no listen yet. 00015 is taken by lyrics translation.
CREATE TABLE listen_state (
    id           boolean     PRIMARY KEY DEFAULT true CHECK (id),
    track_id     text        NOT NULL DEFAULT '',
    started_at   timestamptz,          -- the listen's start: played_at of its history row
    progress_ms  integer,              -- last observed progress (NULL when unknown)
    listened_ms  bigint      NOT NULL DEFAULT 0,
    counted      boolean     NOT NULL DEFAULT false,
    replay       boolean     NOT NULL DEFAULT false,  -- counts as a replay of row_played_at
    row_played_at timestamptz,         -- played_at of the history row replays add to
    applied_at   timestamptz,          -- started_at of the last listen written (idempotency)
    last_seen    timestamptz,          -- fetch time of the last snapshot
    updated_at   timestamptz NOT NULL DEFAULT now()
);
INSERT INTO listen_state (id) VALUES (true);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS listen_state;
-- +goose StatementEnd
