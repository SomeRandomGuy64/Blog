-- Track: sub-categories belong to weeks. track_subcats stays the user's register of names (one
-- id per name, so a name used in several weeks is one sub-category to the charts and to scripts),
-- and a week's form shows only the sub-categories added to that week plus any it has hours for.
-- Hours don't need a row here: a week with hours for a sub-category always shows it.

CREATE TABLE track_week_subcats (
	user_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	week      TEXT    NOT NULL,
	subcat_id INTEGER NOT NULL REFERENCES track_subcats(id) ON DELETE CASCADE,
	PRIMARY KEY (user_id, week, subcat_id)
) WITHOUT ROWID;
CREATE INDEX track_week_subcats_subcat ON track_week_subcats(subcat_id);

-- Existing active sub-categories join the week they were created in (weeks start Sunday 03:00
-- UTC and are labelled by the Saturday before, see internal/week), so past weeks keep only what
-- they have hours for.
INSERT INTO track_week_subcats (user_id, week, subcat_id)
	SELECT user_id, date(created_at - 10800, 'unixepoch', '-7 days', 'weekday 6'), id
	FROM track_subcats WHERE archived = 0;
