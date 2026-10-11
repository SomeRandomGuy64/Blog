-- Profile pictures: one 256×256 JPEG per user, re-encoded server-side (no metadata).
-- updated_at doubles as the cache-busting ?v= in avatar URLs, so it only ever increases.
CREATE TABLE avatars (
	user_id    INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
	data       BLOB    NOT NULL,
	updated_at INTEGER NOT NULL
);
