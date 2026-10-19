-- Second-factor tables. A NULL active_id or pending_id means that slot is empty.
CREATE TABLE IF NOT EXISTS totp_credentials (
    user_id            UUID PRIMARY KEY,
    active_id          UUID,
    active_secret      TEXT,
    active_counter     BIGINT,
    active_created_at  TIMESTAMPTZ,
    pending_id         UUID,
    pending_secret     TEXT,
    pending_created_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS passkey_credentials (
    id               UUID PRIMARY KEY,
    user_id          UUID NOT NULL,
    credential_id    BYTEA NOT NULL,
    public_key       BYTEA NOT NULL,
    attestation_type TEXT NOT NULL DEFAULT '',
    aaguid           BYTEA,
    sign_count       BIGINT NOT NULL DEFAULT 0,
    name             TEXT NOT NULL DEFAULT '',
    transports       JSONB,
    backup_eligible  BOOLEAN NOT NULL DEFAULT FALSE,
    backup_state     BOOLEAN NOT NULL DEFAULT FALSE,
    last_used_at     TIMESTAMPTZ,
    discoverable     BOOLEAN NOT NULL DEFAULT FALSE,
    created_at       TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS passkey_credentials_credential_id_key ON passkey_credentials (credential_id);
CREATE INDEX IF NOT EXISTS passkey_credentials_user_id_idx ON passkey_credentials (user_id);

CREATE TABLE IF NOT EXISTS passkey_challenges (
    challenge_key TEXT PRIMARY KEY,
    session_data  BYTEA NOT NULL,
    expires_at    TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS passkey_challenges_expires_at_idx ON passkey_challenges (expires_at);

CREATE TABLE IF NOT EXISTS recovery_code_sets (
    user_id     UUID PRIMARY KEY,
    replaced_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS recovery_codes (
    user_id   UUID NOT NULL,
    code_hash TEXT NOT NULL,
    PRIMARY KEY (user_id, code_hash)
);
