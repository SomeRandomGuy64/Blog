-- Roles, muting and open sign-up. Additive only: three new columns on users and one new table.
-- No table is rebuilt, so every row, id and foreign key pointing at users stays as it was.

-- role replaces is_admin: 'user', 'moderator' or 'admin'. is_admin stays (an older binary still
-- reads it after a rollback) and the code keeps it equal to role = 'admin' on every write.
ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'user' CHECK (role IN ('user', 'moderator', 'admin'));
UPDATE users SET role = 'admin' WHERE is_admin = 1;

-- Muted users can't post comments or replies (everything else still works). muted_at is when,
-- muted_by who (NULL once that account is deleted; the mute stays).
ALTER TABLE users ADD COLUMN muted_at INTEGER;
ALTER TABLE users ADD COLUMN muted_by INTEGER REFERENCES users(id) ON DELETE SET NULL;

-- Pending sign-ups: a single-use confirmation token (sha256, like login tokens) for an email
-- that has no account yet. Confirming creates the account; nothing else exists until then.
CREATE TABLE signup_tokens (
	hash       BLOB    PRIMARY KEY,
	email      TEXT    NOT NULL COLLATE NOCASE,
	name       TEXT    NOT NULL,
	next       TEXT    NOT NULL DEFAULT '/',
	created_at INTEGER NOT NULL,
	expires_at INTEGER NOT NULL,
	used_at    INTEGER
) WITHOUT ROWID;
CREATE INDEX signup_tokens_email ON signup_tokens(email);
CREATE INDEX signup_tokens_expires ON signup_tokens(expires_at);
