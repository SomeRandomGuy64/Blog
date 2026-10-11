package api

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"fazon.nexus/backend/internal/db"
)

func (c *client) goals(key string, goals any) int {
	return c.do("POST", "/v1/track/weeks/"+key+"/goals", map[string]any{"goals": goals}).Code
}

func (e *env) user(name string) *client {
	e.t.Helper()
	email := strings.ToLower(name) + "@example.com"
	if _, err := db.AddUser(context.Background(), e.db, email, name, false); err != nil {
		e.t.Fatal(err)
	}
	return e.login(email)
}

func TestTrackGoals(t *testing.T) {
	e := newEnv(t)
	wk := e.alice.mustStatus(200, "GET", "/v1/track/weeks/"+thisWeek, nil)
	eq(t, "no goals", fmt.Sprint(wk["goals"], " ", wk["goals_open"]), "map[] true")
	eq(t, "past week closed", e.alice.mustStatus(200, "GET", "/v1/track/weeks/"+lastWeek, nil)["goals_open"], any(false))

	path := "/v1/track/weeks/" + thisWeek + "/goals"
	doc := e.alice.mustStatus(200, "POST", path, map[string]any{"goals": map[string]any{"active": 35}})
	eq(t, "set", fmt.Sprint(doc["goals"]), "map[active:35]")
	eq(t, "full week doc", doc["week"], any(thisWeek))
	eq(t, "same value is a no-op", e.alice.goals(thisWeek, map[string]any{"active": 35.0}), 200)
	eq(t, "change refused", e.alice.goals(thisWeek, map[string]any{"active": 36}), 409)
	if !strings.Contains(e.alice.last.Body.String(), "can't be changed") {
		t.Fatalf("409 body %s", e.alice.last.Body)
	}
	eq(t, "all or nothing", e.alice.goals(thisWeek, map[string]any{"passive": 5, "active": 34}), 409)
	eq(t, "passive not set", fmt.Sprint(e.alice.mustStatus(200, "GET", "/v1/track/weeks/"+thisWeek, nil)["goals"]), "map[active:35]")
	eq(t, "another category later", e.alice.goals(thisWeek, map[string]any{"passive": 0.25, "anki": 168}), 200)
	eq(t, "GET", fmt.Sprint(e.alice.mustStatus(200, "GET", "/v1/track/weeks/"+thisWeek, nil)["goals"]), "map[active:35 anki:168 passive:0.25]")

	for name, g := range map[string]any{
		"zero": map[string]any{"output": 0}, "negative": map[string]any{"output": -1}, "over 168": map[string]any{"output": 168.01},
		"3 decimals": map[string]any{"output": 1.234}, "null (no clearing)": map[string]any{"active": nil}, "unknown": map[string]any{"gaming": 1},
		"empty": map[string]any{}, "missing": nil, "string": map[string]any{"output": "5"},
	} {
		eq(t, name, e.alice.goals(thisWeek, g), 400)
	}
	eq(t, "past week", e.alice.goals(lastWeek, map[string]any{"output": 1}), 409)
	eq(t, "future week", e.alice.goals("2026-10-17", map[string]any{"output": 1}), 400)
	eq(t, "logged out", e.anon().goals(thisWeek, map[string]any{"output": 1}), 401)
	eq(t, "others' goals are their own", fmt.Sprint(e.bob.mustStatus(200, "GET", "/v1/track/weeks/"+thisWeek, nil)["goals"]), "map[]")

	// The week save never touches goals, even when it empties (deletes) the week.
	e.alice.saveWeek(thisWeek, map[string]any{"hours": []any{hrs("active", nil, 3)}, "goals": map[string]any{"active": 1}})
	e.alice.saveWeek(thisWeek, map[string]any{})
	eq(t, "goals survive", fmt.Sprint(e.alice.mustStatus(200, "GET", "/v1/track/weeks/"+thisWeek, nil)["goals"]), "map[active:35 anki:168 passive:0.25]")
	eq(t, "still write-once", e.alice.goals(thisWeek, map[string]any{"active": 1}), 409)

	// Private while the week runs: no public endpoint carries them.
	e.alice.saveWeek(thisWeek, map[string]any{"hours": []any{hrs("active", nil, 40)}, "summary": "s"})
	for _, p := range []string{fmt.Sprint("/v1/users/", e.alice.id, "/track"), "/v1/track/leaderboard?period=last", "/v1/track/summaries",
		fmt.Sprint("/v1/users/", e.alice.id, "/card"), fmt.Sprint("/v1/users/", e.alice.id)} {
		for _, c := range []*client{e.anon(), e.bob, e.alice} {
			if body := c.do("GET", p, nil).Body.String(); strings.Contains(body, "goal") {
				t.Fatalf("%s leaks goals: %s", p, body)
			}
		}
	}
	eq(t, "goal board", len(e.anon().mustStatus(200, "GET", "/v1/track/leaderboard/goals", nil)["users"].([]any)), 0)

	// Deleting the user takes the goals.
	if _, err := e.db.Exec(`DELETE FROM users WHERE id = ?`, e.alice.id); err != nil {
		t.Fatal(err)
	}
	var n int
	e.db.QueryRow(`SELECT COUNT(*) FROM track_goals`).Scan(&n)
	eq(t, "cascade", n, 0)
}

func TestGoalAchievers(t *testing.T) {
	e := newEnv(t)
	carol, dave, erin, frank := e.user("Carol"), e.user("Dave"), e.user("Erin"), e.user("Frank")
	type plan struct {
		c     *client
		goals map[string]any
		hours []any
	}
	plans := []plan{
		{e.alice, map[string]any{"active": 10, "passive": 10}, []any{hrs("active", nil, 12), hrs("passive", nil, 10)}}, // all met, +10%
		{e.bob, map[string]any{"active": 5}, []any{hrs("active", nil, 10)}},                                            // all met, +100%
		{frank, map[string]any{"output": 2.5}, []any{hrs("output", nil, 5), hrs("active", nil, 50)}},                   // all met, +100% (ties bob)
		{carol, map[string]any{"active": 10, "anki": 1}, []any{hrs("active", nil, 30)}},                                // 1 of 2, +172.7%
		{dave, map[string]any{"active": 10}, []any{hrs("active", nil, 5)}},                                             // none met
		{erin, map[string]any{"active": 1}, []any{hrs("active", nil, 5)}},                                              // hidden
		{e.admin, nil, []any{hrs("active", nil, 100)}},                                                                 // no goals
	}
	set := func(key string) {
		for _, p := range plans {
			if p.goals != nil {
				eq(t, "set goals", p.c.goals(key, p.goals), 200)
			}
			p.c.saveWeek(key, map[string]any{"hours": p.hours})
		}
	}
	envClock := e.clock
	e.clock = at("2026-10-05T12:00:00Z") // during lastWeek
	set(lastWeek)
	e.clock = envClock
	erin.mustStatus(200, "POST", "/v1/track/settings", map[string]any{"public": false})

	board := func(q string) (map[string]any, []string) {
		out := e.anon().mustStatus(200, "GET", "/v1/track/leaderboard/goals"+q, nil)
		var rows []string
		for _, u := range out["users"].([]any) {
			m := u.(map[string]any)
			rows = append(rows, fmt.Sprintf("%s:%v:%v:%v/%v:%v", m["user"].(map[string]any)["name"], m["tier"], m["all_met"],
				m["goals_met"], m["goals_set"], m["percent"]))
		}
		return out, rows
	}
	out, rows := board("")
	eq(t, "week", fmt.Sprint(out["week"], " ", out["published_through"]), lastWeek+" "+lastWeek)
	eqStrings(t, "ranking", rows, []string{"Bob:1:true:1/1:100", "Frank:1:true:1/1:100", "Alice:1:true:2/2:10", "Carol:2:false:1/2:172.7"})
	carolRow := out["users"].([]any)[3].(map[string]any)
	eq(t, "per-category", fmt.Sprint(carolRow["goals"], " ", carolRow["logged"]), "map[active:10 anki:1] map[active:30 anki:0]")
	_, rows = board("?week=2026-09-26")
	eq(t, "other published week", len(rows), 0)

	// The running week's goals never show, even when asked for, until it's published.
	set(thisWeek)
	out, _ = board("?week=" + thisWeek)
	eq(t, "clamped", out["week"], any(lastWeek))
	eq(t, "future week", e.anon().do("GET", "/v1/track/leaderboard/goals?week=2026-10-17", nil).Code, 400)
	e.clock = at("2026-10-18T01:30:00Z")
	_, before := board("")
	e.admin.mustStatus(200, "POST", "/v1/track/admin/end-week", nil)
	out, rows = board("")
	eq(t, "ended early", out["week"], any(thisWeek))
	eqStrings(t, "this week's board", rows, before)
	eq(t, "rows", len(rows), 4)
}
