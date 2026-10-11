package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"
	"unicode/utf8"
)

const (
	maxAnswers      = 5000
	maxAnswerField  = 64
	maxSessionsList = 1000
)

var (
	kyotsuLayouts = map[string]bool{"kotu": true, "classic": true}
	kyotsuCats    = map[string]bool{"HM": true, "HA": true, "HN": true, "HO": true, "LM": true, "LA": true, "LN": true, "LO": true}
)

// kyotsuAnswer is one trainer answer, re-encoded from the request so only these fields are stored.
type kyotsuAnswer struct {
	Reading string `json:"reading"`
	Surface string `json:"surface"`
	Pattern string `json:"pattern"`
	Cat     string `json:"cat"`
	Speaker string `json:"speaker"`
	OK      bool   `json:"ok"`
}

type kyotsuSummary struct {
	ID       int64           `json:"id"`
	Started  string          `json:"started"`
	Ended    string          `json:"ended"`
	Layout   string          `json:"layout"`
	Advanced bool            `json:"advanced"`
	Total    int             `json:"total"`
	Correct  int             `json:"correct"`
	Answers  json.RawMessage `json:"answers,omitempty"`
}

func (s *Server) postKyotsu(w http.ResponseWriter, r *http.Request, u *user) {
	var in struct {
		Started  string         `json:"started"`
		Layout   string         `json:"layout"`
		Advanced bool           `json:"advanced"`
		Answers  []kyotsuAnswer `json:"answers"`
	}
	if !decode(w, r, maxKyotsuBody, &in) {
		return
	}
	now := s.now()
	started, err := time.Parse(time.RFC3339, in.Started)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "started must be an RFC 3339 time")
		return
	}
	if started.After(now) { // client clock ahead: the session can't have started after it ended
		started = now
	}
	if !kyotsuLayouts[in.Layout] {
		writeErr(w, http.StatusBadRequest, "layout must be kotu or classic")
		return
	}
	if len(in.Answers) < 1 || len(in.Answers) > maxAnswers {
		writeErr(w, http.StatusBadRequest, "answers must have 1-5000 entries")
		return
	}
	correct := 0
	for i, a := range in.Answers {
		if !kyotsuCats[a.Cat] {
			writeErr(w, http.StatusBadRequest, "answer "+strconv.Itoa(i)+": invalid cat")
			return
		}
		for _, f := range []string{a.Reading, a.Surface, a.Pattern, a.Speaker} {
			if utf8.RuneCountInString(f) > maxAnswerField {
				writeErr(w, http.StatusBadRequest, "answer "+strconv.Itoa(i)+": field longer than 64 characters")
				return
			}
		}
		if a.OK {
			correct++
		}
	}
	if !s.kyoLimit.allow(strconv.FormatInt(u.ID, 10), now) {
		writeErr(w, http.StatusTooManyRequests, "too many sessions, slow down")
		return
	}
	answers, err := json.Marshal(in.Answers)
	if err != nil {
		internalErr(w, "encode answers", err)
		return
	}
	var id int64
	err = s.db.QueryRowContext(r.Context(), `INSERT INTO kyotsu_sessions
		(user_id, started_at, ended_at, layout, advanced, total, correct, answers_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		u.ID, started.Unix(), now.Unix(), in.Layout, in.Advanced, len(in.Answers), correct, string(answers)).Scan(&id)
	if err != nil {
		internalErr(w, "save kyotsu session", err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

// listKyotsu returns the newest sessions (up to maxSessionsList), oldest first for charting.
func (s *Server) listKyotsu(w http.ResponseWriter, r *http.Request, u *user) {
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT * FROM (
			SELECT id, started_at, ended_at, layout, advanced, total, correct FROM kyotsu_sessions
			WHERE user_id = ? ORDER BY started_at DESC, id DESC LIMIT ?
		) ORDER BY started_at, id`, u.ID, maxSessionsList)
	if err != nil {
		internalErr(w, "list kyotsu sessions", err)
		return
	}
	defer rows.Close()
	out := []kyotsuSummary{}
	for rows.Next() {
		var k kyotsuSummary
		var started, ended int64
		if err := rows.Scan(&k.ID, &started, &ended, &k.Layout, &k.Advanced, &k.Total, &k.Correct); err != nil {
			internalErr(w, "scan kyotsu session", err)
			return
		}
		k.Started, k.Ended = rfc3339(started), rfc3339(ended)
		out = append(out, k)
	}
	if err := rows.Err(); err != nil {
		internalErr(w, "list kyotsu sessions", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": out})
}

func (s *Server) getKyotsu(w http.ResponseWriter, r *http.Request, u *user) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var k kyotsuSummary
	var started, ended int64
	var answers string
	// Someone else's session is a 404, not a 403: ids don't reveal what exists.
	err := s.db.QueryRowContext(r.Context(), `SELECT id, started_at, ended_at, layout, advanced, total, correct, answers_json
		FROM kyotsu_sessions WHERE id = ? AND user_id = ?`, id, u.ID).
		Scan(&k.ID, &started, &ended, &k.Layout, &k.Advanced, &k.Total, &k.Correct, &answers)
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		internalErr(w, "get kyotsu session", err)
		return
	}
	k.Started, k.Ended = rfc3339(started), rfc3339(ended)
	k.Answers = json.RawMessage(answers)
	writeJSON(w, http.StatusOK, k)
}
