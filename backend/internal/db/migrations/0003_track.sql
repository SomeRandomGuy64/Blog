-- Track: weekly immersion log. Weeks are keyed by their label Saturday ("YYYY-MM-DD", see
-- internal/week); hours are stored as integer hundredths so sums stay exact.

ALTER TABLE users ADD COLUMN track_public INTEGER NOT NULL DEFAULT 1;
-- "About me" on profiles and hover cards (plain text, ≤ 300 runes, ≤ 6 lines).
ALTER TABLE users ADD COLUMN bio TEXT NOT NULL DEFAULT '';

CREATE TABLE track_subcats (
	id         INTEGER PRIMARY KEY,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	category   TEXT    NOT NULL CHECK (category IN ('active', 'passive', 'phonetic', 'output', 'soroban')),
	name       TEXT    NOT NULL,
	archived   INTEGER NOT NULL DEFAULT 0,
	position   INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL
);
CREATE INDEX track_subcats_user ON track_subcats(user_id, category);
CREATE UNIQUE INDEX track_subcats_name ON track_subcats(user_id, category, name COLLATE NOCASE) WHERE archived = 0;

-- kotu / kyotsu: JSON arrays of 7 daily percentages or nulls (manual entries only).
CREATE TABLE track_weeks (
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	week       TEXT    NOT NULL,
	kotu       TEXT    NOT NULL DEFAULT '[null,null,null,null,null,null,null]',
	kyotsu     TEXT    NOT NULL DEFAULT '[null,null,null,null,null,null,null]',
	summary    TEXT    NOT NULL DEFAULT '',
	summary_private INTEGER NOT NULL DEFAULT 0, -- summaries are public unless the owner hides them
	updated_at INTEGER NOT NULL,
	PRIMARY KEY (user_id, week)
) WITHOUT ROWID;
CREATE INDEX track_weeks_week ON track_weeks(week);
CREATE INDEX track_weeks_feed ON track_weeks(week DESC, updated_at DESC) WHERE summary != '' AND summary_private = 0;

-- subcat_id NULL = the category's unassigned bucket. No cascade on the subcat: deleting a
-- subcat that still has hours is refused (archive it instead).
CREATE TABLE track_hours (
	user_id    INTEGER NOT NULL,
	week       TEXT    NOT NULL,
	category   TEXT    NOT NULL CHECK (category IN ('active', 'passive', 'phonetic', 'output', 'soroban')),
	subcat_id  INTEGER REFERENCES track_subcats(id),
	centihours INTEGER NOT NULL CHECK (centihours > 0 AND centihours <= 16800),
	FOREIGN KEY (user_id, week) REFERENCES track_weeks(user_id, week) ON DELETE CASCADE
);
CREATE UNIQUE INDEX track_hours_key ON track_hours(user_id, week, category, IFNULL(subcat_id, 0));
CREATE INDEX track_hours_subcat ON track_hours(subcat_id);
