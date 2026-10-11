-- Never hand a deleted account's id to a new one (old /users/?id=N links, notifications or cached
-- hover cards would otherwise show a stranger). users.id is a plain INTEGER PRIMARY KEY, which
-- SQLite reuses when the highest row is deleted, and switching it to AUTOINCREMENT would mean
-- rebuilding users, which half the schema references. Instead this keeps a high-water mark in
-- settings, and every INSERT INTO users picks its id past it (db.NextUserID).
INSERT INTO settings (key, value)
	VALUES ('users_max_id', CAST((SELECT COALESCE(MAX(id), 0) FROM users) AS TEXT))
	ON CONFLICT (key) DO NOTHING;

CREATE TRIGGER users_max_id_on_delete AFTER DELETE ON users BEGIN
	INSERT INTO settings (key, value) VALUES ('users_max_id', CAST(OLD.id AS TEXT))
		ON CONFLICT (key) DO UPDATE SET value = CAST(MAX(CAST(value AS INTEGER), OLD.id) AS TEXT);
END;
