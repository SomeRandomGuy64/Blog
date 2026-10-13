package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"fazon.nexus/backend/internal/week"
)

const prevWeek = "2026-10-03" // the Saturday before thisWeek

type mvjReq struct {
	Method, URI string
	Header      http.Header
	Body        map[string]any
}

// fakeMvJ is a Supabase stand-in: sign-in, weeks and weekly_logs, recording every request.
type fakeMvJ struct {
	*httptest.Server
	mu    sync.Mutex
	reqs  []mvjReq
	weeks map[string]string         // start_date → weeks.id
	logs  map[string]map[string]any // week_id → stored weekly_logs row

	fail401   int // REST calls still to answer 401
	failPost  int // upserts still to answer 500
	passwords int // password sign-ins
	refreshes int // refresh_token sign-ins
}

const mvjUser = "user-1"

func newFakeMvJ(t *testing.T) *fakeMvJ {
	f := &fakeMvJ{weeks: map[string]string{}, logs: map[string]map[string]any{}}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeMvJ) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var body map[string]any
	if raw, _ := io.ReadAll(r.Body); len(raw) > 0 {
		json.Unmarshal(raw, &body)
	}
	f.reqs = append(f.reqs, mvjReq{r.Method, r.URL.RequestURI(), r.Header.Clone(), body})
	reply := func(code int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		json.NewEncoder(w).Encode(v)
	}
	q := r.URL.Query()
	switch {
	case r.URL.Path == "/auth/v1/token":
		if q.Get("grant_type") == "refresh_token" {
			f.refreshes++
		} else {
			f.passwords++
		}
		n := f.passwords + f.refreshes
		reply(200, map[string]any{"access_token": fmt.Sprint("acc-", n), "refresh_token": fmt.Sprint("ref-", n),
			"expires_in": 3600, "user": map[string]any{"id": mvjUser}})
	case f.fail401 > 0:
		f.fail401--
		reply(401, map[string]any{"message": "JWT expired"})
	case r.URL.Path == "/rest/v1/weeks":
		rows := []map[string]any{}
		if id, ok := f.weeks[strings.TrimPrefix(q.Get("start_date"), "eq.")]; ok {
			rows = append(rows, map[string]any{"id": id})
		}
		reply(200, rows)
	case r.URL.Path == "/rest/v1/weekly_logs" && r.Method == "GET":
		rows := []map[string]any{}
		if row, ok := f.logs[strings.TrimPrefix(q.Get("week_id"), "eq.")]; ok {
			rows = append(rows, row)
		}
		reply(200, rows)
	case r.URL.Path == "/rest/v1/weekly_logs" && r.Method == "POST":
		if f.failPost > 0 {
			f.failPost--
			reply(500, map[string]any{"message": "boom"})
			return
		}
		f.logs[body["week_id"].(string)] = body
		w.WriteHeader(201)
	default:
		reply(404, nil)
	}
}

func (f *fakeMvJ) upserts() []mvjReq {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []mvjReq
	for _, r := range f.reqs {
		if r.Method == "POST" && strings.HasPrefix(r.URI, "/rest/v1/weekly_logs") {
			out = append(out, r)
		}
	}
	return out
}

// count is how many recorded requests have a URI starting with prefix.
func (f *fakeMvJ) count(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, r := range f.reqs {
		if strings.HasPrefix(r.URI, prefix) {
			n++
		}
	}
	return n
}

func (f *fakeMvJ) setLog(weekID string, row map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logs[weekID] = row
}

func (e *env) newMvJ(f *fakeMvJ, from string) *mvjSync {
	e.t.Helper()
	k, err := week.Parse(from, e.clock)
	if err != nil {
		e.t.Fatal(err)
	}
	return newMvJSync(e.s, MvJConfig{Supabase: f.URL, AnonKey: "anon", Email: "me@mvj", Password: "pw",
		LocalEmail: "admin@example.com", From: k, HTTP: f.Client()})
}

func (e *env) mustPass(m *mvjSync) {
	e.t.Helper()
	if err := m.pass(context.Background()); err != nil {
		e.t.Fatal(err)
	}
}

// cat digs categories[c] out of an upsert body.
func cat(t *testing.T, body map[string]any, c string) (hours float64, rating string) {
	t.Helper()
	m, ok := body["categories"].(map[string]any)[c].(map[string]any)
	if !ok {
		t.Fatalf("no category %s in %v", c, body)
	}
	return m["hours"].(float64), m["rating"].(string)
}

func TestMvJSyncPush(t *testing.T) {
	e := newEnv(t)
	f := newFakeMvJ(t)
	f.weeks[thisWeek], f.weeks[prevWeek] = "w-this", "w-prev" // prevWeek exists remotely but was never logged here
	yt, anime := e.admin.subcat("active", "YouTube"), e.admin.subcat("active", "Anime")
	e.admin.saveWeek(thisWeek, map[string]any{
		"hours": []any{hrs("active", yt, 1.5), hrs("active", anime, 0.75), hrs("active", nil, 0.25),
			hrs("passive", nil, 3), hrs("anki", nil, 0.5)},
		"ratings": map[string]any{"active": 4, "passive": 2, "anki": 1}, "summary": "good week"})

	m := e.newMvJ(f, prevWeek)
	e.mustPass(m)
	ups := f.upserts()
	eq(t, "upserts", len(ups), 1)
	u := ups[0]
	eq(t, "uri", u.URI, "/rest/v1/weekly_logs?on_conflict=user_id,week_id")
	eq(t, "prefer", u.Header.Get("Prefer"), "resolution=merge-duplicates,return=minimal")
	eq(t, "apikey", u.Header.Get("Apikey"), "anon")
	eq(t, "bearer", u.Header.Get("Authorization"), "Bearer acc-1")
	eq(t, "user_id", u.Body["user_id"], any(mvjUser))
	eq(t, "week_id", u.Body["week_id"], any("w-this"))
	eq(t, "notes", u.Body["notes"], any("good week"))
	eq(t, "total_hours", u.Body["total_hours"], any(6.0))
	for c, want := range map[string]struct {
		h float64
		r string
	}{"active": {2.5, "🔥"}, "passive": {3, "😐"}, "anki": {0.5, "😞"}, "phonetic": {0, "🔥"}, "output": {0, "🔥"}, "soroban": {0, "🔥"}} {
		h, r := cat(t, u.Body, c)
		eq(t, c+" hours", h, want.h)
		eq(t, c+" rating", r, want.r)
	}
	eq(t, "categories", len(u.Body["categories"].(map[string]any)), 6)
	eq(t, "password sign-ins", f.passwords, 1)
	eq(t, "refresh sign-ins", f.refreshes, 0)
	// Every request carried the apikey, REST ones a Bearer too; the unlogged week was never looked up.
	for _, r := range f.reqs {
		eq(t, r.URI+" apikey", r.Header.Get("Apikey"), "anon")
		if strings.HasPrefix(r.URI, "/rest/") {
			eq(t, r.URI+" auth", r.Header.Get("Authorization"), "Bearer acc-1")
		}
	}
	eq(t, "prev week lookups", f.count("/rest/v1/weeks?select=id&start_date=eq."+prevWeek), 0)
	eq(t, "this week lookups", f.count("/rest/v1/weeks?select=id&start_date=eq."+thisWeek), 1)
	eq(t, "password body", f.reqs[0].Body["email"], any("me@mvj"))
	eq(t, "password body pw", f.reqs[0].Body["password"], any("pw"))
}

func TestMvJSyncOnlyOnChange(t *testing.T) {
	e := newEnv(t)
	f := newFakeMvJ(t)
	f.weeks[thisWeek] = "w-this"
	save := func(summary string) {
		e.admin.saveWeek(thisWeek, map[string]any{"hours": []any{hrs("active", nil, 1)}, "ratings": map[string]any{"active": 3}, "summary": summary})
	}
	save("one")
	m := e.newMvJ(f, thisWeek)
	e.mustPass(m)
	e.mustPass(m)
	e.mustPass(m)
	eq(t, "unchanged", len(f.upserts()), 1)
	eq(t, "lookups cached", f.count("/rest/v1/weeks"), 1)
	eq(t, "one sign-in", f.passwords+f.refreshes, 1)

	save("two")
	e.mustPass(m)
	ups := f.upserts()
	eq(t, "after change", len(ups), 2)
	eq(t, "new notes", ups[1].Body["notes"], any("two"))
	e.mustPass(m)
	eq(t, "settled", len(f.upserts()), 2)

	// A restart (fresh state) pushes the logged week again.
	e.mustPass(e.newMvJ(f, thisWeek))
	eq(t, "after restart", len(f.upserts()), 3)
}

func TestMvJSyncKeepsRemote(t *testing.T) {
	e := newEnv(t)
	f := newFakeMvJ(t)
	f.weeks[thisWeek] = "w-this"
	f.setLog("w-this", map[string]any{"notes": "written on MvJ",
		"categories": map[string]any{"active": map[string]any{"hours": 9, "rating": "😐"}, "soroban": map[string]any{"hours": 1, "rating": "👌"}}})
	e.admin.saveWeek(thisWeek, map[string]any{
		"hours":   []any{hrs("active", nil, 1), hrs("passive", nil, 3)},
		"ratings": map[string]any{"passive": 3}, "summary": "secret", "summary_private": true})

	e.mustPass(e.newMvJ(f, thisWeek))
	ups := f.upserts()
	eq(t, "upserts", len(ups), 1)
	b := ups[0].Body
	eq(t, "private summary keeps remote notes", b["notes"], any("written on MvJ"))
	for c, want := range map[string]string{"active": "😐", "soroban": "👌", "passive": "👌", "anki": "🔥"} {
		_, r := cat(t, b, c)
		eq(t, c+" rating", r, want)
	}
	h, _ := cat(t, b, "active")
	eq(t, "hours are ours, not remote", h, 1.0)

	// Private summary with nothing remote: empty notes, never ours.
	f2 := newFakeMvJ(t)
	f2.weeks[thisWeek] = "w-x"
	e.mustPass(e.newMvJ(f2, thisWeek))
	eq(t, "no remote row", f2.upserts()[0].Body["notes"], any(""))
}

func TestMvJSyncEmptiedWeek(t *testing.T) {
	e := newEnv(t)
	f := newFakeMvJ(t)
	f.weeks[thisWeek] = "w-this"
	e.admin.saveWeek(thisWeek, map[string]any{"hours": []any{hrs("active", nil, 2), hrs("output", nil, 1)},
		"ratings": map[string]any{"active": 2}, "summary": "hi"})
	m := e.newMvJ(f, thisWeek)
	e.mustPass(m)
	eq(t, "pushed", len(f.upserts()), 1)

	e.admin.saveWeek(thisWeek, map[string]any{"hours": []any{}, "ratings": map[string]any{}, "summary": ""}) // deletes the week
	e.mustPass(m)
	ups := f.upserts()
	eq(t, "zeros pushed", len(ups), 2)
	b := ups[1].Body
	eq(t, "total", b["total_hours"], any(0.0))
	eq(t, "notes", b["notes"], any(""))
	for _, c := range trackCategories {
		h, r := cat(t, b, c.ID)
		eq(t, c.ID+" hours", h, 0.0)
		if c.ID == "active" {
			eq(t, "active rating kept from remote", r, "😐")
		}
	}
	e.mustPass(m)
	eq(t, "zeros pushed once", len(f.upserts()), 2)
}

func TestMvJSyncWeekNotOnSite(t *testing.T) {
	e := newEnv(t)
	f := newFakeMvJ(t)
	e.admin.saveWeek(thisWeek, map[string]any{"hours": []any{hrs("active", nil, 1)}})
	m := e.newMvJ(f, thisWeek)
	e.mustPass(m)
	e.mustPass(m)
	eq(t, "nothing pushed", len(f.upserts()), 0)
	eq(t, "looked up each pass", f.count("/rest/v1/weeks"), 2)

	f.weeks[thisWeek] = "w-this"
	e.mustPass(m)
	ups := f.upserts()
	eq(t, "pushed once the week exists", len(ups), 1)
	eq(t, "week_id", ups[0].Body["week_id"], any("w-this"))
	e.mustPass(m)
	eq(t, "not again", len(f.upserts()), 1)
}

func TestMvJSyncReauthOn401(t *testing.T) {
	e := newEnv(t)
	f := newFakeMvJ(t)
	f.weeks[thisWeek] = "w-this"
	e.admin.saveWeek(thisWeek, map[string]any{"hours": []any{hrs("active", nil, 1)}})
	m := e.newMvJ(f, thisWeek)
	// A past pass (week unlogged then) signed in; now the token is rejected once.
	m.access, m.refresh, m.userID = "acc-old", "ref-old", mvjUser
	m.expires = e.clock.AddDate(10, 0, 0)
	f.fail401 = 1
	e.mustPass(m)
	eq(t, "upserts", len(f.upserts()), 1)
	eq(t, "refresh sign-ins", f.refreshes, 1)
	eq(t, "password sign-ins", f.passwords, 0)
	last := f.upserts()[0]
	eq(t, "retried with the new token", last.Header.Get("Authorization"), "Bearer acc-1")
	eq(t, "refresh body", f.reqs[1].Body["refresh_token"], any("ref-old"))

	// With the refresh token rejected too, it falls back to the password.
	f2 := newFakeMvJ(t)
	f2.weeks[thisWeek] = "w-this"
	m2 := e.newMvJ(f2, thisWeek)
	e.mustPass(m2)
	eq(t, "first sign-in is the password", f2.passwords, 1)
	f2.mu.Lock()
	f2.fail401 = 1
	f2.mu.Unlock()
	e.admin.saveWeek(thisWeek, map[string]any{"hours": []any{hrs("active", nil, 2)}})
	e.mustPass(m2)
	eq(t, "second push", len(f2.upserts()), 2)
	eq(t, "refreshed", f2.refreshes, 1)
}

func TestMvJSyncUpsertError(t *testing.T) {
	e := newEnv(t)
	f := newFakeMvJ(t)
	f.weeks[thisWeek] = "w-this"
	e.admin.saveWeek(thisWeek, map[string]any{"hours": []any{hrs("active", nil, 1)}})
	f.failPost = 1
	m := e.newMvJ(f, thisWeek)
	err := m.pass(context.Background())
	if err == nil || !strings.Contains(err.Error(), thisWeek) || !strings.Contains(err.Error(), "500") {
		t.Fatalf("pass err = %v, want a 500 error naming the week", err)
	}
	eq(t, "stored", len(f.logs), 0)
	e.mustPass(m)
	eq(t, "retried", len(f.upserts()), 2)
	eq(t, "stored after retry", len(f.logs), 1)
	e.mustPass(m)
	eq(t, "then settled", len(f.upserts()), 2)
}

func TestMvJSyncSignInFails(t *testing.T) {
	e := newEnv(t)
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"invalid_grant"}`, 400)
	}))
	defer bad.Close()
	e.admin.saveWeek(thisWeek, map[string]any{"hours": []any{hrs("active", nil, 1)}})
	m := newMvJSync(e.s, MvJConfig{Supabase: bad.URL + "/", Email: "me@mvj", Password: "x", LocalEmail: "admin@example.com",
		From: week.Of(e.clock), HTTP: bad.Client()})
	if err := m.pass(context.Background()); err == nil || !strings.Contains(err.Error(), "sign-in") {
		t.Fatalf("err = %v, want a sign-in error", err)
	}
	if err := newMvJSync(e.s, MvJConfig{LocalEmail: "nobody@example.com", From: week.Of(e.clock)}).pass(context.Background()); err == nil {
		t.Fatal("unknown local account: want an error")
	}
}
