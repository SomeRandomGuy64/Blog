-- Article likes (api/pagelikes.go). Every article in the site's comment-pages index can be liked,
-- once per account; each like from someone else is a point (XP) for the article author, the
-- FAZOND_ADMIN_EMAIL account.
--
-- page_likes is new. page refers to pages(path) like comments.page does (the API upserts the
-- row with the published title before inserting the like).
CREATE TABLE page_likes (
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	page       TEXT    NOT NULL REFERENCES pages(path),
	created_at INTEGER NOT NULL,
	PRIMARY KEY (user_id, page)
) WITHOUT ROWID;
CREATE INDEX page_likes_page ON page_likes(page);

-- A like on an article notifies its author ('page_like'), like a comment like does. Such a
-- notification has a page and no comment, which the old notifications table can't hold
-- (comment_id NOT NULL, a CHECK on kind), so it is rebuilt with:
--   kind       one more value, 'page_like'
--   comment_id NULL exactly for 'page_like'
--   page       new, last column: the liked article, set exactly for 'page_like'
-- Nothing refers to notifications, so the rebuild is a plain copy: every row keeps its id,
-- read state and timestamps, and the indexes are recreated with the same names and
-- definitions (plus one for the new kind). Rows whose user, actor or comment no longer exists
-- (impossible with foreign keys on, which they always are) are dropped rather than abort the
-- migration.
CREATE TABLE notifications_new (
	id         INTEGER PRIMARY KEY,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	kind       TEXT    NOT NULL CHECK (kind IN ('reply', 'like', 'comment', 'page_like')),
	actor_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	comment_id INTEGER REFERENCES comments(id) ON DELETE CASCADE,
	created_at INTEGER NOT NULL,
	read_at    INTEGER,
	page       TEXT    REFERENCES pages(path),
	CHECK ((kind = 'page_like') = (comment_id IS NULL)),
	CHECK ((kind = 'page_like') = (page IS NOT NULL))
);
INSERT INTO notifications_new (id, user_id, kind, actor_id, comment_id, created_at, read_at)
	SELECT n.id, n.user_id, n.kind, n.actor_id, n.comment_id, n.created_at, n.read_at
	FROM notifications n
	WHERE EXISTS (SELECT 1 FROM users u WHERE u.id = n.user_id)
		AND EXISTS (SELECT 1 FROM users a WHERE a.id = n.actor_id)
		AND EXISTS (SELECT 1 FROM comments c WHERE c.id = n.comment_id);
DROP TABLE notifications;
ALTER TABLE notifications_new RENAME TO notifications;
CREATE INDEX notifications_user ON notifications(user_id, id);
CREATE INDEX notifications_unread ON notifications(user_id) WHERE read_at IS NULL;
CREATE INDEX notifications_comment ON notifications(comment_id);
-- At most one notification per (recipient, kind, actor, comment): guards like/unlike/like churn.
CREATE UNIQUE INDEX notifications_once ON notifications(user_id, kind, actor_id, comment_id);
-- The same for article likes (comment_id is NULL there, and NULLs never collide).
CREATE UNIQUE INDEX notifications_page_once ON notifications(user_id, actor_id, page) WHERE kind = 'page_like';
