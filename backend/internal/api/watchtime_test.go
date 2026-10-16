package api

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// watch posts a watch-session heartbeat for a session that started at started, with the
// cumulative active_seconds (nil = the field left out) and video id ("" = none).
func (c *client) watch(want int, clientID string, started time.Time, active any, videoID string) map[string]any {
	c.e.t.Helper()
	body := map[string]any{"client_id": clientID, "started": started.Format(time.RFC3339)}
	if active != nil {
		body["active_seconds"] = active
	}
	if videoID != "" {
		body["video_id"] = videoID
	}
	return c.mustStatus(want, "POST", "/v1/watch/sessions", body)
}

func TestWatchTimeToTrack(t *testing.T) {
	e := newEnv(t)
	a := e.alice
	start := e.clock
	e.clock = start.Add(90 * time.Second)

	// The first heartbeat stores the session and adds its time: 61 s → 1 min → 0.02 h, into a
	// new "Primed Listening" sub-category of Active immersion.
	out := a.watch(201, "watch-0123456789", start, 61, "dQw4w9WgXcQ")
	id := out["id"]
	eq(t, "active stored", out["active_seconds"], any(61.0))
	ta := added(out)
	eq(t, "week", ta["week"], any(thisWeek))
	eq(t, "hours", ta["hours"], any(0.02))
	eq(t, "subcat", ta["subcat"], any("Primed Listening"))
	eq(t, "note", ta["note"], any("Created a “Primed Listening” sub-category under Active immersion for it."))
	eq(t, "track", a.catHours("active", thisWeek), "Primed Listening:0.02")
	eq(t, "subcats", a.catSubcats("active"), "Primed Listening")
	wk := a.mustStatus(200, "GET", "/v1/track/weeks/"+thisWeek, nil)
	eq(t, "week logged, updated", wk["updated"], any(e.clock.Format(time.RFC3339)))

	// Later heartbeats update the same session and add only the difference: 2 min = 0.03 h in all.
	e.clock = e.clock.Add(time.Minute)
	out = a.watch(200, "watch-0123456789", start, 121, "")
	eq(t, "same id", out["id"], id)
	eq(t, "cumulative", added(out)["hours"], any(0.03))
	eq(t, "note kept", added(out)["note"], any("Created a “Primed Listening” sub-category under Active immersion for it."))
	eq(t, "difference added", a.catHours("active", thisWeek), "Primed Listening:0.03")

	// Within the same minute nothing more is added; a lower value takes nothing back.
	e.clock = e.clock.Add(10 * time.Second)
	eq(t, "same minute", added(a.watch(200, "watch-0123456789", start, 140, ""))["hours"], any(0.03))
	out = a.watch(200, "watch-0123456789", start, 20, "")
	eq(t, "never down", out["active_seconds"], any(140.0))
	eq(t, "kept", added(out)["hours"], any(0.03))
	e.clock = e.clock.Add(time.Minute)
	eq(t, "3 min", added(a.watch(200, "watch-0123456789", start, 180, ""))["hours"], any(0.05))
	eq(t, "track", a.catHours("active", thisWeek), "Primed Listening:0.05")

	// Other things in the week stay as they were.
	a.saveWeek(thisWeek, map[string]any{"hours": []any{hrs("active", nil, 2),
		hrs("active", int64(a.mustStatus(200, "GET", "/v1/track/config", nil)["subcats"].([]any)[0].(map[string]any)["id"].(float64)), 0.05)},
		"summary": "keep me"})
	e.clock = e.clock.Add(time.Minute)
	a.watch(200, "watch-0123456789", start, 240, "")
	eq(t, "summed", a.catHours("active", thisWeek), "Primed Listening:0.07 -:2")
	eq(t, "summary kept", a.mustStatus(200, "GET", "/v1/track/weeks/"+thisWeek, nil)["summary"], any("keep me"))

	// Logged out: nothing is saved or added. Someone else's Track is untouched, and their
	// client ids are their own.
	eq(t, "logged out", e.anon().do("POST", "/v1/watch/sessions", map[string]any{"client_id": "watch-0123456789",
		"started": start.Format(time.RFC3339), "active_seconds": 60}).Code, 401)
	eq(t, "bob untouched", e.bob.catHours("active", thisWeek), "")
	b := e.bob.watch(201, "watch-0123456789", e.clock.Add(-time.Hour), 600, "")
	eq(t, "bob's own session", b["id"] != id, true)
	eq(t, "bob's track", e.bob.catHours("active", thisWeek), "Primed Listening:0.17")
	eq(t, "alice unchanged", a.catHours("active", thisWeek), "Primed Listening:0.07 -:2")
	eq(t, "another run", a.watch(201, "watch-abcdefghij", e.clock, 0, "")["id"] != id, true)
}

// A few hours of heartbeats every minute add exactly the session's total and never hit the
// rate limit.
func TestWatchTimeLongSession(t *testing.T) {
	e := newEnv(t)
	a := e.alice
	start := e.clock
	for i := 1; i <= 180; i++ {
		e.clock = start.Add(time.Duration(i) * time.Minute)
		a.watch(map[bool]int{true: 201, false: 200}[i == 1], "long-session", start, i*60, "")
	}
	eq(t, "3 h", a.catHours("active", thisWeek), "Primed Listening:3")
	// A last one on pause, same value: nothing changes.
	eq(t, "final", added(a.watch(200, "long-session", start, 180*60, ""))["hours"], any(3.0))

	// The limit is per user, 300 an hour.
	b := e.bob
	for i := range 300 {
		b.watch(map[bool]int{true: 201, false: 200}[i == 0], "busy-bob-1", e.clock, 0, "")
	}
	eq(t, "limited", b.do("POST", "/v1/watch/sessions", map[string]any{"client_id": "busy-bob-1",
		"started": e.clock.Format(time.RFC3339), "active_seconds": 0}).Code, 429)
	e.clock = e.clock.Add(time.Hour + time.Second)
	b.watch(200, "busy-bob-1", e.clock.Add(-time.Hour), 0, "")
}

func TestWatchTimeClamp(t *testing.T) {
	e := newEnv(t)
	a := e.alice
	// Never more than the time since the session started.
	start := e.clock.Add(-10 * time.Minute)
	out := a.watch(201, "clamp-span", start, 3600, "")
	eq(t, "span", out["active_seconds"], any(600.0))
	eq(t, "span hours", added(out)["hours"], any(0.17))
	// …the stored start: a later heartbeat can't move it back.
	e.clock = e.clock.Add(time.Minute)
	out = a.watch(200, "clamp-span", start.Add(-2*time.Hour), 7200, "")
	eq(t, "stored start", out["active_seconds"], any(660.0))
	eq(t, "stored start hours", added(out)["hours"], any(0.18))

	// Fractions round; absurd values stop at the span, and the span at 24 h.
	eq(t, "fraction", a.watch(201, "clamp-frac", e.clock.Add(-time.Hour), 89.6, "")["active_seconds"], any(90.0))
	long := e.clock.Add(-23 * time.Hour)
	a.watch(201, "clamp-long", long, 0, "")
	e.clock = e.clock.Add(3 * time.Hour)
	out = a.watch(200, "clamp-long", long, 1e300, "") // started is now over 24 h ago: fine, it's stored
	eq(t, "24 h", out["active_seconds"], any(86400.0))
	eq(t, "24 h hours", added(out)["hours"], any(24.0))

	// A client clock a little ahead: the session starts now, so nothing yet.
	out = a.watch(201, "clamp-ahead", e.clock.Add(4*time.Minute), 60, "")
	eq(t, "ahead", out["active_seconds"], any(0.0))
	eq(t, "ahead adds nothing", out["track_added"], any(nil))
	e.clock = e.clock.Add(2 * time.Minute)
	eq(t, "then counts", a.watch(200, "clamp-ahead", e.clock, 60, "")["active_seconds"], any(60.0))
}

func TestWatchTimeValidation(t *testing.T) {
	e := newEnv(t)
	a := e.alice
	now := e.clock.Format(time.RFC3339)
	for _, c := range []struct {
		name string
		body map[string]any
		msg  string
	}{
		{"no client id", map[string]any{"started": now, "active_seconds": 1}, "client_id"},
		{"short client id", map[string]any{"client_id": "abc", "started": now, "active_seconds": 1}, "client_id"},
		{"bad client id", map[string]any{"client_id": "a b c d e f", "started": now, "active_seconds": 1}, "client_id"},
		{"no started", map[string]any{"client_id": "valid-id-1", "active_seconds": 1}, "started"},
		{"bad started", map[string]any{"client_id": "valid-id-1", "started": "yesterday", "active_seconds": 1}, "started"},
		{"future", map[string]any{"client_id": "valid-id-1", "started": e.clock.Add(6 * time.Minute).Format(time.RFC3339), "active_seconds": 1}, "future"},
		{"too old", map[string]any{"client_id": "valid-id-1", "started": e.clock.Add(-25 * time.Hour).Format(time.RFC3339), "active_seconds": 1}, "24 hours"},
		{"no seconds", map[string]any{"client_id": "valid-id-1", "started": now}, "active_seconds"},
		{"negative", map[string]any{"client_id": "valid-id-1", "started": now, "active_seconds": -1}, "active_seconds"},
		{"null seconds", map[string]any{"client_id": "valid-id-1", "started": now, "active_seconds": nil}, "active_seconds"},
		{"string seconds", map[string]any{"client_id": "valid-id-1", "started": now, "active_seconds": "12"}, "invalid JSON"},
		{"bad video", map[string]any{"client_id": "valid-id-1", "started": now, "active_seconds": 1, "video_id": "not-a-video-id"}, "video_id"},
	} {
		rec := a.do("POST", "/v1/watch/sessions", c.body)
		eq(t, c.name, rec.Code, 400)
		if m := errMsg(rec); !strings.Contains(m, c.msg) {
			t.Fatalf("%s: message %q", c.name, m)
		}
	}
	eq(t, "nothing stored", e.count(`SELECT COUNT(*) FROM watch_sessions`), 0)
	eq(t, "nothing added", a.catHours("active", thisWeek), "")
}

func TestWatchTimeSubcatChoice(t *testing.T) {
	e := newEnv(t)

	// An existing active "primed listening" (any case) is used as it is; a same-named
	// sub-category of another category doesn't count.
	a := e.alice
	a.subcat("phonetic", "Primed Listening")
	a.subcat("active", "Anime")
	a.subcat("active", "primed listening")
	out := a.watch(201, "choice-alice", e.clock.Add(-time.Hour), 600, "")
	eq(t, "existing", added(out)["subcat"], any("primed listening"))
	eq(t, "no note", added(out)["note"], any(nil))
	eq(t, "track", a.catHours("active", thisWeek), "primed listening:0.17")
	eq(t, "nothing created", a.catSubcats("active"), "Anime,primed listening")

	// An archived one is restored rather than duplicated.
	b := e.bob
	id := b.subcat("active", "Primed Listening")
	b.mustStatus(200, "POST", fmt.Sprint("/v1/track/subcats/", id), map[string]any{"archived": true})
	out = b.watch(201, "choice-bob", e.clock.Add(-time.Hour), 600, "")
	eq(t, "restored", added(out)["subcat"], any("Primed Listening"))
	eq(t, "restored note", added(out)["note"], any("Restored your archived “Primed Listening” sub-category under Active immersion for it."))
	eq(t, "unarchived", b.catSubcats("active"), "Primed Listening")

	// A full register of active sub-category names: no room to create one, so the unassigned bucket.
	c := e.admin
	fillSubcats(t, e, c.id, "active", maxAllSubcats)
	ta := added(c.watch(201, "choice-admin", e.clock.Add(-time.Hour), 600, ""))
	eq(t, "unassigned", ta["subcat"], any(nil))
	eq(t, "unassigned hours", ta["hours"], any(0.17))
	if n, _ := ta["note"].(string); !strings.Contains(n, "Your Active immersion sub-categories are at their limit (") ||
		!strings.Contains(n, "Other / unassigned") {
		t.Fatalf("note %q", n)
	}
	eq(t, "unassigned track", c.catHours("active", thisWeek), "-:0.17")
}

func TestWatchTimeWeekBoundary(t *testing.T) {
	e := newEnv(t)
	a := e.alice
	// The week of 2026-10-10 ends Sun 2026-10-18 03:00 UTC. Each heartbeat adds to the week
	// it arrives in.
	start := time.Date(2026, 10, 18, 2, 58, 0, 0, time.UTC)
	e.clock = start.Add(time.Minute)
	eq(t, "old week", added(a.watch(201, "rollover-1", start, 60, ""))["week"], any("2026-10-10"))
	e.clock = start.Add(3 * time.Minute)
	ta := added(a.watch(200, "rollover-1", start, 180, ""))
	eq(t, "new week", ta["week"], any("2026-10-17"))
	eq(t, "session total", ta["hours"], any(0.05))
	eq(t, "old week track", a.catHours("active", "2026-10-10"), "Primed Listening:0.02")
	eq(t, "new week track", a.catHours("active", "2026-10-17"), "Primed Listening:0.03")
}

func TestWatchTimeCaps(t *testing.T) {
	e := newEnv(t)
	a := e.alice
	a.saveWeek(thisWeek, map[string]any{"hours": []any{hrs("active", nil, 167.9)}})
	start := e.clock
	e.clock = start.Add(30 * time.Minute)
	ta := added(a.watch(201, "caps-alice", start, 1800, "")) // 0.5 h, only 0.1 h of room
	eq(t, "capped", ta["hours"], any(0.1))
	eq(t, "note", ta["note"], any("Created a “Primed Listening” sub-category under Active immersion for it. Only 0.1 h of 0.5 h was added: Active immersion can't go over 168 h in a week."))
	eq(t, "track", a.catHours("active", thisWeek), "Primed Listening:0.1 -:167.9")

	// Full: the heartbeat still saves, nothing is added and that's said.
	e.clock = e.clock.Add(time.Minute)
	ta = added(a.watch(200, "caps-alice", start, 1860, ""))
	eq(t, "full", ta["hours"], any(0.1))
	eq(t, "full note", ta["note"], any("Nothing was added: Active immersion already has 168 h this week."))
	eq(t, "unchanged", a.catHours("active", thisWeek), "Primed Listening:0.1 -:167.9")

	// What the cap held back isn't carried into the next week: only new time counts there.
	e.clock = time.Date(2026, 10, 18, 3, 0, 0, 0, time.UTC)
	start2 := e.clock.Add(-2 * time.Hour)
	a.watch(201, "caps-next", start2, 0, "")
	e.clock = e.clock.Add(time.Minute)
	ta = added(a.watch(200, "caps-next", start2, 60, ""))
	eq(t, "next week", ta["week"], any("2026-10-17"))
	eq(t, "next week hours", ta["hours"], any(0.02))
	eq(t, "next week track", a.catHours("active", "2026-10-17"), "Primed Listening:0.02")
}

func TestWatchTimeExport(t *testing.T) {
	e := newEnv(t)
	a := e.alice
	start := e.clock
	e.clock = start.Add(10 * time.Minute)
	a.watch(201, "export-run-1", start, 600, "dQw4w9WgXcQ")
	a.watch(201, "export-run-2", e.clock, 0, "")
	e.bob.watch(201, "export-run-1", start, 600, "")
	x := a.mustStatus(200, "GET", "/v1/me/export", nil)["watch_sessions"].([]any)
	eq(t, "sessions", len(x), 2)
	s := x[0].(map[string]any)
	eq(t, "session", fmt.Sprint(s["started"], s["updated"], s["video_id"], s["active_seconds"]),
		fmt.Sprint(start.Format(time.RFC3339), e.clock.Format(time.RFC3339), "dQw4w9WgXcQ", 600))
	eq(t, "added", sp(added(s)["week"], added(s)["hours"], added(s)["subcat"]), thisWeek+" 0.17 Primed Listening")
	s = x[1].(map[string]any)
	eq(t, "empty session", fmt.Sprint(s["video_id"], s["active_seconds"], s["track_added"]), "<nil> 0 <nil>")
}
