package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"
	"time"
	"unicode/utf8"

	"fazon.nexus/backend/internal/week"
)

const (
	maxAnswers      = 5000
	maxAnswerField  = 64
	maxSessionsList = 1000
)

var (
	kyotsuLayouts = map[string]bool{"kotu": true, "classic": true}
	// The Pattern Stats buckets per mode: Keihan register × kernel; Tokyo = Kotu's own three
	// (Heiban / Odaka, Atamadaka, Nakadaka).
	kyotsuCats = map[string]map[string]bool{
		modeKeihan: {"HM": true, "HA": true, "HN": true, "HO": true, "LM": true, "LA": true, "LN": true, "LO": true},
		modeTokyo:  {"TH": true, "TA": true, "TN": true},
	}
)

// Trainer modes (kyotsu_sessions.mode): Keihan pitch, or the Tokyo mode (hyojungo.go).
const (
	modeKeihan = "keihan"
	modeTokyo  = "tokyo"
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
	ID            int64           `json:"id"`
	Started       string          `json:"started"`
	Ended         string          `json:"ended"`
	Layout        string          `json:"layout"`
	Mode          string          `json:"mode"` // keihan or tokyo
	Advanced      bool            `json:"advanced"`
	Total         int             `json:"total"`
	Correct       int             `json:"correct"`
	ActiveSeconds int64           `json:"active_seconds"` // practice time the trainer measured; 0 = none
	TrackAdded    *trackAdded     `json:"track_added"`    // null = nothing added to Track (and nothing to say)
	Answers       json.RawMessage `json:"answers,omitempty"`
}

// kyotsuCols are the columns scanKyotsu reads (answers_json not included).
const kyotsuCols = `id, started_at, ended_at, layout, mode, advanced, total, correct, active_seconds, track_centihours, track_subcat, track_note`

func scanKyotsu(row interface{ Scan(...any) error }, extra ...any) (kyotsuSummary, error) {
	var k kyotsuSummary
	var started, ended, cents int64
	var sub sql.NullString
	var note string
	err := row.Scan(append([]any{&k.ID, &started, &ended, &k.Layout, &k.Mode, &k.Advanced, &k.Total, &k.Correct,
		&k.ActiveSeconds, &cents, &sub, &note}, extra...)...)
	if err != nil {
		return k, err
	}
	k.Started, k.Ended = rfc3339(started), rfc3339(ended)
	if cents > 0 || note != "" {
		k.TrackAdded = &trackAdded{Week: week.Of(time.Unix(ended, 0)).String(), Hours: float64(cents) / 100, Note: note}
		if sub.Valid {
			k.TrackAdded.Subcat = &sub.String
		}
	}
	return k, nil
}

// kyotsuSubcats is the Phonetic training sub-category each mode's time goes to, with the
// other spellings an existing one may have.
var kyotsuSubcats = map[string][]string{
	modeKeihan: {"Kyōtsu", "Kyotsu"},
	modeTokyo:  {"Kotu", "コツ"},
}

const kyotsuCategory = "phonetic"

// postKyotsu stores a finished trainer session and, in the same transaction, adds its active
// practice time to the user's Track: Phonetic training, the "Kyōtsu" sub-category ("Kotu" for
// Tokyo mode; created or restored from the archive if needed), the week the session ended in
// (now). Tokyo-mode sessions need Tokyo-mode access. A save repeated
// with the same client_id (a retry after a lost response) updates that session instead: newer
// answers replace the stored ones and only time not yet added is added.
func (s *Server) postKyotsu(w http.ResponseWriter, r *http.Request, u *user) {
	var in struct {
		Started       string         `json:"started"`
		Layout        string         `json:"layout"`
		Mode          string         `json:"mode"` // "" = keihan (older trainers)
		Advanced      bool           `json:"advanced"`
		Answers       []kyotsuAnswer `json:"answers"`
		ActiveSeconds *float64       `json:"active_seconds"`
		ClientID      string         `json:"client_id"`
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
	if in.Mode == "" {
		in.Mode = modeKeihan
	}
	cats := kyotsuCats[in.Mode]
	if cats == nil {
		writeErr(w, http.StatusBadRequest, "mode must be keihan or tokyo")
		return
	}
	if in.Mode == modeTokyo && !u.Hyojungo {
		writeErr(w, http.StatusForbidden, "Tokyo mode isn't enabled for your account")
		return
	}
	if len(in.Answers) < 1 || len(in.Answers) > maxAnswers {
		writeErr(w, http.StatusBadRequest, "answers must have 1-5000 entries")
		return
	}
	if in.ClientID != "" && !clientIDRe.MatchString(in.ClientID) {
		writeErr(w, http.StatusBadRequest, "client_id must be 8-64 letters, digits, - or _")
		return
	}
	correct := 0
	for i, a := range in.Answers {
		if !cats[a.Cat] {
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
	// Active time: never negative, never more than the session's wall-clock span (a bad or
	// tampered value can't add more than the time that actually passed).
	var active int64
	if in.ActiveSeconds != nil && *in.ActiveSeconds > 0 {
		span := int64(now.Sub(started) / time.Second)
		active = min(int64(math.Round(min(*in.ActiveSeconds, 1e9))), span)
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
	var clientID any
	if in.ClientID != "" {
		clientID = in.ClientID
	}
	status := http.StatusCreated
	var out kyotsuSummary
	err = s.tx(r.Context(), func(tx *sql.Tx) error {
		var id, ended, prevActive, prevCents int64
		var total int
		mode := in.Mode
		err := tx.QueryRow(`SELECT id, ended_at, active_seconds, track_centihours, total, mode FROM kyotsu_sessions
			WHERE user_id = ? AND client_id = ?`, u.ID, clientID).Scan(&id, &ended, &prevActive, &prevCents, &total, &mode)
		switch {
		case errors.Is(err, sql.ErrNoRows): // a new session
			ended = now.Unix()
			if err := tx.QueryRow(`INSERT INTO kyotsu_sessions
				(user_id, started_at, ended_at, layout, mode, advanced, total, correct, answers_json, active_seconds, client_id)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
				u.ID, started.Unix(), ended, in.Layout, in.Mode, in.Advanced, len(in.Answers), correct, string(answers), active, clientID).Scan(&id); err != nil {
				return err
			}
		case err != nil:
			return err
		default: // a retry: keep the first save's end time (and so its week) and mode; take newer answers
			status = http.StatusOK
			if mode != in.Mode {
				return errStatus{http.StatusConflict, "client_id belongs to a session in the other mode"}
			}
			if len(in.Answers) > total {
				if _, err := tx.Exec(`UPDATE kyotsu_sessions SET total = ?, correct = ?, answers_json = ? WHERE id = ?`,
					len(in.Answers), correct, string(answers), id); err != nil {
					return err
				}
			}
			if active <= prevActive {
				active = prevActive
			} else if _, err := tx.Exec(`UPDATE kyotsu_sessions SET active_seconds = ? WHERE id = ?`, active, id); err != nil {
				return err
			}
		}
		// Only time not yet added (a retry adds the difference, if any).
		if add := practiceCents(active) - practiceCents(prevActive); add > 0 {
			if err := s.addKyotsuTime(tx, u.ID, id, mode, week.Of(time.Unix(ended, 0)).String(), add, now.Unix()); err != nil {
				return err
			}
		}
		out, err = scanKyotsu(tx.QueryRow(`SELECT `+kyotsuCols+` FROM kyotsu_sessions WHERE id = ?`, id))
		return err
	})
	if err != nil {
		writeTxErr(w, "save kyotsu session", err)
		return
	}
	writeJSON(w, status, map[string]any{"id": out.ID, "active_seconds": out.ActiveSeconds, "track_added": out.TrackAdded})
}

// addKyotsuTime adds cents of phonetic time from session id (in mode) to the user's week key
// (creditTrackTx) and records on the session what it added, where, and why there if it wasn't
// the mode's sub-category.
func (s *Server) addKyotsuTime(tx *sql.Tx, userID, id int64, mode, key string, cents, now int64) error {
	c, err := creditTrackTx(tx, userID, kyotsuCategory, kyotsuSubcats[mode], key, cents, now)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`UPDATE kyotsu_sessions SET track_centihours = track_centihours + ?,
			track_subcat = COALESCE(track_subcat, ?), track_note = CASE WHEN ? = '' THEN track_note ELSE ? END
		WHERE id = ?`, c.added, c.subcat, c.note, c.note, id)
	return err
}

// listKyotsu returns the newest sessions (up to maxSessionsList), oldest first for charting.
func (s *Server) listKyotsu(w http.ResponseWriter, r *http.Request, u *user) {
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT * FROM (
			SELECT `+kyotsuCols+` FROM kyotsu_sessions
			WHERE user_id = ? ORDER BY started_at DESC, id DESC LIMIT ?
		) ORDER BY started_at, id`, u.ID, maxSessionsList)
	if err != nil {
		internalErr(w, "list kyotsu sessions", err)
		return
	}
	defer rows.Close()
	out := []kyotsuSummary{}
	for rows.Next() {
		k, err := scanKyotsu(rows)
		if err != nil {
			internalErr(w, "scan kyotsu session", err)
			return
		}
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
	var answers string
	// Someone else's session is a 404, not a 403: ids don't reveal what exists.
	k, err := scanKyotsu(s.db.QueryRowContext(r.Context(), `SELECT `+kyotsuCols+`, answers_json
		FROM kyotsu_sessions WHERE id = ? AND user_id = ?`, id, u.ID), &answers)
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		internalErr(w, "get kyotsu session", err)
		return
	}
	k.Answers = json.RawMessage(answers)
	writeJSON(w, http.StatusOK, k)
}
