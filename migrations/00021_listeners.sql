-- +goose Up
-- +goose StatementBegin
-- People invited to listen along at /listen (Clerk invitations). This table, not Clerk's
-- public_metadata.role, decides access: revoking a row cuts access at once. A revoked row is kept
-- as history; inviting the same email again creates a new row.
CREATE TABLE listeners (
    id                  bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    email               text        NOT NULL CHECK (email = lower(email) AND length(email) BETWEEN 3 AND 254),
    status              text        NOT NULL DEFAULT 'invited' CHECK (status IN ('invited', 'active', 'revoked')),
    clerk_user_id       text,
    clerk_invitation_id text,
    invited_by          text        NOT NULL,
    invited_at          timestamptz NOT NULL DEFAULT now(),
    accepted_at         timestamptz,
    revoked_at          timestamptz,
    last_seen_at        timestamptz,
    CHECK ((status = 'revoked') = (revoked_at IS NOT NULL)),
    CHECK (status <> 'active' OR (clerk_user_id IS NOT NULL AND accepted_at IS NOT NULL))
);
CREATE UNIQUE INDEX listeners_email_live_idx ON listeners (email) WHERE status <> 'revoked';
CREATE UNIQUE INDEX listeners_clerk_user_live_idx ON listeners (clerk_user_id)
    WHERE status <> 'revoked' AND clerk_user_id IS NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE listeners;
-- +goose StatementEnd
