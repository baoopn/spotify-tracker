-- +goose Up
-- +goose StatementBegin

-- Status of the off-site backup sidecar (deploy/backup, backupctl). The
-- backup container writes these tables; the API only reads them and queues
-- requests. The database is the only channel between the two: the API never
-- holds storage credentials. Nothing secret is stored here (no storage keys,
-- no endpoint, no private key); age recipients are public keys.

-- One row per backup attempt. Old rows are deleted by backupctl (180 days).
CREATE TABLE backup_runs (
  id          bigserial PRIMARY KEY,
  trigger     text NOT NULL CHECK (trigger IN ('schedule','manual','start','retry')),
  status      text NOT NULL CHECK (status IN ('running','success','failed')),
  started_at  timestamptz NOT NULL DEFAULT now(),
  finished_at timestamptz,
  name        text,          -- object name on success
  size_bytes  bigint,
  sha256      text,
  tables      integer,
  error       text,          -- short, no secrets (<= 500 chars)
  requested_by text          -- admin email for manual runs
);
CREATE INDEX backup_runs_started_idx ON backup_runs (started_at DESC);

-- Snapshot of the remote listing, replaced after each backup/prune/list refresh.
CREATE TABLE backup_inventory (
  name        text PRIMARY KEY,
  size_bytes  bigint NOT NULL,
  created_at  timestamptz NOT NULL,   -- from the name's UTC stamp
  sha256      text,
  refreshed_at timestamptz NOT NULL DEFAULT now()
);

-- Single row (id = 1), upserted by the backup container about once a minute.
CREATE TABLE backup_agent (
  id            smallint PRIMARY KEY CHECK (id = 1),
  last_seen_at  timestamptz NOT NULL,
  version       text,
  configured    boolean NOT NULL,     -- complete config (destination + recipient)
  config_error  text,                 -- partial-config message, else NULL
  destination   text,                 -- redacted: "s3:<bucket>/<prefix>" or "remote:<scheme>/<prefix>", never endpoint/keys
  recipients    text[],               -- age public keys (public, safe), shown shortened in UI
  backup_time   text,                 -- effective HH:MM UTC
  keep_days     integer, keep_min integer, paused boolean NOT NULL DEFAULT false,
  next_run_at   timestamptz,
  settings_source text                -- 'env' | 'admin'
);

-- Work queue from the admin dashboard to the backup container. Done rows are
-- deleted by backupctl after 30 days.
CREATE TABLE backup_requests (
  id           bigserial PRIMARY KEY,
  kind         text NOT NULL CHECK (kind IN ('backup','refresh')),   -- refresh = re-list inventory
  requested_at timestamptz NOT NULL DEFAULT now(),
  requested_by text,
  claimed_at   timestamptz,
  done_at      timestamptz,
  run_id       bigint REFERENCES backup_runs(id) ON DELETE SET NULL,
  error        text
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS backup_requests;
DROP TABLE IF EXISTS backup_agent;
DROP TABLE IF EXISTS backup_inventory;
DROP TABLE IF EXISTS backup_runs;
-- +goose StatementEnd
