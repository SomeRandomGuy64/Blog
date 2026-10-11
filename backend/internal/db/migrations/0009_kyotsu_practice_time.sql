-- Kyōtsu trainer practice time (api/kyotsu.go). A session save adds the trainer's measured
-- active time to the user's Track (Phonetic training, the "Kyōtsu" sub-category) for the week
-- the session ended in, in the same transaction:
--   active_seconds    the active practice time the trainer reported, clamped (0 = none / old trainer)
--   track_centihours  what was actually added to Track, in hundredths of an hour (after caps)
--   track_subcat      the sub-category's name at the time (NULL = the unassigned bucket)
--   track_note        a sentence for the stats page: a sub-category created / restored, a
--                     fallback to unassigned, or a cap ('' = nothing to say)
--   client_id         the trainer's id for the run, so a retried save updates the session it
--                     already stored instead of storing (and counting) it twice (NULL = old trainer)
ALTER TABLE kyotsu_sessions ADD COLUMN active_seconds INTEGER NOT NULL DEFAULT 0;
ALTER TABLE kyotsu_sessions ADD COLUMN track_centihours INTEGER NOT NULL DEFAULT 0;
ALTER TABLE kyotsu_sessions ADD COLUMN track_subcat TEXT;
ALTER TABLE kyotsu_sessions ADD COLUMN track_note TEXT NOT NULL DEFAULT '';
ALTER TABLE kyotsu_sessions ADD COLUMN client_id TEXT;
CREATE UNIQUE INDEX kyotsu_sessions_client ON kyotsu_sessions(user_id, client_id) WHERE client_id IS NOT NULL;
