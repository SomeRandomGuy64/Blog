package api

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strconv"
	"time"
	"unicode/utf8"

	"fazon.nexus/backend/internal/week"
)

const (
	maxWeekCents    = 168 * 100 // hours in a week, per category
	maxSummaryRunes = 10000
)

// scores are a week's 7 daily percentages; nil = no value that day.
type scores [week.Days]*float64

// mean is the average of the non-nil days, or nil.
func (sc scores) mean() *float64 {
	var sum float64
	n := 0
	for _, v := range sc {
		if v != nil {
			sum += *v
			n++
		}
	}
	if n == 0 {
		return nil
	}
	m := sum / float64(n)
	return &m
}

func (sc scores) empty() bool { return sc.mean() == nil }

func round1(p *float64) *float64 {
	if p == nil {
		return nil
	}
	v := math.Round(*p*10) / 10
	return &v
}

// weekData is one logged week (a track_weeks row) with its hours and Kyōtsu results.
type weekData struct {
	key            week.Key
	cents          map[string]map[int64]int64 // category → subcat id (0 = unassigned) → hundredths of an hour
	kotu, kyotsu   scores                     // manual entries
	auto           scores                     // from kyotsu_sessions
	autoAnswers    [week.Days]int
	ratings        map[string]int // category → 1–4 (😞 😐 👌 🔥); unrated categories absent
	summary        string
	summaryPrivate bool
	updated        int64
}

// kyotsuEffective is the manual value per day, falling back to the trainer's results.
func (d *weekData) kyotsuEffective() scores {
	out := d.kyotsu
	for i, v := range out {
		if v == nil {
			out[i] = d.auto[i]
		}
	}
	return out
}

// kyotsuSource says where each effective Kyōtsu day value comes from: "manual", "auto" (the
// trainer) or nil (no value).
func (d *weekData) kyotsuSource() [week.Days]*string {
	var out [week.Days]*string
	manual, auto := "manual", "auto"
	for i := range out {
		switch {
		case d.kyotsu[i] != nil:
			out[i] = &manual
		case d.auto[i] != nil:
			out[i] = &auto
		}
	}
	return out
}

// ratingsOut is a copy of the ratings for a response: never nil, unrated categories absent.
func (d *weekData) ratingsOut() map[string]int {
	out := make(map[string]int, len(d.ratings))
	for c, v := range d.ratings {
		out[c] = v
	}
	return out
}

func (d *weekData) total(cat string) int64 {
	var t int64
	for _, c := range d.cents[cat] {
		t += c
	}
	return t
}

// allCents is every category's hours together; the "all" metric. "nopassive" is this minus
// passive.
func (d *weekData) allCents() int64 {
	var t int64
	for _, c := range trackCategories {
		t += d.total(c.ID)
	}
	return t
}

// totals has every category (0 when nothing was logged), in hours.
func (d *weekData) totals() map[string]float64 {
	out := make(map[string]float64, len(trackCategories))
	for _, c := range trackCategories {
		out[c.ID] = float64(d.total(c.ID)) / 100
	}
	return out
}

func (d *weekData) subtotals() map[string]map[string]float64 {
	out := map[string]map[string]float64{}
	for cat, m := range d.cents {
		sub := map[string]float64{}
		for id, c := range m {
			k := "none"
			if id != 0 {
				k = strconv.FormatInt(id, 10)
			}
			sub[k] = float64(c) / 100
		}
		out[cat] = sub
	}
	return out
}

// weekSummary is a week in the list endpoints. Summary and day fields are set per endpoint.
type weekSummary struct {
	Week           string                        `json:"week"`
	Totals         map[string]float64            `json:"totals"`
	Subtotals      map[string]map[string]float64 `json:"subtotals"`
	Kotu           *float64                      `json:"kotu"`
	Kyotsu         *float64                      `json:"kyotsu"`
	Ratings        map[string]int                `json:"ratings"`
	HasSummary     *bool                         `json:"has_summary,omitempty"`
	Summary        *string                       `json:"summary,omitempty"`
	SummaryPrivate *bool                         `json:"summary_private,omitempty"`
	*weekDays
}

// weekDays is a week's day-by-day Kotu and (effective) Kyōtsu scores, for profile charts.
type weekDays struct {
	Days         []string           `json:"days"` // label dates, as in week.DayLabels
	KotuDays     scores             `json:"kotu_days"`
	KyotsuDays   scores             `json:"kyotsu_days"`
	KyotsuSource [week.Days]*string `json:"kyotsu_source"`
}

func (d *weekData) listItem() weekSummary {
	return weekSummary{Week: d.key.String(), Totals: d.totals(), Subtotals: d.subtotals(),
		Kotu: round1(d.kotu.mean()), Kyotsu: round1(d.kyotsuEffective().mean()), Ratings: d.ratingsOut()}
}

func (d *weekData) days() *weekDays {
	return &weekDays{Days: d.key.DayLabels(), KotuDays: d.kotu, KyotsuDays: d.kyotsuEffective(), KyotsuSource: d.kyotsuSource()}
}

func idsJSON(ids []int64) string {
	b, _ := json.Marshal(ids)
	return string(b)
}

func parseScores(raw string) (sc scores, err error) {
	var vals []*float64
	if err := json.Unmarshal([]byte(raw), &vals); err != nil {
		return sc, err
	}
	copy(sc[:], vals)
	return sc, nil
}

// loadWeeks returns the logged weeks in from..to for each user, oldest first, with hours,
// ratings and Kyōtsu auto scores filled in. Four queries, whatever the number of users.
func (s *Server) loadWeeks(ctx context.Context, userIDs []int64, from, to week.Key) (map[int64][]*weekData, error) {
	out := map[int64][]*weekData{}
	if len(userIDs) == 0 {
		return out, nil
	}
	ids, f, t := idsJSON(userIDs), from.String(), to.String()
	type uw struct {
		user int64
		week string
	}
	index := map[uw]*weekData{}
	rows, err := s.db.QueryContext(ctx, `SELECT user_id, week, kotu, kyotsu, summary, summary_private, updated_at
		FROM track_weeks WHERE user_id IN (SELECT value FROM json_each(?)) AND week BETWEEN ? AND ?
		ORDER BY user_id, week`, ids, f, t)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var user int64
		var key, kotu, kyotsu string
		d := &weekData{cents: map[string]map[int64]int64{}, ratings: map[string]int{}}
		if err := rows.Scan(&user, &key, &kotu, &kyotsu, &d.summary, &d.summaryPrivate, &d.updated); err != nil {
			return nil, err
		}
		if d.key, err = week.Parse(key, time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)); err != nil {
			return nil, fmt.Errorf("stored week %q: %w", key, err)
		}
		if d.kotu, err = parseScores(kotu); err != nil {
			return nil, err
		}
		if d.kyotsu, err = parseScores(kyotsu); err != nil {
			return nil, err
		}
		out[user] = append(out[user], d)
		index[uw{user, key}] = d
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()

	rows, err = s.db.QueryContext(ctx, `SELECT user_id, week, category, IFNULL(subcat_id, 0), centihours
		FROM track_hours WHERE user_id IN (SELECT value FROM json_each(?)) AND week BETWEEN ? AND ?`, ids, f, t)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var user, sub, cents int64
		var key, cat string
		if err := rows.Scan(&user, &key, &cat, &sub, &cents); err != nil {
			return nil, err
		}
		if d := index[uw{user, key}]; d != nil {
			if d.cents[cat] == nil {
				d.cents[cat] = map[int64]int64{}
			}
			d.cents[cat][sub] = cents
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()

	rows, err = s.db.QueryContext(ctx, `SELECT user_id, week, category, rating
		FROM track_ratings WHERE user_id IN (SELECT value FROM json_each(?)) AND week BETWEEN ? AND ?`, ids, f, t)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var user int64
		var key, cat string
		var rating int
		if err := rows.Scan(&user, &key, &cat, &rating); err != nil {
			return nil, err
		}
		if d := index[uw{user, key}]; d != nil {
			d.ratings[cat] = rating
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()

	err = s.kyotsuAuto(ctx, ids, from, to, func(user int64, k week.Key) *weekData { return index[uw{user, k.String()}] })
	return out, err
}

// kyotsuAuto adds each trainer session ending in from..to to its day in the week that target
// returns (sessions in weeks it returns nil for are skipped), then turns the sums into
// percentages.
func (s *Server) kyotsuAuto(ctx context.Context, ids string, from, to week.Key, target func(int64, week.Key) *weekData) error {
	rows, err := s.db.QueryContext(ctx, `SELECT user_id, ended_at, total, correct FROM kyotsu_sessions
		WHERE user_id IN (SELECT value FROM json_each(?)) AND ended_at >= ? AND ended_at < ?`,
		ids, from.Start().Unix(), to.End().Unix())
	if err != nil {
		return err
	}
	defer rows.Close()
	type acc struct{ total, correct [week.Days]int }
	sums := map[*weekData]*acc{}
	for rows.Next() {
		var user, ended int64
		var total, correct int
		if err := rows.Scan(&user, &ended, &total, &correct); err != nil {
			return err
		}
		t := time.Unix(ended, 0)
		k := week.Of(t)
		d := target(user, k)
		if d == nil {
			continue
		}
		a := sums[d]
		if a == nil {
			a = &acc{}
			sums[d] = a
		}
		i := k.DayIndex(t)
		a.total[i] += total
		a.correct[i] += correct
	}
	for d, a := range sums {
		for i := range a.total {
			d.autoAnswers[i] = a.total[i]
			if a.total[i] > 0 {
				d.auto[i] = round1(new(float64(a.correct[i]) / float64(a.total[i]) * 100))
			}
		}
	}
	return rows.Err()
}

// ---- own weeks ----

type hourJSON struct {
	Category string  `json:"category"`
	Subcat   *int64  `json:"subcat"`
	Hours    float64 `json:"hours"`
}

type weekDoc struct {
	Week              string             `json:"week"`
	Start             string             `json:"start"`
	End               string             `json:"end"`
	Days              []string           `json:"days"`
	Hours             []hourJSON         `json:"hours"`
	Kotu              scores             `json:"kotu"`
	Kyotsu            scores             `json:"kyotsu"`
	KyotsuAuto        scores             `json:"kyotsu_auto"`
	KyotsuAutoAnswers [week.Days]int     `json:"kyotsu_auto_answers"`
	Ratings           map[string]int     `json:"ratings"`
	Goals             map[string]float64 `json:"goals"`      // owner only; write-once, see setGoals
	GoalsOpen         bool               `json:"goals_open"` // goals can be set: this is the current week
	Summary           string             `json:"summary"`
	SummaryPrivate    bool               `json:"summary_private"`
	Updated           *string            `json:"updated"`
}

// weekDoc is the full week for its owner; an unlogged week still gets its Kyōtsu auto scores.
func (s *Server) weekDoc(ctx context.Context, userID int64, k week.Key) (weekDoc, error) {
	m, err := s.loadWeeks(ctx, []int64{userID}, k, k)
	if err != nil {
		return weekDoc{}, err
	}
	doc := weekDoc{Week: k.String(), Start: k.Start().Format(time.RFC3339), End: k.End().Format(time.RFC3339),
		Days: k.DayLabels(), Hours: []hourJSON{}}
	d := &weekData{key: k}
	if len(m[userID]) == 1 {
		d = m[userID][0]
		up := rfc3339(d.updated)
		doc.Updated = &up
	} else if err := s.kyotsuAuto(ctx, idsJSON([]int64{userID}), k, k, func(int64, week.Key) *weekData { return d }); err != nil {
		return weekDoc{}, err
	}
	for cat, subs := range d.cents {
		for sub, c := range subs {
			h := hourJSON{Category: cat, Hours: float64(c) / 100}
			if sub != 0 {
				h.Subcat = &sub
			}
			doc.Hours = append(doc.Hours, h)
		}
	}
	slices.SortFunc(doc.Hours, func(a, b hourJSON) int {
		if c := catIndex(a.Category) - catIndex(b.Category); c != 0 {
			return c
		}
		// Unassigned last, like the form.
		ai, bi := int64(math.MaxInt64), int64(math.MaxInt64)
		if a.Subcat != nil {
			ai = *a.Subcat
		}
		if b.Subcat != nil {
			bi = *b.Subcat
		}
		return cmp.Compare(ai, bi)
	})
	doc.Kotu, doc.Kyotsu, doc.KyotsuAuto, doc.KyotsuAutoAnswers = d.kotu, d.kyotsu, d.auto, d.autoAnswers
	doc.Ratings = d.ratingsOut()
	doc.GoalsOpen = k == week.Of(s.now())
	if doc.Goals, err = s.goals(ctx, s.db, userID, k); err != nil {
		return weekDoc{}, err
	}
	doc.Summary, doc.SummaryPrivate = d.summary, d.summaryPrivate
	return doc, nil
}

// pathWeek parses {key}; false after a 400.
func (s *Server) pathWeek(w http.ResponseWriter, r *http.Request) (week.Key, bool) {
	k, err := week.Parse(r.PathValue("key"), s.now())
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return week.Key{}, false
	}
	return k, true
}

func (s *Server) getWeek(w http.ResponseWriter, r *http.Request, u *user) {
	k, ok := s.pathWeek(w, r)
	if !ok {
		return
	}
	doc, err := s.weekDoc(r.Context(), u.ID, k)
	if err != nil {
		internalErr(w, "load week", err)
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

// validScores checks a submitted day array: absent, or exactly 7 entries of null or 0–100
// with at most one decimal.
func validScores(in []*float64) (scores, bool) {
	var sc scores
	if in == nil {
		return sc, true
	}
	if len(in) != week.Days {
		return sc, false
	}
	for i, v := range in {
		if v != nil && (*v < 0 || *v > 100 || !hasDecimals(*v, 1)) {
			return sc, false
		}
		sc[i] = v
	}
	return sc, true
}

// hasDecimals reports whether v has at most n decimal places (allowing for float noise).
func hasDecimals(v float64, n int) bool {
	p := math.Pow10(n)
	return math.Abs(v*p-math.Round(v*p)) < 1e-6
}

func (s *Server) saveWeek(w http.ResponseWriter, r *http.Request, u *user) {
	k, ok := s.pathWeek(w, r)
	if !ok {
		return
	}
	var in struct {
		Hours          []hourJSON      `json:"hours"`
		Kotu           []*float64      `json:"kotu"`
		Kyotsu         []*float64      `json:"kyotsu"`
		Ratings        map[string]*int `json:"ratings"`
		Summary        string          `json:"summary"`
		SummaryPrivate bool            `json:"summary_private"`
	}
	if !decode(w, r, maxBody, &in) {
		return
	}
	ratings := map[string]int{}
	for cat, v := range in.Ratings {
		if catIndex(cat) < 0 {
			writeErr(w, http.StatusBadRequest, fmt.Sprintf("ratings: unknown category %q", cat))
			return
		}
		if v == nil {
			continue // null = not rated
		}
		if *v < 1 || *v > 4 {
			writeErr(w, http.StatusBadRequest, "ratings must be 1-4 (1 = 😞, 2 = 😐, 3 = 👌, 4 = 🔥) or null")
			return
		}
		ratings[cat] = *v
	}
	kotu, ok1 := validScores(in.Kotu)
	kyotsu, ok2 := validScores(in.Kyotsu)
	if !ok1 || !ok2 {
		writeErr(w, http.StatusBadRequest, "kotu and kyotsu must be 7 values, each null or 0-100 with at most 1 decimal")
		return
	}
	summary := cleanBody(in.Summary)
	if utf8.RuneCountInString(summary) > maxSummaryRunes {
		writeErr(w, http.StatusBadRequest, "summary must be at most 10,000 characters")
		return
	}
	if len(in.Hours) > len(trackCategories)*(maxAllSubcats+1) {
		writeErr(w, http.StatusBadRequest, "too many hour entries")
		return
	}
	type slot struct {
		cat string
		sub int64
	}
	cents := map[slot]int64{}
	catTotal := map[string]int64{}
	for _, h := range in.Hours {
		if catIndex(h.Category) < 0 {
			writeErr(w, http.StatusBadRequest, fmt.Sprintf("unknown category %q", h.Category))
			return
		}
		if h.Hours < 0 || h.Hours > 168 || !hasDecimals(h.Hours, 2) {
			writeErr(w, http.StatusBadRequest, "hours must be 0-168 with at most 2 decimals")
			return
		}
		sl := slot{h.Category, 0}
		if h.Subcat != nil {
			sl.sub = *h.Subcat
		}
		if _, dup := cents[sl]; dup {
			writeErr(w, http.StatusBadRequest, "the same category/sub-category appears twice")
			return
		}
		c := int64(math.Round(h.Hours * 100))
		cents[sl] = c
		catTotal[h.Category] += c
		if catTotal[h.Category] > maxWeekCents {
			writeErr(w, http.StatusBadRequest, fmt.Sprintf("%s adds up to more than 168 hours", h.Category))
			return
		}
	}
	if !s.weekLimit.allow(strconv.FormatInt(u.ID, 10), s.now()) {
		writeErr(w, http.StatusTooManyRequests, "too many saves, slow down")
		return
	}
	key := k.String()
	err := s.tx(r.Context(), func(tx *sql.Tx) error {
		owned := map[int64]string{}
		rows, err := tx.Query(`SELECT id, category FROM track_subcats WHERE user_id = ?`, u.ID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id int64
			var cat string
			if err := rows.Scan(&id, &cat); err != nil {
				rows.Close()
				return err
			}
			owned[id] = cat
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		hasHours := false
		for sl, c := range cents {
			if sl.sub != 0 && owned[sl.sub] != sl.cat {
				return errStatus{http.StatusBadRequest, fmt.Sprintf("sub-category %d is not one of your %s sub-categories", sl.sub, sl.cat)}
			}
			hasHours = hasHours || c > 0
		}
		if !hasHours && kotu.empty() && kyotsu.empty() && summary == "" && len(ratings) == 0 {
			_, err := tx.Exec(`DELETE FROM track_weeks WHERE user_id = ? AND week = ?`, u.ID, key)
			return err
		}
		kotuJSON, _ := json.Marshal(kotu)
		kyotsuJSON, _ := json.Marshal(kyotsu)
		if _, err := tx.Exec(`INSERT INTO track_weeks (user_id, week, kotu, kyotsu, summary, summary_private, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (user_id, week) DO UPDATE SET kotu = excluded.kotu, kyotsu = excluded.kyotsu,
				summary = excluded.summary, summary_private = excluded.summary_private, updated_at = excluded.updated_at`,
			u.ID, key, string(kotuJSON), string(kyotsuJSON), summary, in.SummaryPrivate, s.now().Unix()); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM track_hours WHERE user_id = ? AND week = ?`, u.ID, key); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM track_ratings WHERE user_id = ? AND week = ?`, u.ID, key); err != nil {
			return err
		}
		for cat, v := range ratings {
			if _, err := tx.Exec(`INSERT INTO track_ratings (user_id, week, category, rating) VALUES (?, ?, ?, ?)`,
				u.ID, key, cat, v); err != nil {
				return err
			}
		}
		for sl, c := range cents {
			if c == 0 {
				continue // zero rows are the same as no row
			}
			var sub any
			if sl.sub != 0 {
				sub = sl.sub
			}
			if _, err := tx.Exec(`INSERT INTO track_hours (user_id, week, category, subcat_id, centihours) VALUES (?, ?, ?, ?, ?)`,
				u.ID, key, sl.cat, sub, c); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		writeTxErr(w, "save week", err)
		return
	}
	doc, err := s.weekDoc(r.Context(), u.ID, k)
	if err != nil {
		internalErr(w, "load week", err)
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

// queryWeek parses an optional week-key query parameter (def when absent); false after a 400.
func (s *Server) queryWeek(w http.ResponseWriter, r *http.Request, name string, def week.Key) (week.Key, bool) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return def, true
	}
	k, err := week.Parse(v, s.now())
	if err != nil {
		writeErr(w, http.StatusBadRequest, name+": "+err.Error())
		return week.Key{}, false
	}
	return k, true
}

func (s *Server) listWeeks(w http.ResponseWriter, r *http.Request, u *user) {
	to, ok := s.queryWeek(w, r, "to", week.Of(s.now()))
	if !ok {
		return
	}
	from, ok := s.queryWeek(w, r, "from", to.Add(-51))
	if !ok {
		return
	}
	if to.Before(from) {
		writeErr(w, http.StatusBadRequest, "from is after to")
		return
	}
	m, err := s.loadWeeks(r.Context(), []int64{u.ID}, from, to)
	if err != nil {
		internalErr(w, "list weeks", err)
		return
	}
	out := []weekSummary{}
	for _, d := range m[u.ID] {
		item := d.listItem()
		has := d.summary != ""
		item.HasSummary = &has
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"weeks": out})
}
