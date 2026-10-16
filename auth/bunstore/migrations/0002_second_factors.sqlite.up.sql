-- Second-factor tables. A NULL active_id or pending_id means that slot is empty.
CREATE TABLE IF NOT EXISTS totp_credentials (
    user_id            TEXT PRIMARY KEY,
    active_id          TEXT,
    active_secret      TEXT,
    active_counter     INTEGER,
    active_created_at  TIMESTAMP,
    pending_id         TEXT,
    pending_secret     TEXT,
    pending_created_at TIMESTAMP
);

CREATE TABLE IF NOT EXISTS passkey_credentials (
    id               TEXT PRIMARY KEY,
    user_id          TEXT NOT NULL,
    credential_id    BLOB NOT NULL,
    public_key       BLOB NOT NULL,
    attestation_type TEXT NOT NULL DEFAULT '',
    aaguid           BLOB,
    sign_count       INTEGER NOT NULL DEFAULT 0,
    name             TEXT NOT NULL DEFAULT '',
    transports       TEXT,
    backup_eligible  BOOLEAN NOT NULL DEFAULT 0,
    backup_state     BOOLEAN NOT NULL DEFAULT 0,
    last_used_at     TIMESTAMP,
    discoverable     BOOLEAN NOT NULL DEFAULT 0,
    created_at       TIMESTAMP NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS passkey_credentials_credential_id_key ON passkey_credentials (credential_id);
CREATE INDEX IF NOT EXISTS passkey_credentials_user_id_idx ON passkey_credentials (user_id);

CREATE TABLE IF NOT EXISTS passkey_challenges (
    challenge_key TEXT PRIMARY KEY,
    session_data  BLOB NOT NULL,
    expires_at    TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS passkey_challenges_expires_at_idx ON passkey_challenges (expires_at);

CREATE TABLE IF NOT EXISTS recovery_code_sets (
    user_id     TEXT PRIMARY KEY,
    replaced_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS recovery_codes (
    user_id   TEXT NOT NULL,
    code_hash TEXT NOT NULL,
    PRIMARY KEY (user_id, code_hash)
);
