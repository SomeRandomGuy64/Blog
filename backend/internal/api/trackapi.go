package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"fazon.nexus/backend/internal/week"
)

// The /v1/api/track endpoints: a user's own Track data for scripts, authenticated with a
// personal API key (keyAuthed). They read and change only the caller's data and reuse the
// site's computations and rules (loadWeeks, kyotsuAuto, goals, sub-category validation), so a
// week looks and behaves the same from the site and from a script. The published-through rule
// doesn't apply: it is the owner's own data.

const maxHourEntries = 100 // per PATCH …/hours

// apiSubHours is one sub-category's hours in a week.
type apiSubHours struct {
	ID       int64   `json:"id"`
	Name     string  `json:"name"`
	Archived bool    `json:"archived"`
	Hours    float64 `json:"hours"`
}

// apiCatHours splits a category's hours: the unassigned bucket and each sub-category with hours.
type apiCatHours struct {
	Unassigned float64       `json:"unassigned"`
	Subcats    []apiSubHours `json:"subcats"`
}

// apiWeek is one week in the key API.
type apiWeek struct {
	Week      string                 `json:"week"`
	Start     string                 `json:"start"`
	End       string                 `json:"end"`
	Current   bool                   `json:"current"`
	Logged    bool                   `json:"logged"`  // a logged week (hours, scores, summary or ratings)
	Totals    map[string]float64     `json:"totals"`  // every category, plus "all" and "nopassive"
	Hours     map[string]apiCatHours `json:"hours"`   // every category
	Ratings   map[string]int         `json:"ratings"` // rated categories only
	Goals     map[string]float64     `json:"goals"`   // categories with a goal only
	Summary   string                 `json:"summary"`
	Private   bool                   `json:"private"` // the summary is private
	Kotu      *float64               `json:"kotu"`    // weekly averages of the daily %, 1 decimal
	Kyotsu    *float64               `json:"kyotsu"`
	Updated   *string                `json:"updated"`
	*weekDays                        // with ?days=1
}

// apiWeeks builds every week from..to (oldest first), logged or not, for userID.
func (s *Server) apiWeeks(ctx context.Context, userID int64, from, to week.Key, withDays bool) ([]apiWeek, error) {
	m, err := s.loadWeeks(ctx, []int64{userID}, from, to)
	if err != nil {
		return nil, err
	}
	logged := map[week.Key]*weekData{}
	for _, d := range m[userID] {
		logged[d.key] = d
	}
	keys := week.Range(from, to)
	unlogged := map[week.Key]*weekData{}
	for _, k := range keys {
		if logged[k] == nil {
			unlogged[k] = &weekData{key: k, cents: map[string]map[int64]int64{}, ratings: map[string]int{}}
		}
	}
	// Trainer sessions count towards Kyōtsu in unlogged weeks too (loadWeeks did the logged ones).
	if len(unlogged) > 0 {
		if err := s.kyotsuAuto(ctx, idsJSON([]int64{userID}), from, to,
			func(_ int64, k week.Key) *weekData { return unlogged[k] }); err != nil {
			return nil, err
		}
	}
	goals, err := goalsRange(ctx, s.db, userID, from, to)
	if err != nil {
		return nil, err
	}
	subcats, err := s.userSubcats(ctx, userID)
	if err != nil {
		return nil, err
	}
	cur := week.Of(s.now())
	out := make([]apiWeek, 0, len(keys))
	for _, k := range keys {
		d, isLogged := logged[k], true
		if d == nil {
			d, isLogged = unlogged[k], false
		}
		item := d.listItem()
		all := d.allCents()
		item.Totals["all"] = float64(all) / 100
		item.Totals["nopassive"] = float64(all-d.total("passive")) / 100
		aw := apiWeek{Week: k.String(), Start: k.Start().Format(time.RFC3339), End: k.End().Format(time.RFC3339),
			Current: k == cur, Logged: isLogged, Totals: item.Totals, Hours: map[string]apiCatHours{},
			Ratings: item.Ratings, Goals: goals[k.String()], Summary: d.summary, Private: d.summaryPrivate,
			Kotu: item.Kotu, Kyotsu: item.Kyotsu}
		if aw.Goals == nil {
			aw.Goals = map[string]float64{}
		}
		if isLogged {
			up := rfc3339(d.updated)
			aw.Updated = &up
		}
		for _, c := range trackCategories {
			aw.Hours[c.ID] = apiCatHours{Unassigned: float64(d.cents[c.ID][0]) / 100, Subcats: []apiSubHours{}}
		}
		for _, sc := range subcats { // display order
			if c := d.cents[sc.Category][sc.ID]; c > 0 {
				ch := aw.Hours[sc.Category]
				ch.Subcats = append(ch.Subcats, apiSubHours{ID: sc.ID, Name: sc.Name, Archived: sc.Archived, Hours: float64(c) / 100})
				aw.Hours[sc.Category] = ch
			}
		}
		if withDays {
			aw.weekDays = d.days()
		}
		out = append(out, aw)
	}
	return out, nil
}

func wantDays(r *http.Request) bool {
	v := r.URL.Query().Get("days")
	return v == "1" || v == "true"
}

// writeAPIWeek answers one week (after a change too).
func (s *Server) writeAPIWeek(w http.ResponseWriter, r *http.Request, userID int64, k week.Key) {
	weeks, err := s.apiWeeks(r.Context(), userID, k, k, wantDays(r))
	if err != nil {
		internalErr(w, "load week", err)
		return
	}
	writeJSON(w, http.StatusOK, weeks[0])
}

// GET /v1/api/track/weeks?from=&to=&days=1
func (s *Server) apiListWeeks(w http.ResponseWriter, r *http.Request, u *user) {
	to, ok := s.queryWeek(w, r, "to", week.Of(s.now()))
	if !ok {
		return
	}
	from, ok := s.queryWeek(w, r, "from", to.Add(-(defRangeWeeks - 1)))
	if !ok {
		return
	}
	if to.Before(from) {
		writeErr(w, http.StatusBadRequest, "from is after to")
		return
	}
	if !to.Before(from.Add(maxRangeWeeks)) {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("at most %d weeks per request", maxRangeWeeks))
		return
	}
	weeks, err := s.apiWeeks(r.Context(), u.ID, from, to, wantDays(r))
	if err != nil {
		internalErr(w, "list weeks", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"from": from.String(), "to": to.String(),
		"current_week": week.Of(s.now()).String(), "weeks": weeks})
}

// GET /v1/api/track/weeks/{key}?days=1
func (s *Server) apiGetWeek(w http.ResponseWriter, r *http.Request, u *user) {
	k, ok := s.pathWeek(w, r)
	if !ok {
		return
	}
	s.writeAPIWeek(w, r, u.ID, k)
}

// GET /v1/api/track/subcats
func (s *Server) apiListSubcats(w http.ResponseWriter, r *http.Request, u *user) {
	subcats, err := s.userSubcats(r.Context(), u.ID)
	if err != nil {
		internalErr(w, "list subcats", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"subcats": subcats})
}

// POST /v1/api/track/subcats {category, name}: the site's rules (name 1–40 characters, unique
// among the category's active sub-categories ignoring case, at most maxAllSubcats per category).
func (s *Server) apiCreateSubcat(w http.ResponseWriter, r *http.Request, u *user) {
	var in struct {
		Category string `json:"category"`
		Name     string `json:"name"`
	}
	if !decode(w, r, maxBody, &in) {
		return
	}
	name, err := validSubcat(in.Category, in.Name)
	if err == nil {
		var c subcatJSON
		if c, err = s.insertSubcat(r.Context(), u.ID, in.Category, name); err == nil {
			writeJSON(w, http.StatusCreated, c)
			return
		}
	}
	writeTxErr(w, "create subcat", err)
}

// POST /v1/api/track/weeks/{key}/goals {goals:{category:hours}}: exactly the site's goals.
func (s *Server) apiSetGoals(w http.ResponseWriter, r *http.Request, u *user) {
	k, ok := s.pathWeek(w, r)
	if !ok {
		return
	}
	cents, ok := s.goalsInput(w, r, k)
	if !ok {
		return
	}
	if err := s.applyGoals(r.Context(), u.ID, k, cents); err != nil {
		writeTxErr(w, "set goals", err)
		return
	}
	s.writeAPIWeek(w, r, u.ID, k)
}

// ---- writes to a week ----

// touchWeek makes sure the week has a track_weeks row (a new one is empty: no scores, summary
// or ratings) and bumps its updated_at, as a site save does.
func touchWeek(tx *sql.Tx, userID int64, key string, now int64) error {
	_, err := tx.Exec(`INSERT INTO track_weeks (user_id, week, updated_at) VALUES (?, ?, ?)
		ON CONFLICT (user_id, week) DO UPDATE SET updated_at = `+bumpUpdated, userID, key, now)
	return err
}

// bumpUpdated is the updated_at of a changed week in an upsert: now, but always later than
// before, so every change gives the week a new "updated" (the logger's if_updated check) even
// with two changes in the same second.
const bumpUpdated = `MAX(excluded.updated_at, track_weeks.updated_at + 1)`

// pruneWeek deletes the week's row if nothing is left in it — no hours, Kotu or Kyōtsu entries,
// summary or ratings — the same rule as the site's save, so a week emptied through the API is
// unlogged just as if it had been emptied on the site. Ratings keep a week, so they are never
// lost this way; goals live outside the row and stay regardless.
func pruneWeek(tx *sql.Tx, userID int64, key string) error {
	var kotu, kyotsu, summary string
	var hours, ratings int
	err := tx.QueryRow(`SELECT kotu, kyotsu, summary,
			(SELECT COUNT(*) FROM track_hours h WHERE h.user_id = w.user_id AND h.week = w.week),
			(SELECT COUNT(*) FROM track_ratings t WHERE t.user_id = w.user_id AND t.week = w.week)
		FROM track_weeks w WHERE user_id = ? AND week = ?`, userID, key).Scan(&kotu, &kyotsu, &summary, &hours, &ratings)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	ko, err := parseScores(kotu)
	if err != nil {
		return err
	}
	ky, err := parseScores(kyotsu)
	if err != nil {
		return err
	}
	if hours > 0 || ratings > 0 || summary != "" || !ko.empty() || !ky.empty() {
		return nil
	}
	_, err = tx.Exec(`DELETE FROM track_weeks WHERE user_id = ? AND week = ?`, userID, key)
	return err
}

// hourSlot is one hours row of a week: a category and a sub-category id (0 = unassigned).
type hourSlot struct {
	cat string
	sub int64
}

// weekHoursTx reads a week's hour rows (hundredths of an hour) inside tx.
func weekHoursTx(tx *sql.Tx, userID int64, key string) (map[hourSlot]int64, error) {
	hours := map[hourSlot]int64{}
	rows, err := tx.Query(`SELECT category, IFNULL(subcat_id, 0), centihours FROM track_hours WHERE user_id = ? AND week = ?`, userID, key)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var sl hourSlot
		var c int64
		if err := rows.Scan(&sl.cat, &sl.sub, &c); err != nil {
			return nil, err
		}
		hours[sl] = c
	}
	return hours, rows.Err()
}

// catCents is a category's total in a week's hour rows.
func catCents(hours map[hourSlot]int64, cat string) int64 {
	var t int64
	for sl, c := range hours {
		if sl.cat == cat {
			t += c
		}
	}
	return t
}

// addCapped adds up to cents to the row sl of a week's hours, as much as the 168 h per row and
// per category allow, and returns what it added.
func addCapped(hours map[hourSlot]int64, sl hourSlot, cents int64) int64 {
	room := maxWeekCents - catCents(hours, sl.cat) // the row is part of its category
	cents = max(0, min(cents, room))
	if cents > 0 {
		hours[sl] += cents
	}
	return cents
}

// writeWeekHoursTx stores a week's changed hour rows: every row of after that differs from
// before (0 removes a row). If anything changed, the week gets a track_weeks row / a new
// updated_at first (touchWeek, as a site save does) and is unlogged afterwards if nothing is
// left in it (pruneWeek). Callers check the 168 h limits.
func writeWeekHoursTx(tx *sql.Tx, userID int64, key string, now int64, before, after map[hourSlot]int64) error {
	changed := false
	for sl, c := range after {
		changed = changed || c != before[sl]
	}
	if !changed {
		return nil
	}
	if err := touchWeek(tx, userID, key, now); err != nil {
		return err
	}
	for sl, c := range after {
		if c == before[sl] {
			continue
		}
		var subID any
		if sl.sub != 0 {
			subID = sl.sub
		}
		if _, err := tx.Exec(`DELETE FROM track_hours WHERE user_id = ? AND week = ? AND category = ? AND IFNULL(subcat_id, 0) = ?`,
			userID, key, sl.cat, sl.sub); err != nil {
			return err
		}
		if c > 0 {
			if _, err := tx.Exec(`INSERT INTO track_hours (user_id, week, category, subcat_id, centihours) VALUES (?, ?, ?, ?, ?)`,
				userID, key, sl.cat, subID, c); err != nil {
				return err
			}
		}
	}
	return pruneWeek(tx, userID, key)
}

var (
	hmRe  = regexp.MustCompile(`^(-?)(\d{1,3}):([0-5]?\d)$`)
	decRe = regexp.MustCompile(`^-?(\d+(\.\d*)?|\.\d+)$`)
)

// parseHoursValue reads an hours value — a JSON number or a string "1.5" / "1:30" / "-0:45" —
// as hundredths of an hour, rounded to the nearest hundredth (36 seconds). Its size is at most
// 168 hours; negative values only where neg is allowed (add).
func parseHoursValue(raw json.RawMessage, neg bool) (int64, error) {
	bad := errors.New(`must be a number of hours (1.5) or an "h:mm" string ("1:30")`)
	var v float64
	var str string
	if err := json.Unmarshal(raw, &v); err != nil {
		if err := json.Unmarshal(raw, &str); err != nil {
			return 0, bad
		}
		str = strings.TrimSpace(str)
		if m := hmRe.FindStringSubmatch(str); m != nil {
			h, _ := strconv.Atoi(m[2])
			mins, _ := strconv.Atoi(m[3])
			v = float64(h) + float64(mins)/60
			if m[1] == "-" {
				v = -v
			}
		} else if decRe.MatchString(str) {
			if v, err = strconv.ParseFloat(str, 64); err != nil {
				return 0, bad
			}
		} else {
			return 0, bad
		}
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, bad
	}
	c := int64(math.Round(v * 100))
	if c < 0 && !neg {
		return 0, errors.New("can't be negative (use add with a negative value to subtract)")
	}
	if c > maxWeekCents || c < -maxWeekCents {
		return 0, errors.New("must be at most 168 hours")
	}
	return c, nil
}

// fmtCents shows hundredths of an hour as hours ("1.5", "-0.25").
func fmtCents(c int64) string { return strconv.FormatFloat(float64(c)/100, 'f', -1, 64) }

// PATCH /v1/api/track/weeks/{key}/hours {"entries":[{category, subcat|subcat_id, set|add}, …]}
// changes some hour rows of a past or current week and nothing else (scores, summary, ratings,
// goals and other rows stay as they are). Entries apply in order, all or nothing; the results
// must stay within 0–168 hours per row and 168 per category. A row set to 0 is removed.
func (s *Server) apiPatchHours(w http.ResponseWriter, r *http.Request, u *user) {
	k, ok := s.pathWeek(w, r)
	if !ok {
		return
	}
	var in struct {
		Entries []struct {
			Category string          `json:"category"`
			Subcat   *string         `json:"subcat"`
			SubcatID *int64          `json:"subcat_id"`
			Set      json.RawMessage `json:"set"`
			Add      json.RawMessage `json:"add"`
		} `json:"entries"`
	}
	if !decode(w, r, maxBody, &in) {
		return
	}
	if len(in.Entries) == 0 || len(in.Entries) > maxHourEntries {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf(`send 1-%d entries: {"entries":[{"category":"active","set":1.5}, …]}`, maxHourEntries))
		return
	}
	type change struct {
		cat     string
		name    string // sub-category by name, or
		id      int64  // by id (0 with name "" = the unassigned bucket)
		add     bool
		cents   int64
		display string // for messages: "active", "active / Anime"
	}
	changes := make([]change, len(in.Entries))
	for i, e := range in.Entries {
		bad := func(msg string) { writeErr(w, http.StatusBadRequest, fmt.Sprintf("entries[%d]: %s", i, msg)) }
		if catIndex(e.Category) < 0 {
			bad(fmt.Sprintf("unknown category %q", e.Category))
			return
		}
		c := change{cat: e.Category, display: e.Category}
		switch {
		case e.Subcat != nil && e.SubcatID != nil:
			bad("give subcat (a name) or subcat_id, not both")
			return
		case e.Subcat != nil:
			c.name = strings.TrimSpace(*e.Subcat)
			if c.name == "" {
				bad("subcat is empty (leave it out for the category's unassigned hours)")
				return
			}
			c.display += " / " + c.name
		case e.SubcatID != nil:
			c.id = *e.SubcatID
			if c.id <= 0 {
				bad("subcat_id must be a positive integer")
				return
			}
			c.display += " / " + strconv.FormatInt(c.id, 10)
		}
		set, add := len(e.Set) > 0 && string(e.Set) != "null", len(e.Add) > 0 && string(e.Add) != "null"
		if set == add {
			bad("give exactly one of set (the new value) or add (a change, may be negative)")
			return
		}
		raw := e.Set
		if add {
			raw, c.add = e.Add, true
		}
		var err error
		if c.cents, err = parseHoursValue(raw, add); err != nil {
			field := "set "
			if add {
				field = "add "
			}
			bad(field + err.Error())
			return
		}
		changes[i] = c
	}
	key := k.String()
	err := s.tx(r.Context(), func(tx *sql.Tx) error {
		type sub struct {
			id       int64
			cat      string
			name     string
			archived bool
		}
		var subs []sub
		rows, err := tx.Query(`SELECT id, category, name, archived FROM track_subcats WHERE user_id = ?`, u.ID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var x sub
			if err := rows.Scan(&x.id, &x.cat, &x.name, &x.archived); err != nil {
				rows.Close()
				return err
			}
			subs = append(subs, x)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		hours, err := weekHoursTx(tx, u.ID, key)
		if err != nil {
			return err
		}
		before := maps.Clone(hours)
		for i, c := range changes {
			sl := hourSlot{c.cat, c.id}
			switch {
			case c.name != "":
				// Active sub-categories of the category, by name ignoring case; an exact match
				// wins if folding makes two of them equal.
				var exact, folded []int64
				for _, x := range subs {
					if x.cat == c.cat && !x.archived && strings.EqualFold(x.name, c.name) {
						folded = append(folded, x.id)
						if x.name == c.name {
							exact = append(exact, x.id)
						}
					}
				}
				switch {
				case len(exact) == 1:
					sl.sub = exact[0]
				case len(folded) == 1:
					sl.sub = folded[0]
				case len(folded) == 0:
					return errStatus{http.StatusBadRequest, fmt.Sprintf(
						"entries[%d]: you have no sub-category named %q in %s; create it first with POST /v1/api/track/subcats {\"category\":%q,\"name\":%q}",
						i, c.name, c.cat, c.cat, c.name)}
				default:
					return errStatus{http.StatusBadRequest, fmt.Sprintf("entries[%d]: more than one %s sub-category matches %q; use subcat_id", i, c.cat, c.name)}
				}
			case c.id != 0:
				found := false
				for _, x := range subs {
					found = found || x.id == c.id && x.cat == c.cat
				}
				if !found {
					return errStatus{http.StatusBadRequest, fmt.Sprintf("entries[%d]: sub-category %d is not one of your %s sub-categories", i, c.id, c.cat)}
				}
			}
			v := c.cents
			if c.add {
				v += hours[sl]
			}
			switch {
			case v < 0:
				return errStatus{http.StatusBadRequest, fmt.Sprintf("entries[%d]: %s has %s h; adding %s h would go below zero",
					i, c.display, fmtCents(hours[sl]), fmtCents(c.cents))}
			case v > maxWeekCents:
				return errStatus{http.StatusBadRequest, fmt.Sprintf("entries[%d]: %s would be %s h; at most 168 h", i, c.display, fmtCents(v))}
			}
			hours[sl] = v
		}
		for _, c := range trackCategories {
			if t := catCents(hours, c.ID); t > maxWeekCents {
				return errStatus{http.StatusBadRequest, fmt.Sprintf("%s would add up to %s h in this week; at most 168 h per category",
					c.ID, fmtCents(t))}
			}
		}
		return writeWeekHoursTx(tx, u.ID, key, s.now().Unix(), before, hours)
	})
	if err != nil {
		writeTxErr(w, "patch hours", err)
		return
	}
	s.writeAPIWeek(w, r, u.ID, k)
}

// PUT /v1/api/track/weeks/{key}/summary {"summary":"…","private":false} sets the summary (the
// site's limits) and, if given, its private flag; nothing else. Omitting private keeps the
// current setting (public for a new week).
func (s *Server) apiPutSummary(w http.ResponseWriter, r *http.Request, u *user) {
	k, ok := s.pathWeek(w, r)
	if !ok {
		return
	}
	var in struct {
		Summary *string `json:"summary"`
		Private *bool   `json:"private"`
	}
	if !decode(w, r, maxBody, &in) {
		return
	}
	if in.Summary == nil {
		writeErr(w, http.StatusBadRequest, `send {"summary":"…","private":false} ("" clears the summary)`)
		return
	}
	summary := cleanBody(*in.Summary)
	if utf8.RuneCountInString(summary) > maxSummaryRunes {
		writeErr(w, http.StatusBadRequest, "summary must be at most 10,000 characters")
		return
	}
	key := k.String()
	err := s.tx(r.Context(), func(tx *sql.Tx) error {
		if err := touchWeek(tx, u.ID, key, s.now().Unix()); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE track_weeks SET summary = ?, summary_private = COALESCE(?, summary_private) WHERE user_id = ? AND week = ?`,
			summary, in.Private, u.ID, key); err != nil {
			return err
		}
		return pruneWeek(tx, u.ID, key)
	})
	if err != nil {
		writeTxErr(w, "save summary", err)
		return
	}
	s.writeAPIWeek(w, r, u.ID, k)
}
