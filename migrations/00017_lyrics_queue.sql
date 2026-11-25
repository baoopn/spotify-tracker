-- +goose Up
-- +goose StatementBegin

-- The lyrics table becomes the LRCLIB lookup queue, like lyrics_translations
-- (00015). A track with no row still needs a lookup: a worker first inserts
-- a 'pending' row for it, then claims rows with a lease (claimed_by /
-- claimed_until), so API replicas never look up the same track and a crashed
-- worker's claim expires. requested_at puts a re-queued track at the front;
-- claim_requested_at keeps it while a claim holds it, so a released claim
-- stays first in line. fetched_at is NULL until the first lookup.
-- Existing rows keep their status and get no lease.
ALTER TABLE lyrics DROP CONSTRAINT lyrics_status_check;
ALTER TABLE lyrics ADD CONSTRAINT lyrics_status_check
    CHECK (status IN ('pending', 'found', 'instrumental', 'not_found', 'error'));
ALTER TABLE lyrics ALTER COLUMN fetched_at DROP NOT NULL;
ALTER TABLE lyrics ALTER COLUMN fetched_at DROP DEFAULT;
ALTER TABLE lyrics ADD COLUMN requested_at timestamptz;        -- re-queued by an admin: goes first
ALTER TABLE lyrics ADD COLUMN claim_requested_at timestamptz;  -- requested_at taken by the current claim
ALTER TABLE lyrics ADD COLUMN claimed_by text;                 -- worker holding the lease
ALTER TABLE lyrics ADD COLUMN claimed_until timestamptz;       -- lease end; NULL or past = free
CREATE INDEX lyrics_queue_idx ON lyrics (status)
    WHERE status = 'pending' OR requested_at IS NOT NULL OR claimed_until IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS lyrics_queue_idx;
DELETE FROM lyrics WHERE status = 'pending';
ALTER TABLE lyrics DROP COLUMN claimed_until;
ALTER TABLE lyrics DROP COLUMN claimed_by;
ALTER TABLE lyrics DROP COLUMN claim_requested_at;
ALTER TABLE lyrics DROP COLUMN requested_at;
UPDATE lyrics SET fetched_at = now() WHERE fetched_at IS NULL;
ALTER TABLE lyrics ALTER COLUMN fetched_at SET DEFAULT now();
ALTER TABLE lyrics ALTER COLUMN fetched_at SET NOT NULL;
ALTER TABLE lyrics DROP CONSTRAINT lyrics_status_check;
ALTER TABLE lyrics ADD CONSTRAINT lyrics_status_check
    CHECK (status IN ('found', 'instrumental', 'not_found', 'error'));
-- +goose StatementEnd
