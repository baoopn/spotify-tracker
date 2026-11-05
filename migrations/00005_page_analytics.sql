-- +goose Up
-- +goose StatementBegin

-- Visits to the public page, aggregated per UTC hour. No cookies and no
-- personal data: unique visitors are counted in Redis from a salted hash
-- that changes every day; only coarse dimensions are stored here.
CREATE TABLE page_view_stats (
    bucket     timestamptz NOT NULL,
    path       text        NOT NULL,
    device     text        NOT NULL,           -- mobile | tablet | desktop
    browser    text        NOT NULL,           -- Chrome | Safari | Firefox | Edge | Opera | Other
    country    text        NOT NULL,           -- ISO 3166-1 alpha-2, empty when unknown
    referrer   text        NOT NULL,           -- referring host, empty for direct visits
    views      bigint      NOT NULL DEFAULT 0,
    visible_ms bigint      NOT NULL DEFAULT 0, -- total time the page was visible
    PRIMARY KEY (bucket, path, device, browser, country, referrer)
);
CREATE INDEX page_view_stats_bucket_idx ON page_view_stats (bucket DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS page_view_stats;
-- +goose StatementEnd
