-- Video watch time from the site's tools (api/watch.go): the browser sends a heartbeat about
-- once a minute while a video plays, with the session's cumulative playing time, and each one
-- adds what wasn't added yet to the user's Track (Active immersion, the "Primed Listening"
-- sub-category, the current week) in the same transaction. One row per run, additive only:
--   client_id         the tool's id for the run (8-64 of A-Z a-z 0-9 _ -), unique per user:
--                     the first heartbeat inserts the row, later ones update it
--   started_at        when playing started (the first heartbeat's value; caps active_seconds)
--   updated_at        the last heartbeat
--   video_id          the YouTube video id, if the tool sent one (NULL = none)
--   active_seconds    the playing time so far, clamped; never goes down
--   track_centihours  what was actually added to Track in all, in hundredths of an hour (after caps)
--   track_week        the week it last added to (NULL = nothing added yet)
--   track_subcat      the sub-category's name at the time (NULL = the unassigned bucket)
--   track_note        a sentence for the tool: a sub-category created / restored, a fallback to
--                     unassigned, or a cap ('' = nothing to say)
CREATE TABLE watch_sessions (
	id               INTEGER PRIMARY KEY,
	user_id          INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	client_id        TEXT    NOT NULL,
	started_at       INTEGER NOT NULL,
	updated_at       INTEGER NOT NULL,
	video_id         TEXT,
	active_seconds   INTEGER NOT NULL DEFAULT 0,
	track_centihours INTEGER NOT NULL DEFAULT 0,
	track_week       TEXT,
	track_subcat     TEXT,
	track_note       TEXT    NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX watch_sessions_client ON watch_sessions(user_id, client_id);
