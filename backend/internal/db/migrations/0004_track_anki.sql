-- Track: add the 'anki' category. SQLite can't alter a CHECK constraint, so track_subcats and
-- track_hours are rebuilt (create new, copy, drop old, rename) with the widened CHECK. Categories
-- are stored by id, never by position, so existing rows need no remapping.
--
-- This runs inside the migration transaction with foreign_keys ON (the pragma can't be changed
-- inside a transaction), so the order keeps every foreign key satisfied at each step:
--   * track_hours_new points at track_subcats_new, which already holds every subcat id;
--   * the old track_hours (the only table referencing track_subcats) is dropped before the old
--     track_subcats, so the implicit DELETE of that DROP orphans nothing;
--   * renaming track_subcats_new → track_subcats rewrites track_hours_new's REFERENCES clause.
-- Ids (and track_hours' rowids) are copied as-is; the indexes are recreated under their old names.
-- There are no triggers or views on these tables.

CREATE TABLE track_subcats_new (
	id         INTEGER PRIMARY KEY,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	category   TEXT    NOT NULL CHECK (category IN ('active', 'passive', 'anki', 'phonetic', 'output', 'soroban')),
	name       TEXT    NOT NULL,
	archived   INTEGER NOT NULL DEFAULT 0,
	position   INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL
);
INSERT INTO track_subcats_new (id, user_id, category, name, archived, position, created_at)
	SELECT id, user_id, category, name, archived, position, created_at FROM track_subcats;

-- subcat_id NULL = the category's unassigned bucket. No cascade on the subcat: deleting a
-- subcat that still has hours is refused (archive it instead).
CREATE TABLE track_hours_new (
	user_id    INTEGER NOT NULL,
	week       TEXT    NOT NULL,
	category   TEXT    NOT NULL CHECK (category IN ('active', 'passive', 'anki', 'phonetic', 'output', 'soroban')),
	subcat_id  INTEGER REFERENCES track_subcats_new(id),
	centihours INTEGER NOT NULL CHECK (centihours > 0 AND centihours <= 16800),
	FOREIGN KEY (user_id, week) REFERENCES track_weeks(user_id, week) ON DELETE CASCADE
);
INSERT INTO track_hours_new (rowid, user_id, week, category, subcat_id, centihours)
	SELECT rowid, user_id, week, category, subcat_id, centihours FROM track_hours;

DROP TABLE track_hours;
DROP TABLE track_subcats;
ALTER TABLE track_subcats_new RENAME TO track_subcats;
ALTER TABLE track_hours_new RENAME TO track_hours;

CREATE INDEX track_subcats_user ON track_subcats(user_id, category);
CREATE UNIQUE INDEX track_subcats_name ON track_subcats(user_id, category, name COLLATE NOCASE) WHERE archived = 0;
CREATE UNIQUE INDEX track_hours_key ON track_hours(user_id, week, category, IFNULL(subcat_id, 0));
CREATE INDEX track_hours_subcat ON track_hours(subcat_id);
