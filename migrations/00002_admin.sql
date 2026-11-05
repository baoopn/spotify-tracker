-- +goose Up
-- +goose StatementBegin

-- Typed application settings editable from the admin dashboard. Secret
-- fields inside `value` are encrypted by the application (AES-256-GCM).
CREATE TABLE settings (
    key        text        PRIMARY KEY,
    value      jsonb       NOT NULL,
    updated_by text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER settings_updated_at BEFORE UPDATE ON settings
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE admin_audit (
    id     bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    at     timestamptz NOT NULL DEFAULT now(),
    actor  text        NOT NULL,
    action text        NOT NULL,
    detail jsonb,
    ip     text
);
CREATE INDEX admin_audit_at_idx ON admin_audit (at DESC);

-- Hourly aggregates of public API traffic. `latency_buckets` holds counts
-- for the upper bounds (ms) 5,10,25,50,100,250,500,1000,2500,5000,+Inf.
CREATE TABLE api_request_stats (
    bucket          timestamptz NOT NULL,
    route           text        NOT NULL,
    method          text        NOT NULL,
    status          smallint    NOT NULL,
    count           bigint      NOT NULL DEFAULT 0,
    duration_ms_sum double precision NOT NULL DEFAULT 0,
    duration_ms_max double precision NOT NULL DEFAULT 0,
    latency_buckets bigint[]    NOT NULL,
    PRIMARY KEY (bucket, route, method, status)
);
CREATE INDEX api_request_stats_bucket_idx ON api_request_stats (bucket DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS api_request_stats;
DROP TABLE IF EXISTS admin_audit;
DROP TABLE IF EXISTS settings;
-- +goose StatementEnd
