-- IDs are native UUIDs. name and text hold crypt ciphertext, never plaintext.
-- There are no foreign keys, as in the auth tables.
CREATE TABLE IF NOT EXISTS profiles (
    user_id UUID PRIMARY KEY,
    name    BYTEA NOT NULL
);

CREATE TABLE IF NOT EXISTS todos (
    id         UUID PRIMARY KEY,
    owner_id   UUID NOT NULL,
    text       BYTEA NOT NULL,
    done       BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS todos_owner_created ON todos (owner_id, created_at, id);
