-- Personal API keys for scripting your own Track data (/v1/api/…, see api/apikeys.go). Additive
-- only: one new table, no changes to existing ones.
--
-- Only the SHA-256 of the key is stored (as for sessions and login tokens); prefix is the first
-- few characters ("fz_ab12") so the owner can tell keys apart. Revoking deletes the row, so a
-- revoked key stops working at once; deleting the user takes their keys with it.
-- scope: 'read' (GETs only) or 'write' (read & write). last_used_at is updated at most once a
-- minute per key.
CREATE TABLE api_keys (
	id           INTEGER PRIMARY KEY,
	user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	name         TEXT    NOT NULL,
	prefix       TEXT    NOT NULL,
	hash         BLOB    NOT NULL UNIQUE,
	scope        TEXT    NOT NULL CHECK (scope IN ('read', 'write')),
	created_at   INTEGER NOT NULL,
	last_used_at INTEGER
);
CREATE INDEX api_keys_user ON api_keys(user_id);
