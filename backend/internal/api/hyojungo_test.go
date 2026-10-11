package api

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func tokyoAnswers(n, correct int) []map[string]any {
	out := make([]map[string]any, n)
	for i := range out {
		out[i] = map[string]any{"reading": "トッカ", "surface": "Atamadaka [1]", "pattern": "HLL", "cat": "TA", "speaker": "nhk", "ok": i < correct}
	}
	return out
}

func TestHyojungoAccessAndFiles(t *testing.T) {
	e := newEnv(t)
	files := func(c *client, want int) {
		t.Helper()
		c.mustStatus(want, "GET", "/v1/hyojungo/pairs", nil)
		eq(t, "audio", c.do("GET", "/v1/hyojungo/audio/0-1.aac", nil).Code, want)
	}

	// Not set up on the server: 404 for those with access.
	files(e.admin, 404)

	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "audio"), 0o755)
	os.WriteFile(filepath.Join(dir, "index.json"), []byte(`{"pairs":[]}`), 0o644)
	os.WriteFile(filepath.Join(dir, "audio", "0-1.aac"), []byte("\xff\xf9clip"), 0o644)
	e.s.cfg.HyojungoDir = dir

	files(e.anon(), 401)
	files(e.alice, 403)
	files(e.admin, 200)
	r := e.admin.do("GET", "/v1/hyojungo/audio/0-1.aac", nil)
	eq(t, "type", r.Header().Get("Content-Type"), "audio/aac")
	eq(t, "cache", r.Header().Get("Cache-Control"), "private, max-age=2592000")
	eq(t, "body", r.Body.String(), "\xff\xf9clip")
	eq(t, "range", e.admin.do("GET", "/v1/hyojungo/audio/0-1.aac", nil, "Range", "bytes=2-").Body.String(), "clip")
	for _, bad := range []string{"..%2Findex.json", "0-1.mp3", "x", "0-10.aac"} {
		eq(t, "bad name "+bad, e.admin.do("GET", "/v1/hyojungo/audio/"+bad, nil).Code, 404)
	}
	eq(t, "missing clip", e.admin.do("GET", "/v1/hyojungo/audio/9-0.aac", nil).Code, 404)

	// /v1/me says who has it.
	me := func(c *client) any {
		return c.mustStatus(200, "GET", "/v1/me", nil)["user"].(map[string]any)["hyojungo"]
	}
	eq(t, "admin me", me(e.admin), any(true))
	eq(t, "alice me", me(e.alice), any(false))

	// Only admins give it; never to (or away from) an admin.
	path := fmt.Sprint("/v1/admin/users/", e.alice.id, "/hyojungo")
	e.bob.mustStatus(403, "POST", path, map[string]any{"on": true})
	e.admin.mustStatus(400, "POST", path, map[string]any{})
	out := e.admin.mustStatus(200, "POST", path, map[string]any{"on": true})
	eq(t, "row", out["user"].(map[string]any)["hyojungo"], any(true))
	eq(t, "alice me after", me(e.alice), any(true))
	files(e.alice, 200)
	e.admin.mustStatus(409, "POST", fmt.Sprint("/v1/admin/users/", e.admin.id, "/hyojungo"), map[string]any{"on": false})
	e.admin.mustStatus(404, "POST", "/v1/admin/users/99999/hyojungo", map[string]any{"on": true})
	e.admin.mustStatus(200, "POST", path, map[string]any{"on": false})
	files(e.alice, 403)
}

func TestHyojungoSessions(t *testing.T) {
	e := newEnv(t)
	started := e.clock.Add(-20 * time.Minute).Format(time.RFC3339)
	body := func(mode string, ans []map[string]any, active any) map[string]any {
		return map[string]any{"started": started, "layout": "kotu", "mode": mode, "answers": ans, "active_seconds": active}
	}

	// Access and per-mode categories.
	e.alice.mustStatus(403, "POST", "/v1/kyotsu/sessions", body("tokyo", tokyoAnswers(4, 3), 600))
	e.admin.mustStatus(400, "POST", "/v1/kyotsu/sessions", body("tokyo", answers(4, 3), 600))       // Keihan cats
	e.admin.mustStatus(400, "POST", "/v1/kyotsu/sessions", body("keihan", tokyoAnswers(4, 3), 600)) // Tokyo cats
	e.admin.mustStatus(400, "POST", "/v1/kyotsu/sessions", body("osaka", answers(4, 3), 600))       // no such mode

	// A Tokyo session: its time goes to a new "Kotu" sub-category, its score to Kotu's auto %.
	out := e.admin.mustStatus(201, "POST", "/v1/kyotsu/sessions", body("tokyo", tokyoAnswers(4, 3), 600))
	eq(t, "subcat", added(out)["subcat"], any("Kotu"))
	eq(t, "note", added(out)["note"], any("Created a “Kotu” sub-category under Phonetic training for it."))
	// A Keihan one (mode left out, like older trainers) still goes to Kyōtsu.
	out2 := e.admin.mustStatus(201, "POST", "/v1/kyotsu/sessions", map[string]any{"started": started, "layout": "kotu", "answers": answers(10, 9), "active_seconds": 300})
	eq(t, "keihan subcat", added(out2)["subcat"], any("Kyōtsu"))
	eq(t, "track", e.admin.phonetic(thisWeek), "Kotu:0.17 Kyōtsu:0.08")

	s := e.admin.mustStatus(200, "GET", fmt.Sprint("/v1/kyotsu/sessions/", int64(out["id"].(float64))), nil)
	eq(t, "mode", s["mode"], any("tokyo"))
	list := e.admin.mustStatus(200, "GET", "/v1/kyotsu/sessions", nil)["sessions"].([]any)
	eq(t, "list modes", fmt.Sprint(list[0].(map[string]any)["mode"], " ", list[1].(map[string]any)["mode"]), "tokyo keihan")

	wk := e.admin.mustStatus(200, "GET", "/v1/track/weeks/"+thisWeek, nil)
	eq(t, "kotu_auto", fmt.Sprint(wk["kotu_auto"]), "[75 <nil> <nil> <nil> <nil> <nil> <nil>]")
	eq(t, "kotu_auto_answers", fmt.Sprint(wk["kotu_auto_answers"]), "[4 0 0 0 0 0 0]")
	eq(t, "kyotsu_auto", fmt.Sprint(wk["kyotsu_auto"]), "[90 <nil> <nil> <nil> <nil> <nil> <nil>]")
	eq(t, "manual kotu untouched", fmt.Sprint(wk["kotu"]), "[<nil> <nil> <nil> <nil> <nil> <nil> <nil>]")

	// Lists and profiles use manual values, falling back to the trainer's (day 0: auto 75, day 1: manual 50).
	e.admin.saveWeek(thisWeek, map[string]any{"kotu": nums(nil, 50)})
	ws := e.admin.mustStatus(200, "GET", "/v1/track/weeks", nil)["weeks"].([]any)
	w := ws[len(ws)-1].(map[string]any)
	eq(t, "weekly kotu avg", w["kotu"], any(62.5))

	// A retry with the same client id in the other mode is refused.
	cb := body("tokyo", tokyoAnswers(2, 1), 60)
	cb["client_id"] = "run-abcdefgh"
	e.admin.mustStatus(201, "POST", "/v1/kyotsu/sessions", cb)
	kb := map[string]any{"started": started, "layout": "kotu", "answers": answers(2, 1), "client_id": "run-abcdefgh"}
	e.admin.mustStatus(409, "POST", "/v1/kyotsu/sessions", kb)
}
