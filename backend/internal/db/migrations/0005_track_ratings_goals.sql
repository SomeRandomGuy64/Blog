-- Track: per-category weekly self-ratings and goals, and a small key/value table for site-wide
-- settings (first use: the admin ending the current Track week early). Additive only: three new
-- tables, no changes to existing ones.

-- rating: 1 = 😞, 2 = 😐, 3 = 👌, 4 = 🔥. No row = not rated. A rating belongs to a logged week
-- (a track_weeks row), so deleting the week or the user takes its ratings with it.
CREATE TABLE track_ratings (
	user_id  INTEGER NOT NULL,
	week     TEXT    NOT NULL,
	category TEXT    NOT NULL CHECK (category IN ('active', 'passive', 'anki', 'phonetic', 'output', 'soroban')),
	rating   INTEGER NOT NULL CHECK (rating BETWEEN 1 AND 4),
	PRIMARY KEY (user_id, week, category),
	FOREIGN KEY (user_id, week) REFERENCES track_weeks(user_id, week) ON DELETE CASCADE
) WITHOUT ROWID;

-- Goals: hours a user aims for in a category, set during that week and write-once (never changed
-- or cleared; see api.setGoals). Private to the owner. They hang off the user, not track_weeks:
-- a save that empties the week deletes its track_weeks row, and that mustn't wipe (and so free
-- up) the goals.
CREATE TABLE track_goals (
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	week       TEXT    NOT NULL,
	category   TEXT    NOT NULL CHECK (category IN ('active', 'passive', 'anki', 'phonetic', 'output', 'soroban')),
	centihours INTEGER NOT NULL CHECK (centihours > 0 AND centihours <= 16800),
	created_at INTEGER NOT NULL,
	PRIMARY KEY (user_id, week, category)
) WITHOUT ROWID;

-- Keys in use: 'track_ended_week' = the key of a week the admin ended early (see api/trackpublish.go).
CREATE TABLE settings (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
) WITHOUT ROWID;
