-- IDs are 16-byte UUIDs. name and text hold crypt ciphertext, never plaintext.
-- These tables are not STRICT, so the CHECKs test the type. There are no foreign keys.
CREATE TABLE IF NOT EXISTS profiles (
    user_id BLOB NOT NULL PRIMARY KEY CHECK (typeof(user_id) = 'blob' AND length(user_id) = 16),
    name    BLOB NOT NULL CHECK (typeof(name) = 'blob')
);

CREATE TABLE IF NOT EXISTS todos (
    id         BLOB NOT NULL PRIMARY KEY CHECK (typeof(id) = 'blob' AND length(id) = 16),
    owner_id   BLOB NOT NULL CHECK (typeof(owner_id) = 'blob' AND length(owner_id) = 16),
    text       BLOB NOT NULL CHECK (typeof(text) = 'blob'),
    done       BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS todos_owner_created ON todos (owner_id, created_at, id);
