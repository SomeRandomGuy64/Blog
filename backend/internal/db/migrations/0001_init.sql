-- All times are unix seconds (UTC). Token and session secrets are stored only as sha256 hashes.

CREATE TABLE users (
	id         INTEGER PRIMARY KEY,
	email      TEXT    NOT NULL UNIQUE COLLATE NOCASE,
	name       TEXT    NOT NULL,
	is_admin   INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL
);

CREATE TABLE login_tokens (
	hash       BLOB    PRIMARY KEY,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	next       TEXT    NOT NULL DEFAULT '/',
	created_at INTEGER NOT NULL,
	expires_at INTEGER NOT NULL,
	used_at    INTEGER
) WITHOUT ROWID;
CREATE INDEX login_tokens_user ON login_tokens(user_id);
CREATE INDEX login_tokens_expires ON login_tokens(expires_at);

CREATE TABLE sessions (
	hash       BLOB    PRIMARY KEY,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	created_at INTEGER NOT NULL,
	expires_at INTEGER NOT NULL,
	last_seen  INTEGER NOT NULL
) WITHOUT ROWID;
CREATE INDEX sessions_user ON sessions(user_id);
CREATE INDEX sessions_expires ON sessions(expires_at);

CREATE TABLE pages (
	path  TEXT PRIMARY KEY,
	title TEXT NOT NULL DEFAULT ''
) WITHOUT ROWID;

CREATE TABLE comments (
	id         INTEGER PRIMARY KEY,
	page       TEXT    NOT NULL REFERENCES pages(path),
	parent_id  INTEGER REFERENCES comments(id),
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	body       TEXT    NOT NULL,
	created_at INTEGER NOT NULL,
	deleted_at INTEGER
);
CREATE INDEX comments_page ON comments(page, created_at);
CREATE INDEX comments_parent ON comments(parent_id);
CREATE INDEX comments_user ON comments(user_id);

CREATE TABLE comment_likes (
	comment_id INTEGER NOT NULL REFERENCES comments(id) ON DELETE CASCADE,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	created_at INTEGER NOT NULL,
	PRIMARY KEY (comment_id, user_id)
) WITHOUT ROWID;
CREATE INDEX comment_likes_user ON comment_likes(user_id);

-- comment_id is the reply (kind=reply), the new comment (comment) or the liked comment (like).
CREATE TABLE notifications (
	id         INTEGER PRIMARY KEY,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	kind       TEXT    NOT NULL CHECK (kind IN ('reply', 'like', 'comment')),
	actor_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	comment_id INTEGER NOT NULL REFERENCES comments(id) ON DELETE CASCADE,
	created_at INTEGER NOT NULL,
	read_at    INTEGER
);
CREATE INDEX notifications_user ON notifications(user_id, id);
CREATE INDEX notifications_unread ON notifications(user_id) WHERE read_at IS NULL;
CREATE INDEX notifications_comment ON notifications(comment_id);
-- At most one notification per (recipient, kind, actor, comment): guards like/unlike/like churn.
CREATE UNIQUE INDEX notifications_once ON notifications(user_id, kind, actor_id, comment_id);

CREATE TABLE kyotsu_sessions (
	id           INTEGER PRIMARY KEY,
	user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	started_at   INTEGER NOT NULL,
	ended_at     INTEGER NOT NULL,
	layout       TEXT    NOT NULL,
	advanced     INTEGER NOT NULL DEFAULT 0,
	total        INTEGER NOT NULL,
	correct      INTEGER NOT NULL,
	answers_json TEXT    NOT NULL
);
CREATE INDEX kyotsu_sessions_user ON kyotsu_sessions(user_id, started_at);
