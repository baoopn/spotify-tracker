-- +goose Up
-- +goose StatementBegin

-- Admins can hide a published top playlist from the public page. The Spotify
-- playlist itself is left untouched.
ALTER TABLE top_playlists ADD COLUMN hidden boolean NOT NULL DEFAULT false;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE top_playlists DROP COLUMN IF EXISTS hidden;
-- +goose StatementEnd
