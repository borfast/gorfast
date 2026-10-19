-- IDs are 16-byte UUIDs. These tables are not STRICT, so the CHECKs test the type as well
-- as the length. A NULL tokens.user_id means the token has no user.

-- Uniqueness is on the live address only, and case-insensitive via
-- COLLATE NOCASE: a caller that bypasses sulis's own lowercase
-- normalization cannot create "Someone@Example.test" alongside
-- "someone@example.test". pending_email is staged and may legitimately
-- duplicate another account's live address until confirmed.
CREATE TABLE IF NOT EXISTS users (
    id                    BLOB NOT NULL PRIMARY KEY CHECK (typeof(id) = 'blob' AND length(id) = 16),
    email                 TEXT NOT NULL UNIQUE COLLATE NOCASE,
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

CREATE TABLE IF NOT EXISTS sessions (
    id                BLOB NOT NULL PRIMARY KEY CHECK (typeof(id) = 'blob' AND length(id) = 16),
    user_id           BLOB NOT NULL CHECK (typeof(user_id) = 'blob' AND length(user_id) = 16),
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

CREATE UNIQUE INDEX IF NOT EXISTS sessions_token_hash_key ON sessions (token_hash);
CREATE INDEX IF NOT EXISTS sessions_user_id_idx ON sessions (user_id);
CREATE INDEX IF NOT EXISTS sessions_expires_at_idx ON sessions (expires_at);

CREATE TABLE IF NOT EXISTS tokens (
    id          BLOB NOT NULL PRIMARY KEY CHECK (typeof(id) = 'blob' AND length(id) = 16),
    user_id     BLOB CHECK (user_id IS NULL OR (typeof(user_id) = 'blob' AND length(user_id) = 16)),
    token_hash  TEXT NOT NULL,
    purpose     TEXT NOT NULL,
    expires_at  TIMESTAMP NOT NULL,
    created_at  TIMESTAMP NOT NULL,
    used        BOOLEAN NOT NULL DEFAULT 0,
    email       TEXT NOT NULL DEFAULT '',
    nonce_hash  TEXT NOT NULL DEFAULT ''
);

CREATE UNIQUE INDEX IF NOT EXISTS tokens_hash_purpose_key ON tokens (token_hash, purpose);
CREATE INDEX IF NOT EXISTS tokens_user_purpose_idx ON tokens (user_id, purpose);
CREATE INDEX IF NOT EXISTS tokens_expires_at_idx ON tokens (expires_at);
