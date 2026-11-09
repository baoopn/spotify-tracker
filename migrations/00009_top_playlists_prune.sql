-- +goose Up
-- +goose StatementBegin

-- Only the newest TOP_PLAYLISTS_KEEP playlists per kind are kept. Older ones
-- are pruned: removed from Spotify and from the public page, kept here as
-- history. spotify_removed is set once Spotify confirmed the removal.
-- verified_at is the last check that the playlist is still in the owner's
-- library; restored_at is the last time it was restored after being
-- deleted outside this system.
ALTER TABLE top_playlists DROP CONSTRAINT IF EXISTS top_playlists_status_check;
ALTER TABLE top_playlists ADD CONSTRAINT top_playlists_status_check
    CHECK (status IN ('pending', 'created', 'empty', 'waiting_auth', 'failed', 'pruned'));
ALTER TABLE top_playlists
    ADD COLUMN pruned_at timestamptz,
    ADD COLUMN spotify_removed boolean NOT NULL DEFAULT false,
    ADD COLUMN verified_at timestamptz,
    ADD COLUMN restored_at timestamptz;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Pruned playlists are gone from Spotify: never turn them back into
-- published ones. 'empty' keeps them unpublished and skipped by the generator.
UPDATE top_playlists SET status = 'empty' WHERE status = 'pruned';
ALTER TABLE top_playlists DROP CONSTRAINT IF EXISTS top_playlists_status_check;
ALTER TABLE top_playlists ADD CONSTRAINT top_playlists_status_check
    CHECK (status IN ('pending', 'created', 'empty', 'waiting_auth', 'failed'));
ALTER TABLE top_playlists
    DROP COLUMN IF EXISTS restored_at,
    DROP COLUMN IF EXISTS verified_at,
    DROP COLUMN IF EXISTS spotify_removed,
    DROP COLUMN IF EXISTS pruned_at;
-- +goose StatementEnd
