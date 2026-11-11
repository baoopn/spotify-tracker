-- +goose Up
-- +goose StatementBegin

-- postponed_since is the first transient provider failure (rate limit) of the
-- current run: such failures push next_attempt_at back without counting an
-- attempt, but only for 24 hours from here, then they count again. Cleared by
-- a counted failure, a result and a re-queue.
ALTER TABLE track_features ADD COLUMN postponed_since timestamptz;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE track_features DROP COLUMN IF EXISTS postponed_since;
-- +goose StatementEnd
