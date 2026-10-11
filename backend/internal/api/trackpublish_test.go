package api

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

const lastWeek = "2026-10-03" // published-through at the env clock

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestTrackRatings(t *testing.T) {
	e := newEnv(t)
	wk := e.alice.mustStatus(200, "GET", "/v1/track/weeks/"+thisWeek, nil)
	eq(t, "no ratings", fmt.Sprint(wk["ratings"]), "map[]")

	saved := e.alice.saveWeek(thisWeek, map[string]any{"hours": []any{hrs("active", nil, 2)},
		"ratings": map[string]any{"active": 4, "passive": 1, "anki": nil, "soroban": 3}})
	eq(t, "saved", fmt.Sprint(saved["ratings"]), "map[active:4 passive:1 soroban:3]")
	eq(t, "GET", fmt.Sprint(e.alice.mustStatus(200, "GET", "/v1/track/weeks/"+thisWeek, nil)["ratings"]), "map[active:4 passive:1 soroban:3]")

	for name, r := range map[string]any{
		"zero": map[string]any{"active": 0}, "five": map[string]any{"active": 5}, "negative": map[string]any{"output": -1},
		"fraction": map[string]any{"active": 2.5}, "string": map[string]any{"active": "🔥"}, "unknown category": map[string]any{"gaming": 2},
		"not an object": []any{1, 2},
	} {
		eq(t, name, e.alice.do("POST", "/v1/track/weeks/"+thisWeek, map[string]any{"ratings": r}).Code, 400)
	}
	eq(t, "unchanged", fmt.Sprint(e.alice.mustStatus(200, "GET", "/v1/track/weeks/"+thisWeek, nil)["ratings"]), "map[active:4 passive:1 soroban:3]")

	// The save is wholesale: ratings left out are cleared.
	eq(t, "replace", fmt.Sprint(e.alice.saveWeek(thisWeek, map[string]any{"hours": []any{hrs("active", nil, 2)},
		"ratings": map[string]any{"output": 2}})["ratings"]), "map[output:2]")
	eq(t, "absent clears", fmt.Sprint(e.alice.saveWeek(thisWeek, map[string]any{"hours": []any{hrs("active", nil, 2)}})["ratings"]), "map[]")
	eq(t, "null clears", fmt.Sprint(e.alice.saveWeek(thisWeek, map[string]any{"hours": []any{hrs("active", nil, 2)}, "ratings": nil})["ratings"]), "map[]")

	// A rating alone keeps a week; emptying the week takes its ratings.
	e.bob.saveWeek(lastWeek, map[string]any{"ratings": map[string]any{"active": 1}})
	var n int
	e.db.QueryRow(`SELECT COUNT(*) FROM track_weeks WHERE user_id = ?`, e.bob.id).Scan(&n)
	eq(t, "rating-only week", n, 1)
	e.bob.saveWeek(lastWeek, map[string]any{})
	e.db.QueryRow(`SELECT (SELECT COUNT(*) FROM track_weeks WHERE user_id = ?1) + (SELECT COUNT(*) FROM track_ratings WHERE user_id = ?1)`, e.bob.id).Scan(&n)
	eq(t, "emptied", n, 0)

	// Public wherever the week's hours are: profile, leaderboard (period=last), feed, own list.
	e.alice.saveWeek(lastWeek, map[string]any{"hours": []any{hrs("active", nil, 3)}, "summary": "ok",
		"ratings": map[string]any{"active": 3, "anki": 2}})
	e.alice.saveWeek(thisWeek, map[string]any{"hours": []any{hrs("active", nil, 1)}, "ratings": map[string]any{"active": 1}})
	prof := e.bob.mustStatus(200, "GET", fmt.Sprint("/v1/users/", e.alice.id, "/track"), nil)["weeks"].([]any)
	eq(t, "profile weeks", len(prof), 1)
	eq(t, "profile ratings", fmt.Sprint(prof[0].(map[string]any)["ratings"]), "map[active:3 anki:2]")
	lb := e.anon().mustStatus(200, "GET", "/v1/track/leaderboard?period=last", nil)["users"].([]any)[0].(map[string]any)
	eq(t, "leaderboard ratings", fmt.Sprint(lb["ratings"]), "map[active:3 anki:2]")
	lb4 := e.anon().mustStatus(200, "GET", "/v1/track/leaderboard?period=4w", nil)["users"].([]any)[0].(map[string]any)
	eq(t, "no ratings on longer periods", lb4["ratings"], any(nil))
	feed := e.anon().mustStatus(200, "GET", "/v1/track/summaries", nil)["summaries"].([]any)[0].(map[string]any)
	eq(t, "feed ratings", fmt.Sprint(feed["ratings"]), "map[active:3 anki:2]")
	own := e.alice.mustStatus(200, "GET", "/v1/track/weeks", nil)["weeks"].([]any)
	eq(t, "own list", fmt.Sprint(own[1].(map[string]any)["ratings"]), "map[active:1]")

	// Hidden users' ratings go with the rest.
	e.alice.mustStatus(200, "POST", "/v1/track/settings", map[string]any{"public": false})
	eq(t, "hidden", len(e.anon().mustStatus(200, "GET", "/v1/track/leaderboard?period=last", nil)["users"].([]any)), 0)
}

// The current week never shows to anyone but its owner until it is published.
func TestPublishedThrough(t *testing.T) {
	e := newEnv(t)
	e.alice.saveWeek(lastWeek, map[string]any{"hours": []any{hrs("active", nil, 3)}, "summary": "last week"})
	e.alice.saveWeek(thisWeek, map[string]any{"hours": []any{hrs("active", nil, 50)}, "kotu": nums(99), "summary": "CURRENT-SECRET",
		"ratings": map[string]any{"active": 4}})
	e.bob.saveWeek(thisWeek, map[string]any{"hours": []any{hrs("passive", nil, 7)}, "summary": "BOB-CURRENT"})
	trackPath := fmt.Sprint("/v1/users/", e.alice.id, "/track")

	public := func(c *client) string {
		var b strings.Builder
		for _, p := range []string{"/v1/track/leaderboard?period=last", "/v1/track/leaderboard?period=4w", "/v1/track/leaderboard?period=12w",
			"/v1/track/leaderboard?period=52w", "/v1/track/leaderboard?period=all", "/v1/track/leaderboard?period=week",
			"/v1/track/leaderboard?period=4w&week=" + thisWeek, "/v1/track/summaries", "/v1/track/summaries?before=2026-10-17",
			fmt.Sprint("/v1/track/summaries?user=", e.alice.id), trackPath, fmt.Sprint("/v1/users/", e.alice.id, "/card"),
			fmt.Sprint("/v1/users/", e.alice.id), "/v1/track/leaderboard/goals"} {
			rec := c.do("GET", p, nil)
			if rec.Code != 200 {
				t.Fatalf("%s: %d", p, rec.Code)
			}
			b.WriteString(rec.Body.String())
		}
		return b.String()
	}
	hidden := func(what string) {
		t.Helper()
		for _, c := range []*client{e.anon(), e.bob, e.admin} {
			body := public(c)
			for _, leak := range []string{"CURRENT-SECRET", "BOB-CURRENT", `"week":"` + thisWeek, `"` + thisWeek + `"]`, `"active":50`, `"kotu":99`, `"passive":7`} {
				if strings.Contains(body, leak) {
					t.Fatalf("%s: %q leaked: %s", what, leak, body)
				}
			}
		}
	}
	hidden("before the reset")

	// Bob sees alice's last week on her profile, not this one; alice sees both.
	other := e.bob.mustStatus(200, "GET", trackPath, nil)
	eq(t, "published_through", other["published_through"], any(lastWeek))
	eq(t, "other's weeks", len(other["weeks"].([]any)), 1)
	own := e.alice.mustStatus(200, "GET", trackPath, nil)
	eq(t, "owner's weeks", len(own["weeks"].([]any)), 2)
	eq(t, "owner sees current", own["weeks"].([]any)[1].(map[string]any)["summary"], any("CURRENT-SECRET"))
	eq(t, "owner's week view", e.alice.mustStatus(200, "GET", "/v1/track/weeks/"+thisWeek, nil)["summary"], any("CURRENT-SECRET"))
	// The owner's own leaderboard row stops at published-through too.
	lb := e.alice.mustStatus(200, "GET", "/v1/track/leaderboard?period=4w", nil)
	eq(t, "to", lb["to"], any(lastWeek))
	eq(t, "own totals", lb["users"].([]any)[0].(map[string]any)["totals"].(map[string]any)["active"], any(3.0))

	// Leaderboard metadata.
	eq(t, "current_week", lb["current_week"], any(thisWeek))
	eq(t, "current_week_end", lb["current_week_end"], any("2026-10-18T03:00:00Z"))
	eq(t, "ended_early", lb["ended_early"], any(false))

	// After the natural reset it all shows.
	e.clock = at("2026-10-18T03:00:00Z")
	lb = e.anon().mustStatus(200, "GET", "/v1/track/leaderboard?period=last", nil)
	eq(t, "published", lb["to"], any(thisWeek))
	eq(t, "alice tops by active", lb["users"].([]any)[0].(map[string]any)["totals"].(map[string]any)["active"], any(50.0))
	eq(t, "feed", e.anon().mustStatus(200, "GET", "/v1/track/summaries", nil)["summaries"].([]any)[0].(map[string]any)["week"], any(thisWeek))
	eq(t, "profile", len(e.bob.mustStatus(200, "GET", trackPath, nil)["weeks"].([]any)), 2)
}

func TestEndWeekEarly(t *testing.T) {
	e := newEnv(t)
	e.alice.saveWeek(thisWeek, map[string]any{"hours": []any{hrs("active", nil, 5)}, "summary": "this week"})
	trackPath := fmt.Sprint("/v1/users/", e.alice.id, "/track")
	const path = "/v1/track/admin/end-week"

	cfg := e.admin.mustStatus(200, "GET", "/v1/track/config", nil)
	eq(t, "published_through", cfg["published_through"], any(lastWeek))
	eq(t, "current_week_end", cfg["current_week_end"], any("2026-10-18T03:00:00Z"))
	eq(t, "end_week", fmt.Sprint(cfg["end_week"]), "map[allowed:false allowed_from:2026-10-18T01:00:00Z]")
	eq(t, "no end_week for others", e.alice.mustStatus(200, "GET", "/v1/track/config", nil)["end_week"], any(nil))

	eq(t, "too early", e.admin.do("POST", path, nil).Code, 409)
	if !strings.Contains(e.admin.last.Body.String(), "2026-10-18T01:00:00Z") {
		t.Fatalf("409 doesn't say when: %s", e.admin.last.Body)
	}
	e.clock = at("2026-10-18T00:59:59Z")
	eq(t, "a second too early", e.admin.do("POST", path, nil).Code, 409)
	e.clock = at("2026-10-18T01:00:00Z")
	eq(t, "not admin", e.alice.do("POST", path, nil).Code, 403)
	eq(t, "not admin undo", e.alice.do("DELETE", path, nil).Code, 403)
	eq(t, "logged out", e.anon().do("POST", path, nil).Code, 401)
	noOrigin := e.login("admin@example.com")
	noOrigin.origin = ""
	eq(t, "no origin", noOrigin.do("POST", path, nil).Code, 403)
	eq(t, "still not ended", e.anon().mustStatus(200, "GET", "/v1/track/leaderboard?period=last", nil)["to"], any(lastWeek))

	res := e.admin.mustStatus(200, "POST", path, nil)
	eq(t, "ended", fmt.Sprint(res["published_through"], " ", res["ended_early"], " ", res["end_week"]),
		thisWeek+" true map[allowed:true allowed_from:2026-10-18T01:00:00Z]")
	eq(t, "idempotent", fmt.Sprint(e.admin.mustStatus(200, "POST", path, nil)), fmt.Sprint(res))

	lb := e.anon().mustStatus(200, "GET", "/v1/track/leaderboard?period=last", nil)
	eq(t, "leaderboard", fmt.Sprint(lb["to"], " ", lb["ended_early"], " ", len(lb["users"].([]any))), thisWeek+" true 1")
	eq(t, "feed", e.anon().mustStatus(200, "GET", "/v1/track/summaries", nil)["summaries"].([]any)[0].(map[string]any)["summary"], any("this week"))
	eq(t, "profile", len(e.bob.mustStatus(200, "GET", trackPath, nil)["weeks"].([]any)), 1)
	eq(t, "config", e.alice.mustStatus(200, "GET", "/v1/track/config", nil)["published_through"], any(thisWeek))

	// Logging carries on: the ended week is still editable, and edits show live.
	e.alice.saveWeek(thisWeek, map[string]any{"hours": []any{hrs("active", nil, 6)}, "summary": "edited"})
	lb = e.anon().mustStatus(200, "GET", "/v1/track/leaderboard?period=last", nil)
	eq(t, "edit shows", lb["users"].([]any)[0].(map[string]any)["totals"].(map[string]any)["active"], any(6.0))

	// Undo before the reset.
	res = e.admin.mustStatus(200, "DELETE", path, nil)
	eq(t, "reopened", fmt.Sprint(res["published_through"], " ", res["ended_early"]), lastWeek+" false")
	eq(t, "undo idempotent", e.admin.do("DELETE", path, nil).Code, 200)
	eq(t, "hidden again", len(e.anon().mustStatus(200, "GET", "/v1/track/leaderboard?period=last", nil)["users"].([]any)), 0)

	// End it again; at the natural reset the stale setting changes nothing and the new week
	// is unpublished as usual.
	e.admin.mustStatus(200, "POST", path, nil)
	e.clock = at("2026-10-18T03:00:00Z")
	cfg = e.admin.mustStatus(200, "GET", "/v1/track/config", nil)
	eq(t, "rolled over", fmt.Sprint(cfg["current_week"], " ", cfg["published_through"], " ", cfg["ended_early"]), "2026-10-17 "+thisWeek+" false")
	eq(t, "new window", fmt.Sprint(cfg["end_week"]), "map[allowed:false allowed_from:2026-10-25T01:00:00Z]")
	eq(t, "can't end the new week yet", e.admin.do("POST", path, nil).Code, 409)
	e.clock = at("2026-10-25T02:59:59Z")
	eq(t, "last second", e.admin.do("POST", path, nil).Code, 200)
	eq(t, "next week ended", e.anon().mustStatus(200, "GET", "/v1/track/leaderboard?period=last", nil)["to"], any("2026-10-17"))
}

func TestLeaderboardSummariesAndMetrics(t *testing.T) {
	e := newEnv(t)
	e.alice.saveWeek("2026-09-19", map[string]any{"hours": []any{hrs("active", nil, 1)}, "summary": "old public"})
	e.alice.saveWeek("2026-09-26", map[string]any{"hours": []any{hrs("active", nil, 1)}, "summary": "newer but private", "summary_private": true})
	e.alice.saveWeek(lastWeek, map[string]any{"hours": []any{hrs("active", nil, 1.17), hrs("passive", nil, 2.33), hrs("anki", nil, 0.01)}})
	e.bob.saveWeek(lastWeek, map[string]any{"hours": []any{hrs("passive", nil, 9)}, "summary": "bob's", "ratings": map[string]any{"passive": 2}})
	e.alice.saveWeek(thisWeek, map[string]any{"hours": []any{hrs("active", nil, 1)}, "summary": "unpublished"})

	lb := e.anon().mustStatus(200, "GET", "/v1/track/leaderboard?period=4w", nil)
	users := lb["users"].([]any)
	alice, bob := users[0].(map[string]any), users[1].(map[string]any) // ranked by active hours
	eq(t, "alice", alice["user"].(map[string]any)["name"], any("Alice"))
	tot := alice["totals"].(map[string]any)
	eq(t, "exact all", tot["all"], any(5.51))       // 1 + 1 + 1.17 + 2.33 + 0.01
	eq(t, "nopassive", tot["nopassive"], any(3.18)) // 5.51 − 2.33
	eq(t, "summary skips private", alice["summary"], any("old public"))
	eq(t, "summary_week", alice["summary_week"], any("2026-09-19"))
	eq(t, "bob", fmt.Sprint(bob["summary"], " ", bob["summary_week"], " ", bob["totals"].(map[string]any)["nopassive"]), "bob's "+lastWeek+" 0")
	if body := e.anon().do("GET", "/v1/track/leaderboard?period=all", nil).Body.String(); strings.Contains(body, "private") ||
		strings.Contains(body, "unpublished") {
		t.Fatal("private or unpublished summary on the leaderboard")
	}

	last := e.anon().mustStatus(200, "GET", "/v1/track/leaderboard?period=last", nil)["users"].([]any)
	a := last[0].(map[string]any)
	eq(t, "last: no summary that week", fmt.Sprint(a["summary"], a["summary_week"]), "<nil> <nil>")
	eq(t, "last: empty ratings", fmt.Sprint(a["ratings"]), "map[]")
	b := last[1].(map[string]any)
	eq(t, "last: summary", fmt.Sprint(b["summary"], " ", b["summary_week"], " ", b["ratings"]), "bob's "+lastWeek+" map[passive:2]")
	eq(t, "last: series", fmt.Sprint(b["series"].(map[string]any)["all"]), "[9]")
}

func TestProfileDays(t *testing.T) {
	e := newEnv(t)
	e.alice.saveWeek("2026-09-19", map[string]any{"hours": []any{hrs("active", nil, 1)}})
	e.alice.saveWeek("2026-09-26", map[string]any{"kyotsu": nums(nil, 70)})
	e.kyotsuAt(e.alice, at("2026-10-04T05:00:00Z"), 10, 9) // last week, day 0
	e.alice.saveWeek(lastWeek, map[string]any{"kotu": nums(nil, nil, 80.5), "kyotsu": nums(nil, 60)})
	e.alice.saveWeek(thisWeek, map[string]any{"kotu": nums(10)})

	tr := e.bob.mustStatus(200, "GET", fmt.Sprint("/v1/users/", e.alice.id, "/track"), nil)
	eq(t, "first_score_week", tr["first_score_week"], any("2026-09-26"))
	ws := tr["weeks"].([]any)
	eq(t, "weeks", len(ws), 3)
	w0, w2 := ws[0].(map[string]any), ws[2].(map[string]any)
	eq(t, "no scores", fmt.Sprint(w0["kotu_days"], w0["kyotsu_source"]), "[<nil> <nil> <nil> <nil> <nil> <nil> <nil>] [<nil> <nil> <nil> <nil> <nil> <nil> <nil>]")
	eq(t, "days", fmt.Sprint(w2["days"]), "[2026-10-03 2026-10-04 2026-10-05 2026-10-06 2026-10-07 2026-10-08 2026-10-09]")
	eq(t, "kotu_days", fmt.Sprint(w2["kotu_days"]), "[<nil> <nil> 80.5 <nil> <nil> <nil> <nil>]")
	eq(t, "kyotsu_days", fmt.Sprint(w2["kyotsu_days"]), "[90 60 <nil> <nil> <nil> <nil> <nil>]")
	eq(t, "kyotsu_source", fmt.Sprint(w2["kyotsu_source"]), "[auto manual <nil> <nil> <nil> <nil> <nil>]")
	eq(t, "weekly averages still there", fmt.Sprint(w2["kotu"], " ", w2["kyotsu"]), "80.5 75")

	// The owner also gets the current week.
	own := e.alice.mustStatus(200, "GET", fmt.Sprint("/v1/users/", e.alice.id, "/track"), nil)["weeks"].([]any)
	eq(t, "owner current week", fmt.Sprint(own[3].(map[string]any)["kotu_days"]), "[10 <nil> <nil> <nil> <nil> <nil> <nil>]")
	eq(t, "no scores at all", e.bob.mustStatus(200, "GET", fmt.Sprint("/v1/users/", e.bob.id, "/track"), nil)["first_score_week"], any(nil))
}
