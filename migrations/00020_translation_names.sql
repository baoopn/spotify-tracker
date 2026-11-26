-- +goose Up
-- +goose StatementBegin

-- The song title and album name translated into English and Vietnamese, in
-- the same Claude call as the lyrics. NULL when the name stays as written
-- (already in that language, or only a name), when there is no album, and
-- for translations made before these columns (no backfill: re-queue a song
-- to translate it again, it costs API credit).
ALTER TABLE lyrics_translations
	ADD COLUMN title_en text,
	ADD COLUMN title_vi text,
	ADD COLUMN album_en text,
	ADD COLUMN album_vi text;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE lyrics_translations
	DROP COLUMN title_en,
	DROP COLUMN title_vi,
	DROP COLUMN album_en,
	DROP COLUMN album_vi;
-- +goose StatementEnd
