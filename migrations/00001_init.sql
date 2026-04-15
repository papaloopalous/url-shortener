-- +goose Up
-- +goose StatementBegin

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TABLE click_events (
    id          UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
    short_code  TEXT        NOT NULL,
    ip          TEXT,
    user_agent  TEXT,
    referer     TEXT,
    country     TEXT,
    clicked_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE inbox_events (
    event_id    UUID        PRIMARY KEY,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS inbox_events;
DROP TABLE IF EXISTS click_events;

-- +goose StatementEnd