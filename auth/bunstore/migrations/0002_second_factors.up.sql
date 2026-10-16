-- Second-factor tables. A NULL active_id or pending_id means that slot is empty.
CREATE TABLE IF NOT EXISTS totp_credentials (
    user_id            TEXT PRIMARY KEY,
    active_id          TEXT,
    active_secret      TEXT,
    active_counter     BIGINT,
    active_created_at  TIMESTAMPTZ,
    pending_id         TEXT,
    pending_secret     TEXT,
    pending_created_at TIMESTAMPTZ
);
