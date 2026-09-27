CREATE TABLE users (
    id                    TEXT PRIMARY KEY,
    email                 TEXT NOT NULL,
    password_hash         TEXT NOT NULL DEFAULT '',
    created_at            TIMESTAMPTZ NOT NULL,
    updated_at            TIMESTAMPTZ NOT NULL,
    metadata              JSONB,
    email_verified_at     TIMESTAMPTZ,
    pending_email         TEXT NOT NULL DEFAULT '',
    disabled_at           TIMESTAMPTZ,
    disabled_reason       TEXT NOT NULL DEFAULT '',
    locked_until          TIMESTAMPTZ,
    failed_login_attempts INTEGER NOT NULL DEFAULT 0,
    version               BIGINT NOT NULL DEFAULT 0
);

-- Uniqueness is on the live address only. pending_email is staged and may
-- legitimately duplicate another account's live address until confirmed.
CREATE UNIQUE INDEX users_email_key ON users (email);

CREATE TABLE sessions (
    id                TEXT PRIMARY KEY,
    user_id           TEXT NOT NULL,
    token_hash        TEXT NOT NULL,
    expires_at        TIMESTAMPTZ NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL,
    authenticated_at  TIMESTAMPTZ NOT NULL,
    method            TEXT NOT NULL DEFAULT '',
    last_seen_at      TIMESTAMPTZ NOT NULL,
    idle_expires_at   TIMESTAMPTZ,
    ip                TEXT NOT NULL DEFAULT '',
    user_agent        TEXT NOT NULL DEFAULT '',
    metadata          JSONB
);

CREATE UNIQUE INDEX sessions_token_hash_key ON sessions (token_hash);
CREATE INDEX sessions_user_id_idx ON sessions (user_id);
CREATE INDEX sessions_expires_at_idx ON sessions (expires_at);

CREATE TABLE tokens (
    id          TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL DEFAULT '',
    token_hash  TEXT NOT NULL,
    purpose     TEXT NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL,
    used        BOOLEAN NOT NULL DEFAULT FALSE,
    email       TEXT NOT NULL DEFAULT '',
    nonce_hash  TEXT NOT NULL DEFAULT ''
);

CREATE UNIQUE INDEX tokens_hash_purpose_key ON tokens (token_hash, purpose);
CREATE INDEX tokens_user_purpose_idx ON tokens (user_id, purpose);
CREATE INDEX tokens_expires_at_idx ON tokens (expires_at);
