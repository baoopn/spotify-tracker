-- +goose Up
-- +goose StatementBegin

-- Every language in a song's lyrics (ISO 639-1, main first), as Claude
-- reported it. source_language stays the main one. NULL for translations
-- made before this column: re-queue a song to translate it again with the
-- per-language prompt (no backfill, it costs API credit).
-- Numbered 00019 on purpose: 00018 is taken by the manual lyrics migration
-- (feat/own-lyrics). If this one merges first, renumber so goose applies
-- them in order (no allow-missing).
ALTER TABLE lyrics_translations ADD COLUMN languages text[];
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE lyrics_translations DROP COLUMN languages;
-- +goose StatementEnd
