-- +goose Up
-- +goose StatementBegin

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TABLE users (
    id            UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
    email         TEXT        NOT NULL,
    password_hash TEXT        NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT users_email_unique UNIQUE (email)
);

CREATE TABLE refresh_sessions (
    id          UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id     UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash  TEXT        NOT NULL,
    user_agent  TEXT        NOT NULL DEFAULT '',
    ip_address  INET        NOT NULL DEFAULT '0.0.0.0',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at  TIMESTAMPTZ NOT NULL,
    revoked_at  TIMESTAMPTZ,

    CONSTRAINT refresh_sessions_token_hash_unique UNIQUE (token_hash)
);

CREATE INDEX idx_refresh_sessions_user_id ON refresh_sessions (user_id);

CREATE INDEX idx_refresh_sessions_expires_at ON refresh_sessions (expires_at)
    WHERE revoked_at IS NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_refresh_sessions_expires_at;
DROP INDEX IF EXISTS idx_refresh_sessions_user_id;
DROP TABLE IF EXISTS refresh_sessions;
DROP TABLE IF EXISTS users;

-- +goose StatementEnd