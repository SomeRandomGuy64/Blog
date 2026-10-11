package api

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strconv"

	"fazon.nexus/backend/internal/week"
)

// Goals are the hours a user aims for in a category in a week. They can only be set during that
// week and are write-once: a category's goal is never changed or cleared, though categories
// without one can still get one until the week rolls over. They are separate from the week save,
// which never touches them. While the week runs they are private to the owner; once it is
// published, the goal achievers board shows the goals of public users who met at least one.

type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// goals loads a user's goals for week k as category → hours (unset categories absent).
func (s *Server) goals(ctx context.Context, q querier, userID int64, k week.Key) (map[string]float64, error) {
	m, err := goalsRange(ctx, q, userID, k, k)
	if err != nil {
		return nil, err
	}
	if g := m[k.String()]; g != nil {
		return g, nil
	}
	return map[string]float64{}, nil
}

// goalsRange loads a user's goals for the weeks from..to as week key → category → hours (weeks
// without goals absent).
func goalsRange(ctx context.Context, q querier, userID int64, from, to week.Key) (map[string]map[string]float64, error) {
	rows, err := q.QueryContext(ctx, `SELECT week, category, centihours FROM track_goals WHERE user_id = ? AND week BETWEEN ? AND ?`,
		userID, from.String(), to.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]map[string]float64{}
	for rows.Next() {
		var wk, cat string
		var c int64
		if err := rows.Scan(&wk, &cat, &c); err != nil {
			return nil, err
		}
		if out[wk] == nil {
			out[wk] = map[string]float64{}
		}
		out[wk][cat] = float64(c) / 100
	}
	return out, rows.Err()
}

// setGoals (POST /v1/track/weeks/{key}/goals {"goals":{"active":35,…}}) sets goals for the
// categories given, all or nothing. Re-sending a goal's current value is a no-op; a different
// value for a category that already has one is a 409.
func (s *Server) setGoals(w http.ResponseWriter, r *http.Request, u *user) {
	k, ok := s.pathWeek(w, r)
	if !ok {
		return
	}
	cents, ok := s.goalsInput(w, r, k)
	if !ok {
		return
	}
	if !s.weekLimit.allow(strconv.FormatInt(u.ID, 10), s.now()) {
		writeErr(w, http.StatusTooManyRequests, "too many saves, slow down")
		return
	}
	if err := s.applyGoals(r.Context(), u.ID, k, cents); err != nil {
		writeTxErr(w, "set goals", err)
		return
	}
	doc, err := s.weekDoc(r.Context(), u.ID, k)
	if err != nil {
		internalErr(w, "load week", err)
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

// goalsInput decodes and validates a goals request for week k (which must be the current
// week): category → hundredths. False after answering 400/409/413.
func (s *Server) goalsInput(w http.ResponseWriter, r *http.Request, k week.Key) (map[string]int64, bool) {
	var in struct {
		Goals map[string]*float64 `json:"goals"`
	}
	if !decode(w, r, maxBody, &in) {
		return nil, false
	}
	if len(in.Goals) == 0 {
		writeErr(w, http.StatusBadRequest, "send goals: {\"goals\":{\"category\":hours,…}}")
		return nil, false
	}
	cents := map[string]int64{}
	for cat, h := range in.Goals {
		if catIndex(cat) < 0 {
			writeErr(w, http.StatusBadRequest, fmt.Sprintf("goals: unknown category %q", cat))
			return nil, false
		}
		if h == nil || *h <= 0 || *h > 168 || !hasDecimals(*h, 2) {
			writeErr(w, http.StatusBadRequest, "a goal must be more than 0 and at most 168 hours, with at most 2 decimals (goals can't be cleared)")
			return nil, false
		}
		cents[cat] = int64(math.Round(*h * 100))
	}
	if k != week.Of(s.now()) {
		writeErr(w, http.StatusConflict, "goals can only be set for the current week")
		return nil, false
	}
	return cents, true
}

// applyGoals stores goals (category → hundredths) for week k, all or nothing: a category that
// already has a different goal is an errStatus 409 and nothing is written.
func (s *Server) applyGoals(ctx context.Context, userID int64, k week.Key, cents map[string]int64) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		have, err := s.goals(ctx, tx, userID, k)
		if err != nil {
			return err
		}
		todo := map[string]int64{}
		for cat, c := range cents {
			old, set := have[cat]
			if !set {
				todo[cat] = c
				continue
			}
			if int64(math.Round(old*100)) != c {
				return errStatus{http.StatusConflict, fmt.Sprintf("your %s goal for this week is already set to %v hours and can't be changed", cat, old)}
			}
		}
		for cat, c := range todo {
			if _, err := tx.Exec(`INSERT INTO track_goals (user_id, week, category, centihours, created_at) VALUES (?, ?, ?, ?, ?)`,
				userID, k.String(), cat, c, s.now().Unix()); err != nil {
				return err
			}
		}
		return nil
	})
}

type goalAchiever struct {
	User     *publicUser        `json:"user"`
	Tier     int                `json:"tier"`    // 1 = met every goal, 2 = met some
	AllMet   bool               `json:"all_met"` // tier 1
	GoalsSet int                `json:"goals_set"`
	GoalsMet int                `json:"goals_met"`
	Percent  float64            `json:"percent"` // (logged in goal categories ÷ their goals − 1) × 100, 1 decimal
	Goals    map[string]float64 `json:"goals"`   // category → goal hours
	Logged   map[string]float64 `json:"logged"`  // category → hours logged, for the same categories
	logged   int64              // exact sums, for sorting
	goals    int64
}

// goalAchievers (GET /v1/track/leaderboard/goals?week=) ranks public users who met at least one
// of their goals in a published week (default and upper bound: published-through, so goals of a
// week still running are never shown). Everyone who met all their goals ranks above everyone
// who met only some; within a tier, by how far the goal categories' hours beat their goals.
func (s *Server) goalAchievers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	pt, err := s.publishedThrough(ctx)
	if err != nil {
		internalErr(w, "load publication", err)
		return
	}
	k, ok := s.queryWeek(w, r, "week", pt)
	if !ok {
		return
	}
	if pt.Before(k) {
		k = pt
	}
	rows, err := s.db.QueryContext(ctx, `SELECT g.user_id, g.category, g.centihours FROM track_goals g
		JOIN users u ON u.id = g.user_id WHERE u.track_public = 1 AND g.week = ?`, k.String())
	if err != nil {
		internalErr(w, "load goals", err)
		return
	}
	goals := map[int64]map[string]int64{}
	var ids []int64
	for rows.Next() {
		var id, c int64
		var cat string
		if err := rows.Scan(&id, &cat, &c); err != nil {
			rows.Close()
			internalErr(w, "load goals", err)
			return
		}
		if goals[id] == nil {
			goals[id] = map[string]int64{}
			ids = append(ids, id)
		}
		goals[id][cat] = c
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		internalErr(w, "load goals", err)
		return
	}
	data, err := s.loadWeeks(ctx, ids, k, k)
	if err != nil {
		internalErr(w, "load weeks", err)
		return
	}
	users, err := s.publicUsers(ctx, ids)
	if err != nil {
		internalErr(w, "load users", err)
		return
	}
	out := []goalAchiever{}
	for _, id := range ids {
		d := &weekData{}
		if len(data[id]) == 1 {
			d = data[id][0]
		}
		a := goalAchiever{User: users[id], Goals: map[string]float64{}, Logged: map[string]float64{}}
		for cat, g := range goals[id] {
			got := d.total(cat)
			a.GoalsSet++
			if got >= g {
				a.GoalsMet++
			}
			a.goals += g
			a.logged += got
			a.Goals[cat], a.Logged[cat] = float64(g)/100, float64(got)/100
		}
		if a.GoalsMet == 0 || a.User == nil {
			continue
		}
		a.AllMet = a.GoalsMet == a.GoalsSet
		a.Tier = 2
		if a.AllMet {
			a.Tier = 1
		}
		a.Percent = *round1(new((float64(a.logged)/float64(a.goals) - 1) * 100))
		out = append(out, a)
	}
	slices.SortFunc(out, func(a, b goalAchiever) int {
		// b.logged/b.goals vs a.logged/a.goals, cross-multiplied to stay exact.
		return cmp.Or(cmp.Compare(a.Tier, b.Tier), cmp.Compare(b.logged*a.goals, a.logged*b.goals),
			cmp.Compare(a.User.Name, b.User.Name), cmp.Compare(a.User.ID, b.User.ID))
	})
	writeJSON(w, http.StatusOK, map[string]any{"week": k.String(), "published_through": pt.String(), "users": out})
}
