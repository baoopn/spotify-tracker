-- +goose Up
-- +goose StatementBegin

-- Lyrics the admin adds by hand (source 'manual'). Automatic LRCLIB lookups
-- (backfill, retries, now-playing and prefetch wakes) never claim or
-- overwrite a manual row. replace_manual is set by an explicit re-queue
-- with replace_manual: the next LRCLIB result then replaces the manual
-- lyrics. manual_at / manual_by record the last manual edit.
-- Existing rows are all LRCLIB lookups: they keep source 'lrclib' and get
-- no manual audit (the UPDATE is a guard and changes nothing in practice).
UPDATE lyrics SET source = 'lrclib' WHERE source IS DISTINCT FROM 'lrclib';
ALTER TABLE lyrics ADD COLUMN manual_at timestamptz;                       -- last manual edit
ALTER TABLE lyrics ADD COLUMN manual_by text;                              -- who made it (admin email or "mcp")
ALTER TABLE lyrics ADD COLUMN replace_manual boolean NOT NULL DEFAULT false; -- next LRCLIB result may replace it
ALTER TABLE lyrics ADD CONSTRAINT lyrics_source_check CHECK (source IN ('lrclib', 'manual'));
ALTER TABLE lyrics ADD CONSTRAINT lyrics_manual_check
    CHECK ((source = 'manual') = (manual_at IS NOT NULL) AND (source = 'manual' OR NOT replace_manual));

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Manual lyrics stay as ordinary LRCLIB-sourced rows (a later lookup may
-- replace them).
ALTER TABLE lyrics DROP CONSTRAINT IF EXISTS lyrics_manual_check;
ALTER TABLE lyrics DROP CONSTRAINT IF EXISTS lyrics_source_check;
UPDATE lyrics SET source = 'lrclib' WHERE source = 'manual';
ALTER TABLE lyrics DROP COLUMN replace_manual;
ALTER TABLE lyrics DROP COLUMN manual_by;
ALTER TABLE lyrics DROP COLUMN manual_at;
-- +goose StatementEnd
