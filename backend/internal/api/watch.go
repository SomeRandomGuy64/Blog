package api

import (
	"database/sql"
	"errors"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"fazon.nexus/backend/internal/week"
)

const (
	watchCategory = "active"
	// maxWatchSpan bounds a watch session: it must start within this of its first heartbeat,
	// and it never counts more playing time than this.
	maxWatchSpan = 24 * time.Hour
	// maxWatchSkew is how far ahead of the server's clock a client's started may be (it is
	// then taken as now); further is a 400.
	maxWatchSkew = 5 * time.Minute
)

var (
	// watchSubcats is the Active immersion sub-category watch time goes to (no other spellings).
	watchSubcats = []string{"Primed Listening"}
	videoIDRe    = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
)

type watchSummary struct {
	ID            int64       `json:"id"`
	Started       string      `json:"started"`
	Updated       string      `json:"updated"`
	VideoID       *string     `json:"video_id"`
	ActiveSeconds int64       `json:"active_seconds"`
	TrackAdded    *trackAdded `json:"track_added"` // null = nothing added to Track (and nothing to say)
}

// watchCols are the columns scanWatch reads.
const watchCols = `id, started_at, updated_at, video_id, active_seconds, track_centihours, track_week, track_subcat, track_note`

func scanWatch(row interface{ Scan(...any) error }) (watchSummary, error) {
	var ws watchSummary
	var started, updated, cents int64
	var video, wk, sub sql.NullString
	var note string
	if err := row.Scan(&ws.ID, &started, &updated, &video, &ws.ActiveSeconds, &cents, &wk, &sub, &note); err != nil {
		return ws, err
	}
	ws.Started, ws.Updated = rfc3339(started), rfc3339(updated)
	if video.Valid {
		ws.VideoID = &video.String
	}
	if cents > 0 || note != "" {
		ws.TrackAdded = &trackAdded{Week: wk.String, Hours: float64(cents) / 100, Note: note}
		if sub.Valid {
			ws.TrackAdded.Subcat = &sub.String
		}
	}
	return ws, nil
}

// postWatch is a watch session's heartbeat: the tool sends one about every minute while a
// video plays and a last one on pause / page hide, each with the session's cumulative playing
// time. The first for a client_id stores the session (201), later ones update it (200):
// active_seconds only goes up and never past the wall-clock span since the session started
// (capped at 24 h). In the same transaction, whatever the session's time (in whole minutes,
// like the stopwatch) hasn't added to Track yet goes to the current week: Active immersion,
// the "Primed Listening" sub-category (created or restored from the archive if needed).
func (s *Server) postWatch(w http.ResponseWriter, r *http.Request, u *user) {
	var in struct {
		ClientID      string   `json:"client_id"`
		Started       string   `json:"started"`
		ActiveSeconds *float64 `json:"active_seconds"`
		VideoID       string   `json:"video_id"`
	}
	if !decode(w, r, maxBody, &in) {
		return
	}
	now := s.now()
	if !clientIDRe.MatchString(in.ClientID) {
		writeErr(w, http.StatusBadRequest, "client_id must be 8-64 letters, digits, - or _")
		return
	}
	started, err := time.Parse(time.RFC3339, in.Started)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "started must be an RFC 3339 time")
		return
	}
	if in.ActiveSeconds == nil || *in.ActiveSeconds < 0 {
		writeErr(w, http.StatusBadRequest, "active_seconds must be a number of seconds, 0 or more")
		return
	}
	var videoID any
	if in.VideoID != "" {
		if !videoIDRe.MatchString(in.VideoID) {
			writeErr(w, http.StatusBadRequest, "video_id must be an 11-character YouTube video id")
			return
		}
		videoID = in.VideoID
	}
	if !s.watchLimit.allow(strconv.FormatInt(u.ID, 10), now) {
		writeErr(w, http.StatusTooManyRequests, "too many watch heartbeats, slow down")
		return
	}
	status := http.StatusOK
	var out watchSummary
	err = s.tx(r.Context(), func(tx *sql.Tx) error {
		var id, start, prevActive int64
		err := tx.QueryRow(`SELECT id, started_at, active_seconds FROM watch_sessions WHERE user_id = ? AND client_id = ?`,
			u.ID, in.ClientID).Scan(&id, &start, &prevActive)
		switch {
		case errors.Is(err, sql.ErrNoRows): // the first heartbeat: started is checked and kept
			switch {
			case started.After(now.Add(maxWatchSkew)):
				return errStatus{http.StatusBadRequest, "started is in the future"}
			case started.Before(now.Add(-maxWatchSpan)):
				return errStatus{http.StatusBadRequest, "started is more than 24 hours ago: start a new session"}
			}
			status = http.StatusCreated
			start = min(started.Unix(), now.Unix()) // client clock a little ahead: from now
			if err := tx.QueryRow(`INSERT INTO watch_sessions (user_id, client_id, started_at, updated_at, video_id)
				VALUES (?, ?, ?, ?, ?) RETURNING id`, u.ID, in.ClientID, start, now.Unix(), videoID).Scan(&id); err != nil {
				return err
			}
		case err != nil:
			return err
		}
		// Playing time: never more than the time since the stored start (a later heartbeat can't
		// move it), never less than before.
		span := min(now.Unix()-start, int64(maxWatchSpan/time.Second))
		active := max(prevActive, min(int64(math.Round(min(*in.ActiveSeconds, 1e9))), span))
		if _, err := tx.Exec(`UPDATE watch_sessions SET active_seconds = ?, updated_at = ?, video_id = COALESCE(?, video_id)
			WHERE id = ?`, active, now.Unix(), videoID, id); err != nil {
			return err
		}
		// Only time not yet added (most heartbeats within the same minute add nothing).
		if add := practiceCents(active) - practiceCents(prevActive); add > 0 {
			key := week.Of(now).String()
			c, err := creditTrackTx(tx, u.ID, watchCategory, watchSubcats, key, add, now.Unix())
			if err != nil {
				return err
			}
			if _, err := tx.Exec(`UPDATE watch_sessions SET track_centihours = track_centihours + ?, track_week = ?,
					track_subcat = COALESCE(track_subcat, ?), track_note = CASE WHEN ? = '' THEN track_note ELSE ? END
				WHERE id = ?`, c.added, key, c.subcat, c.note, c.note, id); err != nil {
				return err
			}
		}
		out, err = scanWatch(tx.QueryRow(`SELECT `+watchCols+` FROM watch_sessions WHERE id = ?`, id))
		return err
	})
	if err != nil {
		writeTxErr(w, "save watch session", err)
		return
	}
	writeJSON(w, status, map[string]any{"id": out.ID, "active_seconds": out.ActiveSeconds, "track_added": out.TrackAdded})
}
