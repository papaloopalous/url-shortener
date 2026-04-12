-- +goose Up
-- +goose StatementBegin

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TABLE urls (
    id          UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id     UUID        NOT NULL,
    short_code  TEXT        NOT NULL,
    long_url    TEXT        NOT NULL,
    status      TEXT        NOT NULL DEFAULT 'active',
    expires_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ,

    CONSTRAINT urls_short_code_unique UNIQUE (short_code),
    CONSTRAINT urls_status_check CHECK (status IN ('active', 'soft_deleted'))
);

CREATE TABLE outbox_events (
    id           UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
    event_type   TEXT        NOT NULL,
    payload      JSONB       NOT NULL,
    status       TEXT        NOT NULL DEFAULT 'pending',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at TIMESTAMPTZ,

    CONSTRAINT outbox_status_check CHECK (status IN ('pending', 'published'))
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS outbox_events;
DROP TABLE IF EXISTS urls;

-- +goose StatementEnd