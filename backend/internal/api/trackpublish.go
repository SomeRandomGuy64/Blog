package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"time"

	"fazon.nexus/backend/internal/week"
)

// A week's stats go public once it is over: everything other people see (leaderboard, summaries
// feed, someone else's profile) stops at the "published-through" week, normally the previous
// one. The admin may end the current week early, at most endEarlyWindow before its natural end;
// that publishes it at once. Logging is untouched: the calendar stays time-based, so users can
// keep editing an ended week until it rolls over, and those edits show up live.
const (
	endEarlyWindow = 2 * time.Hour
	settingEnded   = "track_ended_week" // settings key: the week the admin ended early
)

// publishedThrough is the last week whose stats are public.
func (s *Server) publishedThrough(ctx context.Context) (week.Key, error) {
	cur := week.Of(s.now())
	ended, err := s.endedEarly(ctx, cur)
	if err != nil || !ended {
		return cur.Add(-1), err
	}
	return cur, nil
}

// endedEarly reports whether the admin ended week cur early.
func (s *Server) endedEarly(ctx context.Context, cur week.Key) (bool, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, settingEnded).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return v == cur.String(), err
}

type endWeekJSON struct {
	Allowed     bool   `json:"allowed"`      // ending the current week early is possible right now
	AllowedFrom string `json:"allowed_from"` // from this time until current_week_end
}

type publicationJSON struct {
	PublishedThrough string       `json:"published_through"`
	CurrentWeek      string       `json:"current_week"`
	CurrentWeekEnd   string       `json:"current_week_end"` // natural end, RFC 3339
	EndedEarly       bool         `json:"ended_early"`      // the admin ended the current week early
	EndWeek          *endWeekJSON `json:"end_week,omitempty"`
}

// publication describes what's public now; admin adds whether the week can be ended early.
func (s *Server) publication(ctx context.Context, admin bool) (publicationJSON, error) {
	now := s.now()
	cur := week.Of(now)
	ended, err := s.endedEarly(ctx, cur)
	if err != nil {
		return publicationJSON{}, err
	}
	p := publicationJSON{PublishedThrough: cur.Add(-1).String(), CurrentWeek: cur.String(),
		CurrentWeekEnd: cur.End().Format(time.RFC3339), EndedEarly: ended}
	if ended {
		p.PublishedThrough = cur.String()
	}
	if admin {
		from := cur.End().Add(-endEarlyWindow)
		p.EndWeek = &endWeekJSON{Allowed: !now.Before(from), AllowedFrom: from.Format(time.RFC3339)}
	}
	return p, nil
}

// endWeek (POST) publishes the current week early; idempotent. Only in its last two hours.
func (s *Server) endWeek(w http.ResponseWriter, r *http.Request, u *user) {
	if !u.Admin {
		writeErr(w, http.StatusForbidden, "admins only")
		return
	}
	cur := week.Of(s.now())
	if from := cur.End().Add(-endEarlyWindow); s.now().Before(from) {
		writeErr(w, http.StatusConflict, "the week of "+cur.String()+" can only be ended in its last 2 hours: from "+
			from.Format(time.RFC3339)+" (it ends by itself at "+cur.End().Format(time.RFC3339)+")")
		return
	}
	if _, err := s.db.ExecContext(r.Context(), `INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, settingEnded, cur.String()); err != nil {
		internalErr(w, "end week", err)
		return
	}
	s.writePublication(w, r, u)
}

// reopenWeek (DELETE) undoes endWeek before the natural reset; idempotent.
func (s *Server) reopenWeek(w http.ResponseWriter, r *http.Request, u *user) {
	if !u.Admin {
		writeErr(w, http.StatusForbidden, "admins only")
		return
	}
	if _, err := s.db.ExecContext(r.Context(), `DELETE FROM settings WHERE key = ?`, settingEnded); err != nil {
		internalErr(w, "reopen week", err)
		return
	}
	s.writePublication(w, r, u)
}

func (s *Server) writePublication(w http.ResponseWriter, r *http.Request, u *user) {
	p, err := s.publication(r.Context(), u.Admin)
	if err != nil {
		internalErr(w, "load publication", err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}
