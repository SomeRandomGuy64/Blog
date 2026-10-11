package api

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// kyoSession posts a trainer session that started span ago with the given active_seconds (nil
// = the field left out, as older trainers do) and client id ("" = none).
func (c *client) kyoSession(want int, span time.Duration, active any, clientID string, n int) map[string]any {
	c.e.t.Helper()
	body := map[string]any{"started": c.e.clock.Add(-span).Format(time.RFC3339), "layout": "kotu", "answers": answers(n, n/2)}
	if active != nil {
		body["active_seconds"] = active
	}
	if clientID != "" {
		body["client_id"] = clientID
	}
	return c.mustStatus(want, "POST", "/v1/kyotsu/sessions", body)
}

// phonetic is the user's phonetic hours in a week as "subcat-name:hours" (unassigned = "-"),
// in the API's order.
func (c *client) phonetic(key string) string {
	c.e.t.Helper()
	names := map[string]string{}
	for _, x := range c.mustStatus(200, "GET", "/v1/track/config", nil)["subcats"].([]any) {
		sc := x.(map[string]any)
		names[fmt.Sprint(sc["id"])] = fmt.Sprint(sc["name"])
	}
	var out []string
	for _, x := range c.mustStatus(200, "GET", "/v1/track/weeks/"+key, nil)["hours"].([]any) {
		h := x.(map[string]any)
		if h["category"] != "phonetic" {
			continue
		}
		n := "-"
		if h["subcat"] != nil {
			n = names[fmt.Sprint(h["subcat"])]
		}
		out = append(out, fmt.Sprint(n, ":", h["hours"]))
	}
	return strings.Join(out, " ")
}

func (c *client) phoneticSubcats() string {
	c.e.t.Helper()
	var out []string
	for _, x := range c.mustStatus(200, "GET", "/v1/track/config", nil)["subcats"].([]any) {
		sc := x.(map[string]any)
		if sc["category"] == "phonetic" {
			out = append(out, fmt.Sprint(sc["name"], map[bool]string{true: "(archived)", false: ""}[sc["archived"].(bool)]))
		}
	}
	return strings.Join(out, ",")
}

func added(out map[string]any) map[string]any {
	ta, _ := out["track_added"].(map[string]any)
	return ta
}

func TestKyotsuTimeToTrack(t *testing.T) {
	e := newEnv(t)
	a := e.alice

	// First session: 12:34 active → 13 min → 0.22 h, into a new "Kyōtsu" sub-category.
	out := a.kyoSession(201, 20*time.Minute, 754, "", 10)
	eq(t, "active stored", out["active_seconds"], any(754.0))
	ta := added(out)
	eq(t, "week", ta["week"], any(thisWeek))
	eq(t, "hours", ta["hours"], any(0.22))
	eq(t, "subcat", ta["subcat"], any("Kyōtsu"))
	eq(t, "note", ta["note"], any("Created a “Kyōtsu” sub-category under Phonetic training for it."))
	eq(t, "track", a.phonetic(thisWeek), "Kyōtsu:0.22")
	eq(t, "subcats", a.phoneticSubcats(), "Kyōtsu")
	wk := a.mustStatus(200, "GET", "/v1/track/weeks/"+thisWeek, nil)
	eq(t, "week logged, updated", wk["updated"], any(e.clock.Format(time.RFC3339)))

	// The stats page reads it back from the session.
	id := int64(out["id"].(float64))
	one := a.mustStatus(200, "GET", fmt.Sprint("/v1/kyotsu/sessions/", id), nil)
	eq(t, "detail active", one["active_seconds"], any(754.0))
	eq(t, "detail added", fmt.Sprint(added(one)), fmt.Sprint(ta))
	list := a.mustStatus(200, "GET", "/v1/kyotsu/sessions", nil)["sessions"].([]any)
	eq(t, "list added", fmt.Sprint(added(list[0].(map[string]any))["hours"]), "0.22")

	// A second session adds to the same row, keeping what else is in the week.
	a.saveWeek(thisWeek, map[string]any{"hours": []any{hrs("phonetic", nil, 1), hrs("active", nil, 2),
		hrs("phonetic", int64(a.mustStatus(200, "GET", "/v1/track/config", nil)["subcats"].([]any)[0].(map[string]any)["id"].(float64)), 0.22)},
		"summary": "keep me", "kotu": nums(80)})
	e.clock = e.clock.Add(time.Hour)
	out = a.kyoSession(201, 40*time.Minute, 30*60, "", 10)
	eq(t, "second: no note", added(out)["note"], any(nil))
	eq(t, "second: hours", added(out)["hours"], any(0.5))
	eq(t, "summed", a.phonetic(thisWeek), "Kyōtsu:0.72 -:1")
	wk = a.mustStatus(200, "GET", "/v1/track/weeks/"+thisWeek, nil)
	eq(t, "summary kept", wk["summary"], any("keep me"))
	eq(t, "kotu kept", fmt.Sprint(wk["kotu"]), "[80 <nil> <nil> <nil> <nil> <nil> <nil>]")
	eq(t, "updated bumped", wk["updated"], any(e.clock.Format(time.RFC3339)))

	// Logged out: nothing is saved or added.
	eq(t, "logged out", e.anon().do("POST", "/v1/kyotsu/sessions", map[string]any{"started": e.clock.Format(time.RFC3339),
		"layout": "kotu", "answers": answers(1, 1), "active_seconds": 60}).Code, 401)

	// Someone else's track is untouched.
	eq(t, "bob untouched", e.bob.phonetic(thisWeek), "")
}

func TestKyotsuTimeSubcatChoice(t *testing.T) {
	e := newEnv(t)

	// An existing active sub-category named Kyotsu/kyōtsu (any case) is used as it is.
	a := e.alice
	a.subcat("phonetic", "Shadowing")
	a.subcat("phonetic", "kyotsu")
	out := a.kyoSession(201, time.Hour, 600, "", 4)
	eq(t, "existing", added(out)["subcat"], any("kyotsu"))
	eq(t, "no note", added(out)["note"], any(nil))
	eq(t, "track", a.phonetic(thisWeek), "kyotsu:0.17")
	eq(t, "nothing created", a.phoneticSubcats(), "Shadowing,kyotsu")
	// …and a same-named sub-category of another category doesn't count.
	e.bob.subcat("active", "Kyōtsu")
	out = e.bob.kyoSession(201, time.Hour, 600, "", 4)
	eq(t, "other category ignored", added(out)["subcat"], any("Kyōtsu"))
	eq(t, "created in phonetic", e.bob.phoneticSubcats(), "Kyōtsu")

	// An archived one is restored rather than duplicated.
	c := e.admin
	id := c.subcat("phonetic", "KYŌTSU")
	c.mustStatus(200, "POST", fmt.Sprint("/v1/track/subcats/", id), map[string]any{"archived": true})
	out = c.kyoSession(201, time.Hour, 600, "", 4)
	eq(t, "restored", added(out)["subcat"], any("KYŌTSU"))
	eq(t, "restored note", added(out)["note"], any("Restored your archived “KYŌTSU” sub-category under Phonetic training for it."))
	eq(t, "unarchived", c.phoneticSubcats(), "KYŌTSU")
}

func TestKyotsuTimeSubcatLimit(t *testing.T) {
	e := newEnv(t)
	a := e.alice
	// 30 active phonetic sub-categories: no room to create one, so the unassigned bucket.
	for i := range maxActiveSubcats {
		if _, err := e.db.Exec(`INSERT INTO track_subcats (user_id, category, name, position, created_at) VALUES (?, 'phonetic', ?, ?, 0)`,
			a.id, fmt.Sprint("S", i), i); err != nil {
			t.Fatal(err)
		}
	}
	out := a.kyoSession(201, time.Hour, 600, "", 4)
	ta := added(out)
	eq(t, "unassigned", ta["subcat"], any(nil))
	eq(t, "hours", ta["hours"], any(0.17))
	if n, _ := ta["note"].(string); !strings.Contains(n, "at most 30 active") || !strings.Contains(n, "Other / unassigned") {
		t.Fatalf("note %q", n)
	}
	eq(t, "track", a.phonetic(thisWeek), "-:0.17")

	// Same with an archived Kyōtsu: it can't be restored past the limit either.
	if _, err := e.db.Exec(`INSERT INTO track_subcats (user_id, category, name, archived, created_at) VALUES (?, 'phonetic', 'Kyōtsu', 1, 0)`, a.id); err != nil {
		t.Fatal(err)
	}
	out = a.kyoSession(201, time.Hour, 600, "", 4)
	eq(t, "still unassigned", added(out)["subcat"], any(nil))
	eq(t, "track", a.phonetic(thisWeek), "-:0.34")
}

func TestKyotsuTimeWeekBoundary(t *testing.T) {
	e := newEnv(t)
	a := e.alice
	// The week of 2026-10-10 ends Sun 2026-10-18 03:00 UTC.
	e.clock = time.Date(2026, 10, 18, 2, 59, 59, 0, time.UTC)
	eq(t, "last second", added(a.kyoSession(201, time.Hour, 600, "", 2))["week"], any("2026-10-10"))
	e.clock = time.Date(2026, 10, 18, 3, 0, 0, 0, time.UTC)
	// The session started in the old week but ended in the new one: it counts where it ended.
	eq(t, "first second", added(a.kyoSession(201, time.Hour, 1200, "", 2))["week"], any("2026-10-17"))
	eq(t, "old week", a.phonetic("2026-10-10"), "Kyōtsu:0.17")
	eq(t, "new week", a.phonetic("2026-10-17"), "Kyōtsu:0.33")
}

func TestKyotsuTimeClamp(t *testing.T) {
	e := newEnv(t)
	a := e.alice
	for _, c := range []struct {
		name   string
		span   time.Duration
		active any
		stored float64
		hours  any // nil = nothing added
	}{
		{"missing (old trainer)", time.Hour, nil, 0, nil},
		{"zero", time.Hour, 0, 0, nil},
		{"negative", time.Hour, -500, 0, nil},
		{"under half a minute", time.Hour, 29, 29, nil},
		{"half a minute rounds up", time.Hour, 30, 30, 0.02},      // 1 min
		{"fractional", time.Hour, 89.6, 90, 0.03},                 // 90 s → 2 min
		{"more than the span", 10 * time.Minute, 3600, 600, 0.17}, // 10 min
		{"absurd", 2 * time.Hour, 1e300, 7200, 2.0},
		{"future start", -time.Hour, 600, 0, nil}, // start clamped to now: no span
	} {
		out := a.kyoSession(201, c.span, c.active, "", 2)
		eq(t, c.name+": stored", out["active_seconds"], any(c.stored))
		if c.hours == nil {
			eq(t, c.name+": nothing added", out["track_added"], any(nil))
		} else {
			eq(t, c.name+": added", added(out)["hours"], c.hours)
		}
	}
	eq(t, "total", a.phonetic(thisWeek), "Kyōtsu:2.22")
	eq(t, "not a number", a.do("POST", "/v1/kyotsu/sessions", map[string]any{"started": e.clock.Format(time.RFC3339),
		"layout": "kotu", "answers": answers(1, 1), "active_seconds": "12"}).Code, 400)
}

func TestKyotsuTimeCaps(t *testing.T) {
	e := newEnv(t)
	a := e.alice
	a.saveWeek(thisWeek, map[string]any{"hours": []any{hrs("phonetic", nil, 167.9)}})
	out := a.kyoSession(201, time.Hour, 1800, "", 2) // 0.5 h, only 0.1 h of room
	ta := added(out)
	eq(t, "capped", ta["hours"], any(0.1))
	eq(t, "note", ta["note"], any("Created a “Kyōtsu” sub-category under Phonetic training for it. Only 0.1 h of 0.5 h was added: Phonetic training can't go over 168 h in a week."))
	eq(t, "track", a.phonetic(thisWeek), "Kyōtsu:0.1 -:167.9")

	// Full: the session still saves, nothing is added and that's said.
	before := a.phoneticSubcats()
	out = a.kyoSession(201, time.Hour, 1800, "", 2)
	eq(t, "full", added(out)["hours"], any(0.0))
	eq(t, "full note", added(out)["note"], any("Nothing was added: Phonetic training already has 168 h this week."))
	eq(t, "unchanged", a.phonetic(thisWeek), "Kyōtsu:0.1 -:167.9")
	eq(t, "no sub-category made", a.phoneticSubcats(), before)

	// Other categories don't count towards phonetic's 168 h.
	b := e.bob
	b.saveWeek(thisWeek, map[string]any{"hours": []any{hrs("active", nil, 168)}})
	eq(t, "other cats", added(b.kyoSession(201, time.Hour, 1800, "", 2))["hours"], any(0.5))
}

func TestKyotsuTimeIdempotent(t *testing.T) {
	e := newEnv(t)
	a := e.alice
	out := a.kyoSession(201, time.Hour, 600, "run-0123456789", 4)
	id := out["id"]
	eq(t, "first", added(out)["hours"], any(0.17))

	// The same save again (response lost, user pressed End session again): same session, no more time.
	e.clock = e.clock.Add(5 * time.Second)
	out = a.kyoSession(200, time.Hour, 600, "run-0123456789", 4)
	eq(t, "same id", out["id"], id)
	eq(t, "same added", added(out)["hours"], any(0.17))
	eq(t, "one session", len(a.mustStatus(200, "GET", "/v1/kyotsu/sessions", nil)["sessions"].([]any)), 1)
	eq(t, "not doubled", a.phonetic(thisWeek), "Kyōtsu:0.17")

	// Retried later with more answers and more time: answers replaced, only the difference added.
	e.clock = e.clock.Add(10 * time.Minute)
	out = a.kyoSession(200, time.Hour, 1200, "run-0123456789", 8)
	eq(t, "same id again", out["id"], id)
	eq(t, "cumulative", added(out)["hours"], any(0.33))
	one := a.mustStatus(200, "GET", fmt.Sprint("/v1/kyotsu/sessions/", id), nil)
	eq(t, "answers replaced", one["total"], any(8.0))
	eq(t, "active", one["active_seconds"], any(1200.0))
	eq(t, "difference added", a.phonetic(thisWeek), "Kyōtsu:0.33")

	// Fewer answers / less time never take anything back.
	out = a.kyoSession(200, time.Hour, 60, "run-0123456789", 2)
	eq(t, "kept", added(out)["hours"], any(0.33))
	eq(t, "kept answers", a.mustStatus(200, "GET", fmt.Sprint("/v1/kyotsu/sessions/", id), nil)["total"], any(8.0))

	// Client ids are per user; another run is another session.
	eq(t, "bob same client id", e.bob.kyoSession(201, time.Hour, 600, "run-0123456789", 4)["id"] != id, true)
	eq(t, "another run", a.kyoSession(201, time.Hour, 600, "run-abcdefghij", 4)["id"] != id, true)
	eq(t, "two sessions", len(a.mustStatus(200, "GET", "/v1/kyotsu/sessions", nil)["sessions"].([]any)), 2)
	eq(t, "bad client id", a.do("POST", "/v1/kyotsu/sessions", map[string]any{"started": e.clock.Format(time.RFC3339),
		"layout": "kotu", "answers": answers(1, 1), "client_id": "a b"}).Code, 400)
}

func TestWeekSaveIfUpdated(t *testing.T) {
	e := newEnv(t)
	a := e.alice
	save := func(ifUpdated any) int {
		body := map[string]any{"hours": []any{hrs("active", nil, 1)}}
		if ifUpdated != "absent" {
			body["if_updated"] = ifUpdated
		}
		return a.do("POST", "/v1/track/weeks/"+thisWeek, body).Code
	}
	// An unlogged week: "" means "it had no row when I loaded it".
	eq(t, "unlogged + \"\"", save(""), 200)
	eq(t, "logged now + \"\"", save(""), 409)
	loaded := a.mustStatus(200, "GET", "/v1/track/weeks/"+thisWeek, nil)["updated"].(string)
	eq(t, "matching", save(loaded), 200)

	// The logger loaded the week; then the trainer added time (later, in another tab).
	// All in the same second as the last save: updated still moves on.
	stale := a.mustStatus(200, "GET", "/v1/track/weeks/"+thisWeek, nil)["updated"].(string)
	a.kyoSession(201, time.Hour, 600, "", 2)
	eq(t, "updated moved on", a.mustStatus(200, "GET", "/v1/track/weeks/"+thisWeek, nil)["updated"] != stale, true)
	eq(t, "stale", save(stale), 409)
	if m := errMsg(a.last); !strings.Contains(m, "changed elsewhere") {
		t.Fatalf("409 message %q", m)
	}
	eq(t, "time survived", a.phonetic(thisWeek), "Kyōtsu:0.17")
	eq(t, "null = no check", save(nil), 200)
	eq(t, "absent = no check (old behaviour)", save("absent"), 200)
	eq(t, "wiped by an unchecked save", a.phonetic(thisWeek), "")
	eq(t, "bad", save("yesterday"), 400)

	// After its own save the logger has the new updated, so its next save goes through.
	e.clock = e.clock.Add(time.Minute)
	w := a.saveWeek(thisWeek, map[string]any{"hours": []any{hrs("active", nil, 2)}})
	eq(t, "fresh", save(w["updated"]), 200)
}
