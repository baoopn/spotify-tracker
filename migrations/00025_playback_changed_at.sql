-- +goose Up
-- +goose StatementBegin
-- Spotify's timestamp of the current playback: the last state change (play,
-- pause, seek, skip). NULL when Spotify sent none, and for rows written
-- before this column. Served as changed_at in the now-playing JSON.
ALTER TABLE current_playback ADD COLUMN changed_at timestamptz;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE current_playback DROP COLUMN changed_at;
-- +goose StatementEnd
