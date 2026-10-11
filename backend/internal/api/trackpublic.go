package api

import (
	"cmp"
	"database/sql"
	"errors"
	"net/http"
	"slices"
	"strconv"

	"fazon.nexus/backend/internal/week"
)

// trackVisible loads whether userID exists and may be shown to the requester: hidden
// (track_public = 0) users are visible only to themselves.
func (s *Server) trackVisible(w http.ResponseWriter, r *http.Request, userID int64) (owner, ok bool) {
	viewer, err := s.session(w, r)
	if err != nil {
		internalErr(w, "load session", err)
		return false, false
	}
	var public bool
	err = s.db.QueryRowContext(r.Context(), `SELECT track_public FROM users WHERE id = ?`, userID).Scan(&public)
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "not found")
		return false, false
	}
	if err != nil {
		internalErr(w, "load user", err)
		return false, false
	}
	owner = viewer != nil && viewer.ID == userID
	if !public && !owner {
		writeErr(w, http.StatusNotFound, "not found")
		return false, false
	}
	return owner, true
}

// userTrack is a profile's track history: all weeks, oldest first. Summaries are included
// unless private; the owner always gets theirs, with the private flag.
func (s *Server) userTrack(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	owner, ok := s.trackVisible(w, r, id)
	if !ok {
		return
	}
	ctx := r.Context()
	u, err := s.publicUser(ctx, id)
	if err != nil {
		internalErr(w, "load user", err)
		return
	}
	all, err := s.userSubcats(r, id)
	if err != nil {
		internalErr(w, "list subcats", err)
		return
	}
	type sub struct {
		ID       int64  `json:"id"`
		Category string `json:"category"`
		Name     string `json:"name"`
	}
	subcats := []sub{}
	for _, c := range all { // archived ones too: their hours still show in past weeks
		subcats = append(subcats, sub{c.ID, c.Category, c.Name})
	}
	m, err := s.loadWeeks(ctx, []int64{id}, week.Earliest, week.Of(s.now()))
	if err != nil {
		internalErr(w, "load weeks", err)
		return
	}
	weeks := []weekSummary{}
	for _, d := range m[id] {
		item := d.listItem()
		summary := d.summary
		if d.summaryPrivate && !owner {
			summary = ""
		}
		item.Summary = &summary
		if owner {
			item.SummaryPrivate = &d.summaryPrivate
		}
		weeks = append(weeks, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": u, "subcats": subcats, "weeks": weeks})
}

var leaderboardWeeks = map[string]int{"week": 1, "4w": 4, "12w": 12, "52w": 52, "all": 0}

type leaderboardEntry struct {
	User        *publicUser           `json:"user"`
	Totals      map[string]float64    `json:"totals"`
	Kotu        *float64              `json:"kotu"`
	Kyotsu      *float64              `json:"kyotsu"`
	WeeksLogged int                   `json:"weeks_logged"`
	Series      map[string][]*float64 `json:"series"`
	cents       map[string]int64      // exact totals, for sorting
}

func (s *Server) leaderboard(w http.ResponseWriter, r *http.Request) {
	period := r.URL.Query().Get("period")
	if period == "" {
		period = "4w"
	}
	n, ok := leaderboardWeeks[period]
	if !ok {
		writeErr(w, http.StatusBadRequest, "period must be week, 4w, 12w, 52w or all")
		return
	}
	to, ok := s.queryWeek(w, r, "week", week.Of(s.now()))
	if !ok {
		return
	}
	ctx := r.Context()
	from := to.Add(1 - n)
	if period == "all" {
		from = to
		var first sql.NullString
		if err := s.db.QueryRowContext(ctx, `SELECT MIN(w.week) FROM track_weeks w JOIN users u ON u.id = w.user_id
			WHERE u.track_public = 1 AND w.week <= ?`, to.String()).Scan(&first); err != nil {
			internalErr(w, "leaderboard range", err)
			return
		}
		if first.Valid {
			k, err := week.Parse(first.String, s.now())
			if err == nil && k.Before(to) {
				from = k
			}
		}
	}
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT w.user_id FROM track_weeks w JOIN users u ON u.id = w.user_id
		WHERE u.track_public = 1 AND w.week BETWEEN ? AND ?`, from.String(), to.String())
	if err != nil {
		internalErr(w, "leaderboard users", err)
		return
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			internalErr(w, "leaderboard users", err)
			return
		}
		ids = append(ids, id)
	}
	rows.Close()
	data, err := s.loadWeeks(ctx, ids, from, to)
	if err != nil {
		internalErr(w, "leaderboard weeks", err)
		return
	}
	users, err := s.publicUsers(ctx, ids)
	if err != nil {
		internalErr(w, "leaderboard users", err)
		return
	}
	keys := week.Range(from, to)
	pos := make(map[week.Key]int, len(keys))
	weekNames := make([]string, len(keys))
	for i, k := range keys {
		pos[k] = i
		weekNames[i] = k.String()
	}
	metrics := []string{"immersion", "kotu", "kyotsu"}
	for _, c := range trackCategories {
		metrics = append(metrics, c.ID)
	}
	entries := []leaderboardEntry{}
	for _, id := range ids {
		e := leaderboardEntry{User: users[id], Totals: map[string]float64{}, Series: map[string][]*float64{},
			cents: map[string]int64{}}
		for _, m := range metrics {
			e.Series[m] = make([]*float64, len(keys))
		}
		var kotuSum, kyotsuSum float64
		var kotuN, kyotsuN int
		for _, d := range data[id] {
			i := pos[d.key]
			e.WeeksLogged++
			for _, c := range trackCategories {
				t := d.total(c.ID)
				e.cents[c.ID] += t
				e.Series[c.ID][i] = new(float64(t) / 100)
			}
			imm := d.total("active") + d.total("passive")
			e.Series["immersion"][i] = new(float64(imm) / 100)
			if m := d.kotu.mean(); m != nil {
				kotuSum += *m
				kotuN++
				e.Series["kotu"][i] = round1(m)
			}
			if m := d.kyotsuEffective().mean(); m != nil {
				kyotsuSum += *m
				kyotsuN++
				e.Series["kyotsu"][i] = round1(m)
			}
		}
		for _, c := range trackCategories {
			e.Totals[c.ID] = float64(e.cents[c.ID]) / 100
		}
		e.cents["immersion"] = e.cents["active"] + e.cents["passive"]
		e.Totals["immersion"] = float64(e.cents["immersion"]) / 100
		if kotuN > 0 {
			e.Kotu = round1(new(kotuSum / float64(kotuN)))
		}
		if kyotsuN > 0 {
			e.Kyotsu = round1(new(kyotsuSum / float64(kyotsuN)))
		}
		entries = append(entries, e)
	}
	slices.SortFunc(entries, func(a, b leaderboardEntry) int {
		return cmp.Or(cmp.Compare(b.cents["immersion"], a.cents["immersion"]), cmp.Compare(b.WeeksLogged, a.WeeksLogged),
			cmp.Compare(a.User.ID, b.User.ID))
	})
	writeJSON(w, http.StatusOK, map[string]any{"period": period, "from": from.String(), "to": to.String(),
		"weeks": weekNames, "users": entries})
}

type summaryItem struct {
	User    *publicUser        `json:"user"`
	Week    string             `json:"week"`
	Summary string             `json:"summary"`
	Totals  map[string]float64 `json:"totals"`
	Kotu    *float64           `json:"kotu"`
	Kyotsu  *float64           `json:"kyotsu"`
	Updated string             `json:"updated"`
}

// summaries is the public feed of weekly summaries, newest week first. A page never splits a
// week: if the limit lands inside one, the rest of that week comes too, so paging with
// before=<last week> loses nothing.
func (s *Server) summaries(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := 20
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			writeErr(w, http.StatusBadRequest, "invalid limit")
			return
		}
		limit = min(n, 50)
	}
	var userID int64
	if v := q.Get("user"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 1 {
			writeErr(w, http.StatusBadRequest, "invalid user")
			return
		}
		userID = n
	}
	before := ""
	if v := q.Get("before"); v != "" {
		// Exclusive bound, so next week's key is fine too.
		k, err := week.Parse(v, s.now().Add(week.Length))
		if err != nil {
			writeErr(w, http.StatusBadRequest, "before: "+err.Error())
			return
		}
		before = k.String()
	}
	ctx := r.Context()
	const base = `SELECT w.user_id, w.week FROM track_weeks w JOIN users u ON u.id = w.user_id
		WHERE u.track_public = 1 AND w.summary != '' AND w.summary_private = 0 AND (?1 = 0 OR w.user_id = ?1)`
	type uw struct {
		user int64
		week string
	}
	scan := func(rows *sql.Rows, err error) ([]uw, error) {
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []uw
		for rows.Next() {
			var x uw
			if err := rows.Scan(&x.user, &x.week); err != nil {
				return nil, err
			}
			out = append(out, x)
		}
		return out, rows.Err()
	}
	page, err := scan(s.db.QueryContext(ctx, base+` AND (?2 = '' OR w.week < ?2)
		ORDER BY w.week DESC, w.updated_at DESC, w.user_id LIMIT ?3`, userID, before, limit))
	if err == nil && len(page) == limit {
		last := page[len(page)-1].week
		var rest []uw
		rest, err = scan(s.db.QueryContext(ctx, base+` AND w.week = ?2 ORDER BY w.updated_at DESC, w.user_id`, userID, last))
		for len(page) > 0 && page[len(page)-1].week == last {
			page = page[:len(page)-1]
		}
		page = append(page, rest...)
	}
	if err != nil {
		internalErr(w, "summaries", err)
		return
	}
	out := []summaryItem{}
	if len(page) > 0 {
		var ids []int64
		seen := map[int64]bool{}
		from, to := page[len(page)-1].week, page[0].week
		for _, x := range page {
			if !seen[x.user] {
				seen[x.user] = true
				ids = append(ids, x.user)
			}
		}
		farFuture := s.now().Add(100 * week.Length)
		fk, err1 := week.Parse(from, farFuture)
		tk, err2 := week.Parse(to, farFuture)
		if err := errors.Join(err1, err2); err != nil {
			internalErr(w, "summaries range", err)
			return
		}
		data, err := s.loadWeeks(ctx, ids, fk, tk)
		if err != nil {
			internalErr(w, "summaries weeks", err)
			return
		}
		users, err := s.publicUsers(ctx, ids)
		if err != nil {
			internalErr(w, "summaries users", err)
			return
		}
		byKey := map[uw]*weekData{}
		for id, ds := range data {
			for _, d := range ds {
				byKey[uw{id, d.key.String()}] = d
			}
		}
		for _, x := range page {
			d := byKey[x]
			if d == nil {
				continue
			}
			it := d.listItem()
			out = append(out, summaryItem{User: users[x.user], Week: x.week, Summary: d.summary, Totals: it.Totals,
				Kotu: it.Kotu, Kyotsu: it.Kyotsu, Updated: rfc3339(d.updated)})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"summaries": out})
}
