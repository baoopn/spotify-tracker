-- +goose Up
-- +goose StatementBegin
-- "Request an invite" from /listen: a visitor leaves a name, an email and a
-- message; the admin invites or dismisses them. No IP address is stored.
-- Decided requests are deleted after 90 days.
CREATE TABLE listen_requests (
    id         bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name       text        NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
    email      text        NOT NULL CHECK (email = lower(email) AND length(email) BETWEEN 3 AND 254),
    message    text        CHECK (length(message) <= 500),
    status     text        NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'invited', 'dismissed')),
    country    text        CHECK (country ~ '^[A-Z]{2}$'),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    decided_at timestamptz,
    decided_by text,
    CHECK ((status = 'pending') = (decided_at IS NULL))
);
CREATE UNIQUE INDEX listen_requests_pending_email_idx ON listen_requests (email) WHERE status = 'pending';
CREATE INDEX listen_requests_created_idx ON listen_requests (created_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE listen_requests;
-- +goose StatementEnd
