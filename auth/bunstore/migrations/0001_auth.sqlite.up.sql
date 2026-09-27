CREATE TABLE users (
    id                    TEXT PRIMARY KEY,
    email                 TEXT NOT NULL,
    password_hash         TEXT NOT NULL DEFAULT '',
    created_at            TIMESTAMP NOT NULL,
    updated_at            TIMESTAMP NOT NULL,
    metadata              TEXT,
    email_verified_at     TIMESTAMP,
    pending_email         TEXT NOT NULL DEFAULT '',
    disabled_at           TIMESTAMP,
    disabled_reason       TEXT NOT NULL DEFAULT '',
    locked_until          TIMESTAMP,
    failed_login_attempts INTEGER NOT NULL DEFAULT 0,
    version               INTEGER NOT NULL DEFAULT 0
);

-- Uniqueness is on the live address only. pending_email is staged and may
-- legitimately duplicate another account's live address until confirmed.
CREATE UNIQUE INDEX users_email_key ON users (email);

CREATE TABLE sessions (
    id                TEXT PRIMARY KEY,
    user_id           TEXT NOT NULL,
    token_hash        TEXT NOT NULL,
    expires_at        TIMESTAMP NOT NULL,
    created_at        TIMESTAMP NOT NULL,
    authenticated_at  TIMESTAMP NOT NULL,
    method            TEXT NOT NULL DEFAULT '',
    last_seen_at      TIMESTAMP NOT NULL,
    idle_expires_at   TIMESTAMP,
    ip                TEXT NOT NULL DEFAULT '',
    user_agent        TEXT NOT NULL DEFAULT '',
    metadata          TEXT
);

CREATE UNIQUE INDEX sessions_token_hash_key ON sessions (token_hash);
CREATE INDEX sessions_user_id_idx ON sessions (user_id);
CREATE INDEX sessions_expires_at_idx ON sessions (expires_at);

CREATE TABLE tokens (
    id          TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL DEFAULT '',
    token_hash  TEXT NOT NULL,
    purpose     TEXT NOT NULL,
    expires_at  TIMESTAMP NOT NULL,
    created_at  TIMESTAMP NOT NULL,
    used        BOOLEAN NOT NULL DEFAULT 0,
    email       TEXT NOT NULL DEFAULT '',
    nonce_hash  TEXT NOT NULL DEFAULT ''
);

CREATE UNIQUE INDEX tokens_hash_purpose_key ON tokens (token_hash, purpose);
CREATE INDEX tokens_user_purpose_idx ON tokens (user_id, purpose);
CREATE INDEX tokens_expires_at_idx ON tokens (expires_at);
