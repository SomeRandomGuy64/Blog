package api

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// The env clock is Sun 2026-10-11 12:00 UTC: current week 2026-10-10 (from Sun 03:00 UTC).
const thisWeek = "2026-10-10"

// nextWeek moves the clock on a week, so what was thisWeek is over and public.
func (e *env) nextWeek() { e.clock = e.clock.Add(7 * 24 * time.Hour) }

func (c *client) subcat(cat, name string) int64 {
	c.e.t.Helper()
	return int64(c.mustStatus(201, "POST", "/v1/track/subcats", map[string]any{"category": cat, "name": name})["id"].(float64))
}

func (c *client) saveWeek(key string, body map[string]any) map[string]any {
	c.e.t.Helper()
	return c.mustStatus(200, "POST", "/v1/track/weeks/"+key, body)
}

func hrs(cat string, sub any, h float64) map[string]any {
	return map[string]any{"category": cat, "subcat": sub, "hours": h}
}

func nums(vals ...any) []any { // 7 day values; missing trailing days are null
	out := make([]any, 7)
	copy(out, vals)
	return out
}

func TestTrackSubcats(t *testing.T) {
	e := newEnv(t)
	cfg := e.alice.mustStatus(200, "GET", "/v1/track/config", nil)
	var cats []string
	for _, c := range cfg["categories"].([]any) {
		cats = append(cats, c.(map[string]any)["id"].(string))
	}
	eqStrings(t, "categories", cats, []string{"active", "passive", "phonetic", "output", "soroban", "anki"})
	eq(t, "anki label", cfg["categories"].([]any)[5].(map[string]any)["label"], any("Anki"))
	eq(t, "label", cfg["categories"].([]any)[0].(map[string]any)["label"], any("Active immersion"))
	eq(t, "public", cfg["public"], any(true))
	eq(t, "current_week", cfg["current_week"], any(thisWeek))
	eq(t, "no subcats", len(cfg["subcats"].([]any)), 0)
	eq(t, "logged out", e.anon().do("GET", "/v1/track/config", nil).Code, 401)

	yt := e.alice.subcat("active", "YouTube")
	anime := e.alice.subcat("active", " Anime ")
	podcast := e.alice.subcat("passive", "YouTube") // same name, other category
	e.alice.subcat("phonetic", "Pitch")
	deck := e.alice.subcat("anki", "Core 2k")
	e.alice.subcat("soroban", "Drills") // created after anki, listed before it
	e.bob.subcat("active", "YouTube")   // same name, other user
	create := func(c *client, cat, name string) int {
		return c.do("POST", "/v1/track/subcats", map[string]any{"category": cat, "name": name}).Code
	}
	eq(t, "duplicate (case-insensitive)", create(e.alice, "active", "youtube"), 409)
	eq(t, "bad category", create(e.alice, "gaming", "X"), 400)
	eq(t, "empty name", create(e.alice, "active", "  "), 400)
	eq(t, "41 runes", create(e.alice, "active", strings.Repeat("名", 41)), 400)
	eq(t, "control char", create(e.alice, "active", "a\tb"), 400)
	eq(t, "logged out", create(e.anon(), "active", "X"), 401)

	upd := func(c *client, id int64, body map[string]any) int {
		return c.do("POST", fmt.Sprint("/v1/track/subcats/", id), body).Code
	}
	eq(t, "rename onto existing", upd(e.alice, anime, map[string]any{"name": "YOUTUBE"}), 409)
	eq(t, "rename", upd(e.alice, anime, map[string]any{"name": "Anime (JP)", "position": 7}), 200)
	eq(t, "renamed", e.alice.last.Body.String(), fmt.Sprintf(`{"id":%d,"category":"active","name":"Anime (JP)","archived":false,"position":7}`+"\n", anime))
	eq(t, "rename to own name, other case", upd(e.alice, yt, map[string]any{"name": "youtube"}), 200)
	eq(t, "bad position", upd(e.alice, yt, map[string]any{"position": -1}), 400)
	eq(t, "other user's", upd(e.bob, yt, map[string]any{"name": "Mine"}), 404)
	eq(t, "missing", upd(e.alice, 9999, map[string]any{"name": "X"}), 404)

	// Archiving frees the name; unarchiving into a clash is refused.
	eq(t, "archive", upd(e.alice, yt, map[string]any{"archived": true}), 200)
	yt2 := e.alice.subcat("active", "YouTube")
	eq(t, "unarchive clash", upd(e.alice, yt, map[string]any{"archived": false}), 409)
	eq(t, "rename archived", upd(e.alice, yt, map[string]any{"name": "YouTube (old)"}), 200)
	eq(t, "unarchive", upd(e.alice, yt, map[string]any{"archived": false}), 200)

	cfg = e.alice.mustStatus(200, "GET", "/v1/track/config", nil)
	var got []string
	for _, x := range cfg["subcats"].([]any) {
		m := x.(map[string]any)
		got = append(got, fmt.Sprintf("%s:%s:%v", m["category"], m["name"], m["archived"]))
	}
	eqStrings(t, "config subcats", got, []string{"active:YouTube (old):false", "active:Anime (JP):false", "active:YouTube:false", "passive:YouTube:false",
		"phonetic:Pitch:false", "soroban:Drills:false", "anki:Core 2k:false"}) // by category order, then position: 0, 7, 8
	_ = yt2
	eq(t, "anki rename", upd(e.alice, deck, map[string]any{"name": "Core 2k/6k"}), 200)

	// Limits: 30 active per category; 100 including archived.
	for i := 0; i < 30; i++ {
		e.admin.subcat("output", fmt.Sprint("o", i))
	}
	eq(t, "31st active", create(e.admin, "output", "one more"), 409)
	cfg = e.admin.mustStatus(200, "GET", "/v1/track/config", nil)
	first := int64(cfg["subcats"].([]any)[0].(map[string]any)["id"].(float64))
	eq(t, "archive one", upd(e.admin, first, map[string]any{"archived": true}), 200)
	eq(t, "room again", create(e.admin, "output", "one more"), 201)
	eq(t, "unarchive over limit", upd(e.admin, first, map[string]any{"archived": false}), 409)
	for i := 0; i < 98; i++ {
		e.db.Exec(`INSERT INTO track_subcats (user_id, category, name, archived, created_at) VALUES (?, 'soroban', ?, 1, 0)`, e.admin.id, fmt.Sprint("old", i))
	}
	eq(t, "under total limit", create(e.admin, "soroban", "x"), 201)
	e.db.Exec(`INSERT INTO track_subcats (user_id, category, name, archived, created_at) VALUES (?, 'soroban', 'old-extra', 1, 0)`, e.admin.id)
	eq(t, "100 including archived", create(e.admin, "soroban", "y"), 409)

	// Delete: only when unused.
	e.alice.saveWeek(thisWeek, map[string]any{"hours": []any{hrs("passive", podcast, 2)}})
	eq(t, "delete used", e.alice.do("DELETE", fmt.Sprint("/v1/track/subcats/", podcast), nil).Code, 409)
	eq(t, "delete other user's", e.bob.do("DELETE", fmt.Sprint("/v1/track/subcats/", anime), nil).Code, 404)
	eq(t, "delete unused", e.alice.do("DELETE", fmt.Sprint("/v1/track/subcats/", anime), nil).Code, 200)
	eq(t, "delete again", e.alice.do("DELETE", fmt.Sprint("/v1/track/subcats/", anime), nil).Code, 404)
	eq(t, "archive used", upd(e.alice, podcast, map[string]any{"archived": true}), 200)
	// Archived subcats can still be logged against (editing past weeks).
	e.alice.saveWeek("2026-10-03", map[string]any{"hours": []any{hrs("passive", podcast, 1)}})
}

func TestTrackWeekSave(t *testing.T) {
	e := newEnv(t)
	yt := e.alice.subcat("active", "YouTube")
	podcast := e.alice.subcat("passive", "Podcasts")
	deck := e.alice.subcat("anki", "Mining")
	bobs := e.bob.subcat("active", "Games")

	empty := e.alice.mustStatus(200, "GET", "/v1/track/weeks/"+thisWeek, nil)
	eq(t, "start", empty["start"], any("2026-10-11T03:00:00Z"))
	eq(t, "end", empty["end"], any("2026-10-18T03:00:00Z"))
	eq(t, "days", fmt.Sprint(empty["days"]), "[2026-10-10 2026-10-11 2026-10-12 2026-10-13 2026-10-14 2026-10-15 2026-10-16]")
	eq(t, "no hours", len(empty["hours"].([]any)), 0)
	eq(t, "kotu", fmt.Sprint(empty["kotu"]), "[<nil> <nil> <nil> <nil> <nil> <nil> <nil>]")
	eq(t, "auto answers", fmt.Sprint(empty["kyotsu_auto_answers"]), "[0 0 0 0 0 0 0]")
	eq(t, "updated", empty["updated"], any(nil))
	eq(t, "summary", empty["summary"], any(""))
	eq(t, "summary_private", empty["summary_private"], any(false))
	for _, k := range []string{"2026-10-17", "2026-10-11", "1999-12-25", "2026-1-3", "x"} {
		eq(t, "bad key "+k, e.alice.do("GET", "/v1/track/weeks/"+k, nil).Code, 400)
		eq(t, "bad key save "+k, e.alice.do("POST", "/v1/track/weeks/"+k, map[string]any{}).Code, 400)
	}
	eq(t, "logged out", e.anon().do("GET", "/v1/track/weeks/"+thisWeek, nil).Code, 401)

	saved := e.alice.saveWeek(thisWeek, map[string]any{
		"hours": []any{hrs("phonetic", nil, 1), hrs("anki", nil, 0.5), hrs("passive", nil, 10), hrs("active", nil, 1.25), hrs("active", yt, 2.5),
			hrs("anki", deck, 0.75), hrs("output", nil, 0)},
		"kotu": nums(80, nil, 90.5), "kyotsu": nums(nil, 70), "summary": "  Good week\r\nlots of YouTube  ",
		"summary_private": true,
	})
	var got []string
	for _, h := range saved["hours"].([]any) {
		m := h.(map[string]any)
		got = append(got, fmt.Sprintf("%s/%v=%v", m["category"], m["subcat"], m["hours"]))
	}
	eqStrings(t, "hours (sorted, zero dropped)", got, []string{fmt.Sprintf("active/%d=2.5", yt), "active/<nil>=1.25", "passive/<nil>=10",
		"phonetic/<nil>=1", fmt.Sprintf("anki/%d=0.75", deck), "anki/<nil>=0.5"})
	eq(t, "kotu", fmt.Sprint(saved["kotu"]), "[80 <nil> 90.5 <nil> <nil> <nil> <nil>]")
	eq(t, "summary cleaned", saved["summary"], any("Good week\nlots of YouTube"))
	eq(t, "private", saved["summary_private"], any(true))
	if saved["updated"] == nil {
		t.Fatal("updated not set")
	}
	eq(t, "GET matches", fmt.Sprint(e.alice.mustStatus(200, "GET", "/v1/track/weeks/"+thisWeek, nil)), fmt.Sprint(saved))

	// Validation; a rejected save changes nothing.
	bad := map[string]map[string]any{
		"over 168":          {"hours": []any{hrs("active", nil, 168.01)}},
		"3 decimals":        {"hours": []any{hrs("active", nil, 1.234)}},
		"negative":          {"hours": []any{hrs("active", nil, -1)}},
		"category > 168":    {"hours": []any{hrs("active", nil, 100), hrs("active", yt, 68.5)}},
		"duplicate slot":    {"hours": []any{hrs("active", yt, 1), hrs("active", yt, 2)}},
		"duplicate none":    {"hours": []any{hrs("active", nil, 1), hrs("active", nil, 2)}},
		"unknown category":  {"hours": []any{hrs("gaming", nil, 1)}},
		"other's subcat":    {"hours": []any{hrs("active", bobs, 1)}},
		"wrong category":    {"hours": []any{hrs("active", podcast, 1)}},
		"anki subcat":       {"hours": []any{hrs("passive", deck, 1)}},
		"anki > 168":        {"hours": []any{hrs("anki", nil, 100), hrs("anki", deck, 68.5)}},
		"missing subcat":    {"hours": []any{hrs("active", 9999, 1)}},
		"6 kotu days":       {"kotu": []any{1, 2, 3, 4, 5, 6}},
		"kotu > 100":        {"kotu": nums(100.1)},
		"kotu 2 decimals":   {"kyotsu": nums(50.55)},
		"kotu negative":     {"kotu": nums(-0.1)},
		"summary too long":  {"summary": strings.Repeat("字", 10001)},
		"hours not numbers": {"hours": []any{map[string]any{"category": "active", "hours": "1:30"}}},
	}
	for name, body := range bad {
		eq(t, name, e.alice.do("POST", "/v1/track/weeks/"+thisWeek, body).Code, 400)
	}
	eq(t, "unchanged", fmt.Sprint(e.alice.mustStatus(200, "GET", "/v1/track/weeks/"+thisWeek, nil)), fmt.Sprint(saved))

	// Wholesale replace.
	repl := e.alice.saveWeek(thisWeek, map[string]any{"hours": []any{hrs("active", nil, 168), hrs("soroban", nil, 0.01)}})
	eq(t, "replaced hours", len(repl["hours"].([]any)), 2)
	eq(t, "scores cleared", fmt.Sprint(repl["kotu"]), "[<nil> <nil> <nil> <nil> <nil> <nil> <nil>]")
	eq(t, "summary cleared", repl["summary"], any(""))
	var n int
	e.db.QueryRow(`SELECT COUNT(*) FROM track_hours WHERE user_id = ?`, e.alice.id).Scan(&n)
	eq(t, "stored rows", n, 2)

	// Backfill an old week.
	e.alice.saveWeek("2013-06-15", map[string]any{"summary": "long ago"})

	// Saving nothing deletes the week (and its hours); only zero hours counts as nothing.
	gone := e.alice.saveWeek(thisWeek, map[string]any{"hours": []any{hrs("active", nil, 0)}, "kotu": nums(), "summary": "  ", "summary_private": true})
	eq(t, "deleted week updated", gone["updated"], any(nil))
	e.db.QueryRow(`SELECT COUNT(*) FROM track_weeks WHERE user_id = ? AND week = ?`, e.alice.id, thisWeek).Scan(&n)
	eq(t, "row deleted", n, 0)
	e.db.QueryRow(`SELECT COUNT(*) FROM track_hours WHERE user_id = ?`, e.alice.id).Scan(&n)
	eq(t, "hours deleted", n, 0)
	e.alice.saveWeek(thisWeek, map[string]any{"kyotsu": nums(nil, nil, nil, nil, nil, nil, 0)})
	e.db.QueryRow(`SELECT COUNT(*) FROM track_weeks WHERE user_id = ? AND week = ?`, e.alice.id, thisWeek).Scan(&n)
	eq(t, "a 0% score is data", n, 1)

	// Rate limit.
	for i := 0; ; i++ {
		if e.bob.do("POST", "/v1/track/weeks/"+thisWeek, map[string]any{"summary": "x"}).Code == 429 {
			break
		}
		if i > 200 {
			t.Fatal("no week save rate limit")
		}
	}
}

// kyotsuAt posts a trainer session that ends at t.
func (e *env) kyotsuAt(c *client, t time.Time, total, correct int) {
	e.t.Helper()
	saved := e.clock
	e.clock = t
	c.mustStatus(201, "POST", "/v1/kyotsu/sessions", map[string]any{"started": t.Add(-time.Minute).Format(time.RFC3339),
		"layout": "kotu", "answers": answers(total, correct)})
	e.clock = saved
}

func TestKyotsuAuto(t *testing.T) {
	e := newEnv(t)
	day := func(s string) time.Time { tm, _ := time.Parse(time.RFC3339, s); return tm }
	e.kyotsuAt(e.alice, day("2026-10-11T03:00:00Z"), 10, 7)  // day 0, first second
	e.kyotsuAt(e.alice, day("2026-10-12T02:59:59Z"), 10, 10) // day 0, last second
	e.kyotsuAt(e.alice, day("2026-10-12T03:00:00Z"), 4, 1)   // day 1
	e.kyotsuAt(e.alice, day("2026-10-11T02:59:59Z"), 5, 5)   // previous week, day 6
	e.kyotsuAt(e.bob, day("2026-10-11T12:00:00Z"), 10, 0)    // someone else

	wk := e.alice.mustStatus(200, "GET", "/v1/track/weeks/"+thisWeek, nil)
	eq(t, "auto", fmt.Sprint(wk["kyotsu_auto"]), "[85 25 <nil> <nil> <nil> <nil> <nil>]")
	eq(t, "auto answers", fmt.Sprint(wk["kyotsu_auto_answers"]), "[20 4 0 0 0 0 0]")
	prev := e.alice.mustStatus(200, "GET", "/v1/track/weeks/2026-10-03", nil)
	eq(t, "previous week", fmt.Sprint(prev["kyotsu_auto"]), "[<nil> <nil> <nil> <nil> <nil> <nil> 100]")

	// Sessions alone don't make a logged week.
	eq(t, "unlogged", len(e.alice.mustStatus(200, "GET", "/v1/track/weeks", nil)["weeks"].([]any)), 0)

	// Manual overrides auto per day; the weekly score averages the effective days.
	saved := e.alice.saveWeek(thisWeek, map[string]any{"kotu": nums(80, nil, 90.5), "kyotsu": nums(nil, 90, 60)})
	eq(t, "manual kept separate", fmt.Sprint(saved["kyotsu"]), "[<nil> 90 60 <nil> <nil> <nil> <nil>]")
	eq(t, "auto still reported", fmt.Sprint(saved["kyotsu_auto"]), "[85 25 <nil> <nil> <nil> <nil> <nil>]")
	w0 := e.alice.mustStatus(200, "GET", "/v1/track/weeks", nil)["weeks"].([]any)[0].(map[string]any)
	eq(t, "kyotsu = mean(85, 90, 60)", w0["kyotsu"], any(78.3))
	eq(t, "kotu = mean(80, 90.5)", w0["kotu"], any(85.3))
	e.nextWeek()
	lb := e.anon().mustStatus(200, "GET", "/v1/track/leaderboard?period=last", nil)["users"].([]any)[0].(map[string]any)
	eq(t, "leaderboard kyotsu", lb["kyotsu"], any(78.3))
	tr := e.anon().mustStatus(200, "GET", fmt.Sprint("/v1/users/", e.alice.id, "/track"), nil)["weeks"].([]any)[0].(map[string]any)
	eq(t, "profile kyotsu", tr["kyotsu"], any(78.3))

	eq(t, "profile kyotsu days", fmt.Sprint(tr["kyotsu_days"]), "[85 90 60 <nil> <nil> <nil> <nil>]")
	eq(t, "profile kyotsu source", fmt.Sprint(tr["kyotsu_source"]), "[auto manual manual <nil> <nil> <nil> <nil>]")
	eq(t, "profile kotu days", fmt.Sprint(tr["kotu_days"]), "[80 <nil> 90.5 <nil> <nil> <nil> <nil>]")

	// Auto alone fills the weekly score when nothing manual is entered.
	e.alice.saveWeek(thisWeek, map[string]any{"summary": "x"})
	w0 = e.alice.mustStatus(200, "GET", "/v1/track/weeks", nil)["weeks"].([]any)[0].(map[string]any)
	eq(t, "auto-only kyotsu = mean(85, 25)", w0["kyotsu"], any(55.0))
	eq(t, "no kotu", w0["kotu"], any(nil))
}

func TestTrackWeeksList(t *testing.T) {
	e := newEnv(t)
	yt := e.alice.subcat("active", "YouTube")
	e.alice.saveWeek(thisWeek, map[string]any{"hours": []any{hrs("active", yt, 2), hrs("active", nil, 2.5), hrs("soroban", nil, 0.5), hrs("anki", nil, 1.5)}, "summary": "s"})
	e.alice.saveWeek("2026-09-26", map[string]any{"hours": []any{hrs("passive", nil, 1)}})
	e.alice.saveWeek("2025-10-11", map[string]any{"hours": []any{hrs("passive", nil, 1)}}) // 53 weeks back
	e.bob.saveWeek(thisWeek, map[string]any{"hours": []any{hrs("passive", nil, 9)}})

	weeks := e.alice.mustStatus(200, "GET", "/v1/track/weeks", nil)["weeks"].([]any)
	eq(t, "default 52 weeks", len(weeks), 2)
	w0, w1 := weeks[0].(map[string]any), weeks[1].(map[string]any)
	eq(t, "oldest first", w0["week"], any("2026-09-26"))
	eq(t, "has_summary false", w0["has_summary"], any(false))
	eq(t, "has_summary", w1["has_summary"], any(true))
	eq(t, "no summary text", w1["summary"], any(nil))
	eq(t, "totals", fmt.Sprint(w1["totals"]), "map[active:4.5 anki:1.5 output:0 passive:0 phonetic:0 soroban:0.5]")
	eq(t, "subtotals", fmt.Sprint(w1["subtotals"]), fmt.Sprintf("map[active:map[%d:2 none:2.5] anki:map[none:1.5] soroban:map[none:0.5]]", yt))

	all := e.alice.mustStatus(200, "GET", "/v1/track/weeks?from=2000-01-01", nil)["weeks"].([]any)
	eq(t, "from 2000", len(all), 3)
	one := e.alice.mustStatus(200, "GET", "/v1/track/weeks?from=2026-09-26&to=2026-10-03", nil)["weeks"].([]any)
	eq(t, "range", len(one), 1)
	eq(t, "from after to", e.alice.do("GET", "/v1/track/weeks?from=2026-10-10&to=2026-10-03", nil).Code, 400)
	eq(t, "bad from", e.alice.do("GET", "/v1/track/weeks?from=2026-10-11", nil).Code, 400)
	eq(t, "future to", e.alice.do("GET", "/v1/track/weeks?to=2026-10-17", nil).Code, 400)
}

func TestTrackVisibility(t *testing.T) {
	e := newEnv(t)
	sub := e.alice.subcat("active", "YouTube")
	e.alice.saveWeek("2026-10-03", map[string]any{"hours": []any{hrs("active", sub, 3)}, "summary": "public summary"})
	e.alice.saveWeek(thisWeek, map[string]any{"hours": []any{hrs("active", nil, 1)}, "summary": "secret summary", "summary_private": true})
	e.bob.saveWeek(thisWeek, map[string]any{"hours": []any{hrs("passive", nil, 2)}, "summary": "bob's week"})
	e.nextWeek() // thisWeek is over: everything above is published
	trackPath := fmt.Sprint("/v1/users/", e.alice.id, "/track")
	publicBodies := func() string {
		var b strings.Builder
		for _, p := range []string{trackPath, "/v1/track/summaries", "/v1/track/leaderboard?period=all",
			fmt.Sprint("/v1/track/summaries?user=", e.alice.id), fmt.Sprint("/v1/users/", e.alice.id),
			fmt.Sprint("/v1/users/", e.alice.id, "/card")} {
			for _, c := range []*client{e.anon(), e.bob} {
				b.WriteString(c.do("GET", p, nil).Body.String())
			}
		}
		return b.String()
	}
	if body := publicBodies(); strings.Contains(body, "secret summary") || strings.Contains(body, "summary_private") ||
		strings.Contains(body, "@example.com") {
		t.Fatalf("private data leaked: %s", body)
	}

	other := e.bob.mustStatus(200, "GET", trackPath, nil)
	eq(t, "user", other["user"].(map[string]any)["name"], any("Alice"))
	eq(t, "subcats", fmt.Sprint(other["subcats"]), fmt.Sprintf("[map[category:active id:%d name:YouTube]]", sub))
	ws := other["weeks"].([]any)
	eq(t, "weeks", len(ws), 2)
	eq(t, "public summary", ws[0].(map[string]any)["summary"], any("public summary"))
	eq(t, "private blanked", ws[1].(map[string]any)["summary"], any(""))
	eq(t, "no has_summary", ws[0].(map[string]any)["has_summary"], any(nil))
	eq(t, "totals", ws[1].(map[string]any)["totals"].(map[string]any)["active"], any(1.0))

	own := e.alice.mustStatus(200, "GET", trackPath, nil)["weeks"].([]any)
	eq(t, "owner sees private", own[1].(map[string]any)["summary"], any("secret summary"))
	eq(t, "owner flag", own[1].(map[string]any)["summary_private"], any(true))
	eq(t, "owner flag false", own[0].(map[string]any)["summary_private"], any(false))
	eq(t, "unknown user", e.anon().do("GET", "/v1/users/9999/track", nil).Code, 404)

	// Hidden: nothing to anyone else, everything to the owner.
	eq(t, "settings needs public", e.alice.do("POST", "/v1/track/settings", map[string]any{}).Code, 400)
	eq(t, "hide", fmt.Sprint(e.alice.mustStatus(200, "POST", "/v1/track/settings", map[string]any{"public": false})), "map[public:false]")
	eq(t, "config", e.alice.mustStatus(200, "GET", "/v1/track/config", nil)["public"], any(false))
	eq(t, "anon 404", e.anon().do("GET", trackPath, nil).Code, 404)
	eq(t, "bob 404", e.bob.do("GET", trackPath, nil).Code, 404)
	eq(t, "owner 200", e.alice.do("GET", trackPath, nil).Code, 200)
	if body := publicBodies(); strings.Contains(body, "public summary") {
		t.Fatalf("hidden user exposed: %s", body)
	}
	lb := e.anon().mustStatus(200, "GET", "/v1/track/leaderboard?period=all", nil)["users"].([]any)
	eq(t, "leaderboard without alice", len(lb), 1)
	eq(t, "only bob", lb[0].(map[string]any)["user"].(map[string]any)["name"], any("Bob"))
	eq(t, "feed without alice", len(e.anon().mustStatus(200, "GET", fmt.Sprint("/v1/track/summaries?user=", e.alice.id), nil)["summaries"].([]any)), 0)
	eq(t, "profile itself still public", e.anon().do("GET", fmt.Sprint("/v1/users/", e.alice.id), nil).Code, 200)
	e.alice.mustStatus(200, "POST", "/v1/track/settings", map[string]any{"public": true})
	eq(t, "visible again", e.anon().do("GET", trackPath, nil).Code, 200)
}

func TestLeaderboard(t *testing.T) {
	e := newEnv(t)
	e.alice.saveWeek(thisWeek, map[string]any{"hours": []any{hrs("active", nil, 5), hrs("passive", nil, 5), hrs("output", nil, 1), hrs("anki", nil, 3)}, "kotu": nums(80)})
	e.alice.saveWeek("2026-10-03", map[string]any{"hours": []any{hrs("active", nil, 12)}, "kotu": nums(60, 70)})
	e.alice.saveWeek("2026-09-05", map[string]any{"hours": []any{hrs("passive", nil, 1)}})
	e.bob.saveWeek(thisWeek, map[string]any{"hours": []any{hrs("active", nil, 20.5)}, "kyotsu": nums(50)})
	e.admin.saveWeek("2026-10-03", map[string]any{"summary": "words only"}) // logged, zero hours
	e.nextWeek()                                                            // thisWeek is the last published week
	type row struct {
		name   string
		imm    float64
		logged int
	}
	board := func(q string) (map[string]any, []row) {
		out := e.anon().mustStatus(200, "GET", "/v1/track/leaderboard"+q, nil)
		var rows []row
		for _, u := range out["users"].([]any) {
			m := u.(map[string]any)
			rows = append(rows, row{m["user"].(map[string]any)["name"].(string), m["totals"].(map[string]any)["all"].(float64), int(m["weeks_logged"].(float64))})
			for k, s := range m["series"].(map[string]any) {
				if len(s.([]any)) != len(out["weeks"].([]any)) {
					t.Fatalf("%s: series %s has %d points for %d weeks", q, k, len(s.([]any)), len(out["weeks"].([]any)))
				}
			}
		}
		return out, rows
	}

	out, rows := board("?period=last")
	eq(t, "period", out["period"], any("last"))
	eq(t, "weeks", fmt.Sprint(out["weeks"]), "["+thisWeek+"]")
	eq(t, "from/to", fmt.Sprint(out["from"], " ", out["to"]), thisWeek+" "+thisWeek)
	eq(t, "last ranking (by active)", fmt.Sprint(rows), "[{Bob 20.5 1} {Alice 14 1}]")
	alias, aliasRows := board("?period=week")
	eq(t, "week is an alias of last", alias["period"], any("last"))
	eq(t, "alias rows", fmt.Sprint(aliasRows), fmt.Sprint(rows))

	out, rows = board("?period=4w")
	eq(t, "4w", fmt.Sprint(out["weeks"]), "[2026-09-19 2026-09-26 2026-10-03 2026-10-10]")
	eq(t, "4w ranking", fmt.Sprint(rows), "[{Bob 20.5 1} {Alice 26 2} {Admin 0 1}]") // active 20.5 > 17
	alice := out["users"].([]any)[1].(map[string]any)
	eq(t, "totals", fmt.Sprint(alice["totals"]), "map[active:17 all:26 anki:3 immersion:22 nopassive:21 output:1 passive:5 phonetic:0 soroban:0]")
	eq(t, "kotu avg of weekly means", alice["kotu"], any(72.5)) // mean(80, 65)
	eq(t, "no kyotsu", alice["kyotsu"], any(nil))
	series := alice["series"].(map[string]any)
	eq(t, "active series", fmt.Sprint(series["active"]), "[<nil> <nil> 12 5]")
	eq(t, "all series", fmt.Sprint(series["all"]), "[<nil> <nil> 12 14]")
	eq(t, "nopassive series", fmt.Sprint(series["nopassive"]), "[<nil> <nil> 12 9]")
	eq(t, "anki series", fmt.Sprint(series["anki"]), "[<nil> <nil> 0 3]")
	eq(t, "kotu series", fmt.Sprint(series["kotu"]), "[<nil> <nil> 65 80]")
	eq(t, "bob kyotsu", out["users"].([]any)[0].(map[string]any)["kyotsu"], any(50.0))

	_, rows = board("?period=12w")
	eq(t, "12w", fmt.Sprint(rows), "[{Bob 20.5 1} {Alice 27 3} {Admin 0 1}]")
	out, _ = board("?period=all")
	eq(t, "all starts at first logged week", out["from"], any("2026-09-05"))
	eq(t, "all weeks", len(out["weeks"].([]any)), 6)
	out, rows = board("?period=last&week=2026-10-03")
	eq(t, "past week", fmt.Sprint(rows), "[{Alice 12 1} {Admin 0 1}]")
	out, rows = board("?period=4w&week=2026-08-01")
	eq(t, "empty range", len(rows), 0)
	eq(t, "empty users is a list", fmt.Sprint(out["users"]), "[]")
	_, rows = board("")
	eq(t, "default period 4w", len(rows), 3)
	eq(t, "bad period", e.anon().do("GET", "/v1/track/leaderboard?period=2w", nil).Code, 400)
	eq(t, "future week", e.anon().do("GET", "/v1/track/leaderboard?week=2026-10-24", nil).Code, 400)
	eq(t, "non-saturday", e.anon().do("GET", "/v1/track/leaderboard?week=2026-10-11", nil).Code, 400)
	out, _ = board("?period=last&week=2026-10-17") // the current week: clamped to published-through
	eq(t, "clamped", fmt.Sprint(out["to"], " ", out["published_through"]), thisWeek+" "+thisWeek)
}

func TestSummariesFeed(t *testing.T) {
	e := newEnv(t)
	save := func(c *client, key, summary string, private bool) {
		c.saveWeek(key, map[string]any{"hours": []any{hrs("active", nil, 1)}, "summary": summary, "summary_private": private})
		e.clock = e.clock.Add(time.Minute) // distinct updated_at
	}
	save(e.alice, "2026-09-26", "a1", false)
	save(e.bob, "2026-10-03", "b2", false)
	save(e.alice, "2026-10-03", "a2", false) // same week as b2, updated later
	save(e.admin, "2026-10-03", "", false)   // no summary
	save(e.bob, thisWeek, "b3 secret", true)
	save(e.alice, thisWeek, "a3", false)
	e.nextWeek()
	feed := func(q string) []string {
		var out []string
		for _, s := range e.anon().mustStatus(200, "GET", "/v1/track/summaries"+q, nil)["summaries"].([]any) {
			m := s.(map[string]any)
			out = append(out, m["week"].(string)+":"+m["summary"].(string))
		}
		return out
	}
	eqStrings(t, "feed", feed(""), []string{"2026-10-10:a3", "2026-10-03:a2", "2026-10-03:b2", "2026-09-26:a1"})
	eqStrings(t, "before", feed("?before=2026-10-10"), []string{"2026-10-03:a2", "2026-10-03:b2", "2026-09-26:a1"})
	eqStrings(t, "before next week", feed("?before=2026-10-17"), feed(""))
	eqStrings(t, "user", feed(fmt.Sprint("?user=", e.bob.id)), []string{"2026-10-03:b2"})
	eqStrings(t, "user+before", feed(fmt.Sprint("?user=", e.alice.id, "&before=2026-10-03")), []string{"2026-09-26:a1"})
	eqStrings(t, "limit", feed("?limit=1"), []string{"2026-10-10:a3"})
	// A page never ends mid-week, so paging by week loses nothing.
	eqStrings(t, "limit inside a week", feed("?limit=2"), []string{"2026-10-10:a3", "2026-10-03:a2", "2026-10-03:b2"})
	eqStrings(t, "next page", feed("?limit=2&before=2026-10-03"), []string{"2026-09-26:a1"})

	item := e.anon().mustStatus(200, "GET", "/v1/track/summaries?limit=1", nil)["summaries"].([]any)[0].(map[string]any)
	eq(t, "user", item["user"].(map[string]any)["name"], any("Alice"))
	eq(t, "totals", item["totals"].(map[string]any)["active"], any(1.0))
	if _, err := time.Parse(time.RFC3339, item["updated"].(string)); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"?limit=0", "?limit=x", "?user=x", "?before=2026-10-11", "?before=2026-10-31"} {
		eq(t, "bad "+q, e.anon().do("GET", "/v1/track/summaries"+q, nil).Code, 400)
	}
}

func TestBioAndCard(t *testing.T) {
	e := newEnv(t)
	set := func(body map[string]any) int { return e.alice.do("POST", "/v1/me/profile", body).Code }
	eq(t, "bio", set(map[string]any{"bio": "  Learning Kansai-ben.\r\nLikes: Higuchi\n  "}), 200)
	eq(t, "bio not on User", e.alice.last.Body.String(), strings.Replace(e.alice.last.Body.String(), "bio", "", 1))
	eq(t, "name kept", e.alice.mustStatus(200, "GET", "/v1/me", nil)["user"].(map[string]any)["name"], any("Alice"))
	eq(t, "both", set(map[string]any{"name": "Alicia", "bio": "大阪"}), 200)
	eq(t, "name only keeps bio", set(map[string]any{"name": "Alice"}), 200)
	eq(t, "neither", set(map[string]any{}), 400)
	eq(t, "300 runes", set(map[string]any{"bio": strings.Repeat("字", 300)}), 200)
	eq(t, "6 lines", set(map[string]any{"bio": "1\n2\n3\n4\n5\n6"}), 200)
	for name, bio := range map[string]any{
		"301 runes": strings.Repeat("字", 301), "7 lines": "1\n2\n3\n4\n5\n6\n7", "tab": "a\tb", "nul": "a\x00b",
		"escape": "a\x1b[31mb", "number": 5,
	} {
		eq(t, name, set(map[string]any{"bio": bio}), 400)
	}
	eq(t, "bad name, good bio", set(map[string]any{"name": "", "bio": "ok"}), 400)
	eq(t, "clear", set(map[string]any{"bio": "Hi\n\nthere"}), 200)

	prof := e.anon().mustStatus(200, "GET", fmt.Sprint("/v1/users/", e.alice.id), nil)
	eq(t, "profile bio", prof["bio"], any("Hi\n\nthere"))
	e.alice.post(map[string]any{"body": "x"})
	id := e.alice.post(map[string]any{"body": "y"})
	e.bob.mustStatus(200, "POST", fmt.Sprintf("/v1/comments/%d/like", id), nil)
	rec := e.anon().do("GET", fmt.Sprint("/v1/users/", e.alice.id, "/card"), nil)
	eq(t, "card", rec.Code, 200)
	eq(t, "card cache", rec.Header().Get("Cache-Control"), "private, max-age=60")
	if strings.Contains(rec.Body.String(), "@example.com") || strings.Contains(rec.Body.String(), "email") || strings.Contains(rec.Body.String(), "recent") {
		t.Fatalf("card body %s", rec.Body)
	}
	card := e.anon().mustStatus(200, "GET", fmt.Sprint("/v1/users/", e.alice.id, "/card"), nil)
	eq(t, "card bio", card["bio"], any("Hi\n\nthere"))
	eq(t, "card comments", card["comments"], any(2.0))
	eq(t, "card likes", card["likes"], any(1.0))
	eq(t, "card level", card["user"].(map[string]any)["xp"], any(1.0))
	if _, err := time.Parse(time.RFC3339, card["joined"].(string)); err != nil {
		t.Fatal(err)
	}
	eq(t, "card 404", e.anon().do("GET", "/v1/users/9999/card", nil).Code, 404)
	if strings.Contains(e.anon().do("GET", "/v1/comments?page="+page, nil).Body.String(), "bio") {
		t.Fatal("bio in comment payload")
	}
}
