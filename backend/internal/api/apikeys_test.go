package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// newKey creates an API key for c through the site's endpoint and returns it.
func (c *client) newKey(name, scope string) string {
	c.e.t.Helper()
	out := c.mustStatus(201, "POST", "/v1/me/api-keys", map[string]any{"name": name, "scope": scope})
	return out["key"].(string)
}

// keyReq is a script's request: no cookie, no Origin, the key (if any) as a bearer token, and a
// JSON body with no Content-Type unless one is given in hdr.
func (e *env) keyReq(key, method, path string, body any, hdr ...string) *httptest.ResponseRecorder {
	e.t.Helper()
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		rd = strings.NewReader(b)
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			e.t.Fatal(err)
		}
		rd = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, rd)
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	req.RemoteAddr = "198.51.100.7:40000"
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

// keyJSON does a key request, checks the status and decodes the JSON answer.
func (e *env) keyJSON(want int, key, method, path string, body any) map[string]any {
	e.t.Helper()
	rec := e.keyReq(key, method, path, body)
	if rec.Code != want {
		e.t.Fatalf("%s %s: status %d, want %d; body %s", method, path, rec.Code, want, rec.Body)
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		e.t.Fatalf("%s %s: bad JSON %q", method, path, rec.Body)
	}
	return out
}

func errMsg(rec *httptest.ResponseRecorder) string {
	var out map[string]string
	json.Unmarshal(rec.Body.Bytes(), &out)
	return out["error"]
}

func TestAPIKeyManagement(t *testing.T) {
	e := newEnv(t)
	list := e.alice.mustStatus(200, "GET", "/v1/me/api-keys", nil)
	eq(t, "no keys", len(list["keys"].([]any)), 0)
	eq(t, "max", list["max"], any(float64(5)))

	out := e.alice.mustStatus(201, "POST", "/v1/me/api-keys", map[string]any{"name": " My script ", "scope": "read"})
	key := out["key"].(string)
	k := out["api_key"].(map[string]any)
	if !strings.HasPrefix(key, "fz_") || len(key) != 3+43 {
		t.Fatalf("key %q", key)
	}
	eq(t, "name", k["name"], any("My script"))
	eq(t, "scope", k["scope"], any("read"))
	eq(t, "prefix", k["prefix"], any(key[:7]))
	eq(t, "created", k["created"], any("2026-10-11T12:00:00Z"))
	eq(t, "last_used", k["last_used"], nil)
	if _, has := k["key"]; has {
		t.Fatal("key metadata carries the key")
	}

	// Only the SHA-256 is stored; the key appears nowhere in the database.
	var hash []byte
	var prefix string
	e.db.QueryRow(`SELECT hash, prefix FROM api_keys WHERE id = ?`, int64(k["id"].(float64))).Scan(&hash, &prefix)
	sum := sha256.Sum256([]byte(key))
	eq(t, "stored hash", string(hash), string(sum[:]))
	eq(t, "stored prefix", prefix, key[:7])
	var dump string
	e.db.QueryRow(`SELECT group_concat(id || name || prefix || hex(hash) || scope || created_at, '|') FROM api_keys`).Scan(&dump)
	if strings.Contains(dump, key[3:]) || strings.Contains(strings.ToLower(dump), strings.ToLower(key[3:])) {
		t.Fatal("key stored in plain text")
	}

	list = e.alice.mustStatus(200, "GET", "/v1/me/api-keys", nil)
	keys := list["keys"].([]any)
	eq(t, "one key", len(keys), 1)
	if strings.Contains(e.alice.last.Body.String(), key) {
		t.Fatal("list shows the key")
	}

	create := func(c *client, body map[string]any) int { return c.do("POST", "/v1/me/api-keys", body).Code }
	eq(t, "bad scope", create(e.alice, map[string]any{"name": "x", "scope": "admin"}), 400)
	eq(t, "no scope", create(e.alice, map[string]any{"name": "x"}), 400)
	eq(t, "empty name", create(e.alice, map[string]any{"name": " ", "scope": "read"}), 400)
	eq(t, "long name", create(e.alice, map[string]any{"name": strings.Repeat("a", 41), "scope": "read"}), 400)
	eq(t, "logged out", create(e.anon(), map[string]any{"name": "x", "scope": "read"}), 401)
	e.alice.origin = ""
	eq(t, "no origin (CSRF)", create(e.alice, map[string]any{"name": "x", "scope": "read"}), 403)
	e.alice.origin = "https://evil.example"
	eq(t, "foreign origin", e.alice.do("DELETE", "/v1/me/api-keys/1", nil).Code, 403)
	e.alice.origin = origin
	// A key can't manage keys: those routes only take the session cookie.
	eq(t, "list with key", e.keyReq(key, "GET", "/v1/me/api-keys", nil).Code, 401)

	// At most 5.
	for i := 2; i <= 5; i++ {
		e.alice.newKey(fmt.Sprint("k", i), "write")
	}
	eq(t, "sixth", create(e.alice, map[string]any{"name": "k6", "scope": "read"}), 409)
	eq(t, "bob unaffected", create(e.bob, map[string]any{"name": "b", "scope": "read"}), 201)

	// Revoke: own keys only; then there's room again.
	id := int64(k["id"].(float64))
	eq(t, "revoke other's", e.bob.do("DELETE", fmt.Sprint("/v1/me/api-keys/", id), nil).Code, 404)
	eq(t, "still works", e.keyReq(key, "GET", "/v1/api/track/subcats", nil).Code, 200)
	eq(t, "revoke", e.alice.do("DELETE", fmt.Sprint("/v1/me/api-keys/", id), nil).Code, 200)
	eq(t, "revoked key", e.keyReq(key, "GET", "/v1/api/track/subcats", nil).Code, 401)
	eq(t, "revoke again", e.alice.do("DELETE", fmt.Sprint("/v1/me/api-keys/", id), nil).Code, 404)
	eq(t, "room again", create(e.alice, map[string]any{"name": "k6", "scope": "read"}), 201)
	eq(t, "bad id", e.alice.do("DELETE", "/v1/me/api-keys/x", nil).Code, 404)
}

func TestAPIKeyAuth(t *testing.T) {
	e := newEnv(t)
	rw := e.alice.newKey("rw", "write")
	ro := e.alice.newKey("ro", "read")

	// No key / bad keys / the session cookie: 401 with a Bearer challenge.
	rec := e.keyReq("", "GET", "/v1/api/track/weeks", nil)
	eq(t, "no key", rec.Code, 401)
	eq(t, "challenge", rec.Header().Get("WWW-Authenticate"), `Bearer realm="fazon.nexus"`)
	if !strings.Contains(errMsg(rec), "Authorization: Bearer") {
		t.Fatalf("error %q", errMsg(rec))
	}
	eq(t, "wrong key", e.keyReq("fz_"+strings.Repeat("A", 43), "GET", "/v1/api/track/weeks", nil).Code, 401)
	eq(t, "not ours", e.keyReq("abc", "GET", "/v1/api/track/weeks", nil).Code, 401)
	eq(t, "basic scheme", e.keyReq("", "GET", "/v1/api/track/weeks", nil, "Authorization", "Basic "+rw).Code, 401)
	eq(t, "lower-case scheme", e.keyReq("", "GET", "/v1/api/track/weeks", nil, "Authorization", "bearer "+rw).Code, 200)
	eq(t, "cookie on /v1/api", e.alice.do("GET", "/v1/api/track/weeks", nil).Code, 401)
	eq(t, "cookie write on /v1/api", e.alice.do("PUT", "/v1/api/track/weeks/"+thisWeek+"/summary", map[string]any{"summary": "x"}).Code, 401)

	// Keys only work on /v1/api.
	for _, p := range []string{"/v1/track/weeks/" + thisWeek, "/v1/track/config", "/v1/notifications", "/v1/me/api-keys"} {
		eq(t, "key on "+p, e.keyReq(rw, "GET", p, nil).Code, 401)
	}
	eq(t, "key on /v1/me", e.keyJSON(200, rw, "GET", "/v1/me", nil)["user"], nil)
	eq(t, "key write on site route", e.keyReq(rw, "POST", "/v1/track/subcats", map[string]any{"category": "active", "name": "X"},
		"Origin", origin, "Content-Type", "application/json").Code, 401)
	eq(t, "key comment", e.keyReq(rw, "POST", "/v1/comments", map[string]any{"page": page, "body": "hi"},
		"Origin", origin, "Content-Type", "application/json").Code, 401)
	eq(t, "unknown /v1/api route", e.keyReq(rw, "GET", "/v1/api/comments", nil).Code, 404)
	eq(t, "dot-dot doesn't escape the CSRF rules", e.keyReq(rw, "POST", "/v1/api/../comments", map[string]any{"page": page, "body": "hi"}).Code, 403)

	// Writes need no Origin and no Content-Type; read-only keys can't write.
	sum := map[string]any{"summary": "via script"}
	eq(t, "write, no origin", e.keyReq(rw, "PUT", "/v1/api/track/weeks/"+thisWeek+"/summary", sum).Code, 200)
	eq(t, "write, form content type", e.keyReq(rw, "PUT", "/v1/api/track/weeks/"+thisWeek+"/summary", sum,
		"Content-Type", "application/x-www-form-urlencoded").Code, 200)
	eq(t, "write, foreign origin", e.keyReq(rw, "PUT", "/v1/api/track/weeks/"+thisWeek+"/summary", sum, "Origin", "https://evil.example").Code, 200)
	for _, w := range []struct {
		method, path string
		body         any
	}{
		{"PUT", "/v1/api/track/weeks/" + thisWeek + "/summary", sum},
		{"PATCH", "/v1/api/track/weeks/" + thisWeek + "/hours", map[string]any{"entries": []any{map[string]any{"category": "active", "set": 1}}}},
		{"POST", "/v1/api/track/weeks/" + thisWeek + "/goals", map[string]any{"goals": map[string]any{"active": 1}}},
		{"POST", "/v1/api/track/subcats", map[string]any{"category": "active", "name": "X"}},
	} {
		rec := e.keyReq(ro, w.method, w.path, w.body)
		eq(t, "read-only "+w.method+" "+w.path, rec.Code, 403)
		if !strings.Contains(errMsg(rec), "read-only") {
			t.Fatalf("error %q", errMsg(rec))
		}
	}
	eq(t, "read-only reads", e.keyReq(ro, "GET", "/v1/api/track/weeks/"+thisWeek, nil).Code, 200)
	var sub int
	e.db.QueryRow(`SELECT COUNT(*) FROM track_subcats`).Scan(&sub)
	eq(t, "nothing written by read-only key", sub, 0)

	// No CORS for /v1/api, even from the site's own origin; preflights are refused.
	rec = e.keyReq(rw, "GET", "/v1/api/track/weeks", nil, "Origin", origin)
	eq(t, "no ACAO", rec.Header().Get("Access-Control-Allow-Origin"), "")
	eq(t, "no ACAC", rec.Header().Get("Access-Control-Allow-Credentials"), "")
	eq(t, "nosniff still", rec.Header().Get("X-Content-Type-Options"), "nosniff")
	rec = e.keyReq("", "OPTIONS", "/v1/api/track/weeks", nil, "Origin", origin, "Access-Control-Request-Method", "GET",
		"Access-Control-Request-Headers", "authorization")
	eq(t, "preflight refused", rec.Code, 403)
	eq(t, "preflight ACAO", rec.Header().Get("Access-Control-Allow-Origin"), "")
	eq(t, "preflight headers", rec.Header().Get("Access-Control-Allow-Headers"), "")
	// The rest of the API is unchanged.
	rec = e.alice.do("GET", "/v1/track/config", nil)
	eq(t, "site ACAO", rec.Header().Get("Access-Control-Allow-Origin"), origin)

	// last_used_at: set on use, then at most once a minute.
	used := func() int64 {
		var v int64
		e.db.QueryRow(`SELECT IFNULL(last_used_at, 0) FROM api_keys WHERE name = 'ro'`).Scan(&v)
		return v
	}
	t0 := e.clock.Unix()
	eq(t, "used", used(), t0)
	e.clock = e.clock.Add(30 * time.Second)
	e.keyReq(ro, "GET", "/v1/api/track/subcats", nil)
	eq(t, "not again within a minute", used(), t0)
	e.clock = e.clock.Add(31 * time.Second)
	e.keyReq(ro, "GET", "/v1/api/track/subcats", nil)
	eq(t, "again after a minute", used(), e.clock.Unix())
	keys := e.alice.mustStatus(200, "GET", "/v1/me/api-keys", nil)["keys"].([]any)
	eq(t, "listed last_used", keys[1].(map[string]any)["last_used"], any(e.clock.UTC().Format(time.RFC3339)))

	// Deleting the user removes their keys.
	e.db.Exec(`DELETE FROM users WHERE id = ?`, e.alice.id)
	eq(t, "deleted user's key", e.keyReq(rw, "GET", "/v1/api/track/subcats", nil).Code, 401)
}

func TestAPIKeyRateLimit(t *testing.T) {
	e := newEnv(t)
	k1 := e.alice.newKey("one", "write")
	k2 := e.alice.newKey("two", "write")
	for i := 0; i < 120; i++ {
		if c := e.keyReq(k1, "GET", "/v1/api/track/subcats", nil).Code; c != 200 {
			t.Fatalf("read %d: %d", i, c)
		}
		e.clock = e.clock.Add(100 * time.Millisecond)
	}
	rec := e.keyReq(k1, "GET", "/v1/api/track/subcats", nil)
	eq(t, "121st read", rec.Code, 429)
	ra, _ := strconv.Atoi(rec.Header().Get("Retry-After"))
	if ra < 1 || ra > 60 {
		t.Fatalf("Retry-After %q", rec.Header().Get("Retry-After"))
	}
	if !strings.Contains(errMsg(rec), "rate limit") {
		t.Fatalf("error %q", errMsg(rec))
	}
	eq(t, "other key", e.keyReq(k2, "GET", "/v1/api/track/subcats", nil).Code, 200)
	body := map[string]any{"summary": "s"}
	for i := 0; i < 30; i++ {
		if c := e.keyReq(k1, "PUT", "/v1/api/track/weeks/"+thisWeek+"/summary", body).Code; c != 200 {
			t.Fatalf("write %d: %d", i, c)
		}
	}
	eq(t, "31st write", e.keyReq(k1, "PUT", "/v1/api/track/weeks/"+thisWeek+"/summary", body).Code, 429)
	e.clock = e.clock.Add(time.Minute)
	eq(t, "reads after a minute", e.keyReq(k1, "GET", "/v1/api/track/subcats", nil).Code, 200)
	eq(t, "writes after a minute", e.keyReq(k1, "PUT", "/v1/api/track/weeks/"+thisWeek+"/summary", body).Code, 200)
}

// weekOf returns one week object from a list answer.
func weekOf(t *testing.T, out map[string]any, key string) map[string]any {
	t.Helper()
	for _, w := range out["weeks"].([]any) {
		if w := w.(map[string]any); w["week"] == key {
			return w
		}
	}
	t.Fatalf("no week %s in %v", key, out)
	return nil
}

func jsonOf(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestAPIReadWeeks(t *testing.T) {
	e := newEnv(t)
	key := e.alice.newKey("r", "read")
	anime := e.alice.subcat("active", "Anime")
	yt := e.alice.subcat("active", "YouTube")
	deck := e.alice.subcat("anki", "Core")
	e.alice.saveWeek("2026-10-03", map[string]any{
		"hours":   []any{hrs("active", anime, 10.5), hrs("active", yt, 1.25), hrs("active", nil, 2), hrs("passive", nil, 4), hrs("anki", deck, 1)},
		"kotu":    nums(80, nil, 90),
		"kyotsu":  nums(nil, 70),
		"ratings": map[string]any{"active": 4},
		"summary": "secret week", "summary_private": true,
	})
	e.alice.saveWeek(thisWeek, map[string]any{"hours": []any{hrs("output", nil, 0.5)}})
	e.alice.mustStatus(200, "POST", "/v1/track/weeks/"+thisWeek+"/goals", map[string]any{"goals": map[string]any{"active": 35}})
	e.kyotsuAt(e.alice, at("2026-10-06T12:00:00Z"), 10, 9) // Tuesday of 2026-10-03's week
	e.bob.saveWeek("2026-10-03", map[string]any{"hours": []any{hrs("active", nil, 99)}})

	out := e.keyJSON(200, key, "GET", "/v1/api/track/weeks", nil)
	eq(t, "from", out["from"], any("2026-07-25"))
	eq(t, "to", out["to"], any(thisWeek))
	eq(t, "current_week", out["current_week"], any(thisWeek))
	weeks := out["weeks"].([]any)
	eq(t, "12 weeks", len(weeks), 12)
	eq(t, "oldest first", weeks[0].(map[string]any)["week"], any("2026-07-25"))

	w := weekOf(t, out, "2026-10-03")
	eq(t, "logged", w["logged"], any(true))
	eq(t, "current", w["current"], any(false))
	eq(t, "start", w["start"], any("2026-10-04T03:00:00Z"))
	eq(t, "totals", jsonOf(w["totals"]), `{"active":13.75,"all":18.75,"anki":1,"nopassive":14.75,"output":0,"passive":4,"phonetic":0,"soroban":0}`)
	eq(t, "active hours", jsonOf(w["hours"].(map[string]any)["active"]), fmt.Sprintf(
		`{"subcats":[{"archived":false,"hours":10.5,"id":%d,"name":"Anime"},{"archived":false,"hours":1.25,"id":%d,"name":"YouTube"}],"unassigned":2}`, anime, yt))
	eq(t, "passive hours", jsonOf(w["hours"].(map[string]any)["passive"]), `{"subcats":[],"unassigned":4}`)
	eq(t, "every category", len(w["hours"].(map[string]any)), 6)
	eq(t, "ratings", jsonOf(w["ratings"]), `{"active":4}`)
	eq(t, "goals none", jsonOf(w["goals"]), `{}`)
	eq(t, "private summary shown to owner", w["summary"], any("secret week"))
	eq(t, "private", w["private"], any(true))
	eq(t, "kotu", w["kotu"], any(85.0))
	eq(t, "kyotsu (manual + trainer)", w["kyotsu"], any(80.0))
	if _, has := w["days"]; has {
		t.Fatal("days without ?days=1")
	}
	cur := weekOf(t, out, thisWeek)
	eq(t, "current week", cur["current"], any(true))
	eq(t, "goals", jsonOf(cur["goals"]), `{"active":35}`)
	empty := weekOf(t, out, "2026-09-26")
	eq(t, "unlogged", empty["logged"], any(false))
	eq(t, "unlogged updated", empty["updated"], nil)
	eq(t, "unlogged totals", empty["totals"].(map[string]any)["all"], any(0.0))

	// Single week, with days; an unlogged week with trainer sessions still has its Kyōtsu.
	one := e.keyJSON(200, key, "GET", "/v1/api/track/weeks/2026-10-03?days=1", nil)
	eq(t, "same as in the list", jsonOf(one["totals"]), jsonOf(w["totals"]))
	eq(t, "days", jsonOf(one["days"]), `["2026-10-04","2026-10-05","2026-10-06","2026-10-07","2026-10-08","2026-10-09","2026-10-10"]`)
	eq(t, "kotu_days", jsonOf(one["kotu_days"]), `[80,null,90,null,null,null,null]`)
	eq(t, "kyotsu_days", jsonOf(one["kyotsu_days"]), `[null,70,90,null,null,null,null]`)
	eq(t, "kyotsu_source", jsonOf(one["kyotsu_source"]), `[null,"manual","auto",null,null,null,null]`)
	e.kyotsuAt(e.alice, at("2026-09-28T12:00:00Z"), 4, 3)
	un := e.keyJSON(200, key, "GET", "/v1/api/track/weeks?from=2026-09-26&to=2026-09-26&days=1", nil)
	uw := weekOf(t, un, "2026-09-26")
	eq(t, "unlogged auto kyotsu", uw["kyotsu"], any(75.0))
	eq(t, "unlogged source", jsonOf(uw["kyotsu_source"]), `[null,"auto",null,null,null,null,null]`)

	// Ranges.
	bad := func(q string) int { return e.keyReq(key, "GET", "/v1/api/track/weeks"+q, nil).Code }
	eq(t, "from after to", bad("?from=2026-10-10&to=2026-10-03"), 400)
	eq(t, "future to", bad("?to=2026-10-17"), 400)
	eq(t, "not a saturday", bad("?from=2026-10-04"), 400)
	eq(t, "garbage", bad("?from=x"), 400)
	eq(t, "260 weeks", bad("?from="+e.s.now().AddDate(0, 0, -7*259-1).Format("2006-01-02")+"&to="+thisWeek), 200)
	eq(t, "261 weeks", bad("?from="+e.s.now().AddDate(0, 0, -7*260-1).Format("2006-01-02")+"&to="+thisWeek), 400)
	eq(t, "future week", e.keyReq(key, "GET", "/v1/api/track/weeks/2026-10-17", nil).Code, 400)
	eq(t, "bad week", e.keyReq(key, "GET", "/v1/api/track/weeks/2026-10-11", nil).Code, 400)

	// Own data only: Bob's key sees Bob's week.
	bk := e.bob.newKey("b", "write")
	bw := e.keyJSON(200, bk, "GET", "/v1/api/track/weeks/2026-10-03", nil)
	eq(t, "bob's totals", bw["totals"].(map[string]any)["active"], any(99.0))
	eq(t, "bob's summary", bw["summary"], any(""))
	eq(t, "bob's subcats", len(e.keyJSON(200, bk, "GET", "/v1/api/track/subcats", nil)["subcats"].([]any)), 0)
	rec := e.keyReq(bk, "PATCH", "/v1/api/track/weeks/"+thisWeek+"/hours",
		map[string]any{"entries": []any{map[string]any{"category": "active", "subcat_id": anime, "set": 1}}})
	eq(t, "alice's subcat id via bob's key", rec.Code, 400)
	rec = e.keyReq(bk, "PATCH", "/v1/api/track/weeks/"+thisWeek+"/hours",
		map[string]any{"entries": []any{map[string]any{"category": "active", "subcat": "Anime", "set": 1}}})
	eq(t, "alice's subcat name via bob's key", rec.Code, 400)
	eq(t, "alice's week untouched", e.alice.mustStatus(200, "GET", "/v1/track/weeks/2026-10-03", nil)["summary"], any("secret week"))
}

func TestAPISubcats(t *testing.T) {
	e := newEnv(t)
	key := e.alice.newKey("w", "write")
	out := e.keyJSON(201, key, "POST", "/v1/api/track/subcats", map[string]any{"category": "active", "name": " Anime "})
	eq(t, "name", out["name"], any("Anime"))
	eq(t, "category", out["category"], any("active"))
	e.alice.subcat("anki", "Core")
	create := func(cat, name string) int {
		return e.keyReq(key, "POST", "/v1/api/track/subcats", map[string]any{"category": cat, "name": name}).Code
	}
	eq(t, "dup ignoring case", create("active", "ANIME"), 409)
	eq(t, "same name other category", create("passive", "Anime"), 201)
	eq(t, "bad category", create("gaming", "X"), 400)
	eq(t, "empty name", create("active", ""), 400)
	eq(t, "bad JSON", e.keyReq(key, "POST", "/v1/api/track/subcats", "{").Code, 400)
	list := e.keyJSON(200, key, "GET", "/v1/api/track/subcats", nil)["subcats"].([]any)
	eq(t, "count", len(list), 3)
	eq(t, "display order", jsonOf([]any{list[0].(map[string]any)["category"], list[1].(map[string]any)["category"], list[2].(map[string]any)["category"]}),
		`["active","passive","anki"]`)
	eq(t, "archived field", list[0].(map[string]any)["archived"], any(false))
	// Shows up on the site too.
	eq(t, "site config", len(e.alice.mustStatus(200, "GET", "/v1/track/config", nil)["subcats"].([]any)), 3)
}

func TestAPIPatchHours(t *testing.T) {
	e := newEnv(t)
	key := e.alice.newKey("w", "write")
	anime := e.alice.subcat("active", "Anime")
	old := e.alice.subcat("active", "Old show")
	e.alice.mustStatus(200, "POST", fmt.Sprint("/v1/track/subcats/", old), map[string]any{"archived": true})
	path := "/v1/api/track/weeks/" + thisWeek + "/hours"
	patch := func(entries ...map[string]any) *httptest.ResponseRecorder {
		list := make([]any, len(entries))
		for i, en := range entries {
			list[i] = en
		}
		e.clock = e.clock.Add(3 * time.Second) // stay under the per-key write limit
		return e.keyReq(key, "PATCH", path, map[string]any{"entries": list})
	}
	ok := func(entries ...map[string]any) map[string]any {
		t.Helper()
		rec := patch(entries...)
		if rec.Code != 200 {
			t.Fatalf("patch %v: %d %s", entries, rec.Code, rec.Body)
		}
		var out map[string]any
		json.Unmarshal(rec.Body.Bytes(), &out)
		return out
	}
	hours := func(w map[string]any, cat string) string { return jsonOf(w["hours"].(map[string]any)[cat]) }
	en := func(kv ...any) map[string]any {
		m := map[string]any{}
		for i := 0; i+1 < len(kv); i += 2 {
			m[kv[i].(string)] = kv[i+1]
		}
		return m
	}

	// A new week: the row is created, as the site would.
	w := ok(en("category", "active", "subcat", "anime", "set", "1:30"), en("category", "anki", "add", 0.75))
	eq(t, "logged", w["logged"], any(true))
	eq(t, "by name, ignoring case", hours(w, "active"), fmt.Sprintf(`{"subcats":[{"archived":false,"hours":1.5,"id":%d,"name":"Anime"}],"unassigned":0}`, anime))
	eq(t, "unassigned add", hours(w, "anki"), `{"subcats":[],"unassigned":0.75}`)
	eq(t, "summary untouched", w["summary"], any(""))
	site := e.alice.mustStatus(200, "GET", "/v1/track/weeks/"+thisWeek, nil)
	eq(t, "site sees it", jsonOf(site["hours"]), fmt.Sprintf(`[{"category":"active","hours":1.5,"subcat":%d},{"category":"anki","hours":0.75,"subcat":null}]`, anime))

	// Ratings, scores, summary, goals and other rows survive API writes.
	e.alice.saveWeek(thisWeek, map[string]any{
		"hours": []any{hrs("active", anime, 1.5), hrs("anki", nil, 0.75), hrs("passive", nil, 3)},
		"kotu":  nums(50), "ratings": map[string]any{"passive": 2}, "summary": "keep me", "summary_private": true})
	e.alice.mustStatus(200, "POST", "/v1/track/weeks/"+thisWeek+"/goals", map[string]any{"goals": map[string]any{"anki": 1}})
	w = ok(en("category", "active", "subcat_id", anime, "add", "0:30"), en("category", "active", "add", "1.25"), en("category", "active", "add", 0.5))
	eq(t, "add h:mm by id; repeated adds apply in order", hours(w, "active"),
		fmt.Sprintf(`{"subcats":[{"archived":false,"hours":2,"id":%d,"name":"Anime"}],"unassigned":1.75}`, anime))
	eq(t, "passive untouched", hours(w, "passive"), `{"subcats":[],"unassigned":3}`)
	eq(t, "ratings kept", jsonOf(w["ratings"]), `{"passive":2}`)
	eq(t, "goals kept", jsonOf(w["goals"]), `{"anki":1}`)
	eq(t, "summary kept", w["summary"], any("keep me"))
	eq(t, "private kept", w["private"], any(true))
	eq(t, "kotu kept", w["kotu"], any(50.0))

	// Archived sub-categories can be targeted by id (old weeks), not by name.
	eq(t, "archived by id", patch(en("category", "active", "subcat_id", old, "set", 1)).Code, 200)
	rec := patch(en("category", "active", "subcat", "old show", "set", 1))
	eq(t, "archived by name", rec.Code, 400)
	rec = patch(en("category", "active", "subcat", "Drama", "set", 1))
	eq(t, "unknown subcat", rec.Code, 400)
	if m := errMsg(rec); !strings.Contains(m, `"Drama"`) || !strings.Contains(m, "POST /v1/api/track/subcats") {
		t.Fatalf("unknown subcat message %q", m)
	}
	eq(t, "subcat in another category", patch(en("category", "passive", "subcat_id", anime, "set", 1)).Code, 400)

	// Validation; nothing is applied when any entry fails (atomic).
	before := e.alice.mustStatus(200, "GET", "/v1/track/weeks/"+thisWeek, nil)
	for name, entries := range map[string][]map[string]any{
		"set and add":           {en("category", "active", "set", 1, "add", 1)},
		"neither":               {en("category", "active")},
		"null set":              {en("category", "active", "set", nil)},
		"negative set":          {en("category", "active", "set", -1)},
		"bad category":          {en("category", "gaming", "set", 1)},
		"bad value":             {en("category", "active", "set", "1h30")},
		"bad minutes":           {en("category", "active", "set", "1:60")},
		"bool value":            {en("category", "active", "set", true)},
		"over 168 set":          {en("category", "output", "set", 168.01)},
		"over 168 string":       {en("category", "output", "set", "168:01")},
		"name and id":           {en("category", "active", "subcat", "Anime", "subcat_id", anime, "set", 1)},
		"empty name":            {en("category", "active", "subcat", " ", "set", 1)},
		"zero id":               {en("category", "active", "subcat_id", 0, "set", 1)},
		"below zero":            {en("category", "output", "set", 5), en("category", "active", "add", -2)},
		"below zero after adds": {en("category", "output", "add", 1), en("category", "output", "add", -1.01)},
		"row over 168":          {en("category", "passive", "set", 100), en("category", "passive", "add", 68.01)},
		"category over 168":     {en("category", "output", "set", 100), en("category", "output", "subcat_id", nil, "subcat", nil, "add", 0), en("category", "active", "set", 167)},
		"late failure":          {en("category", "output", "set", 7), en("category", "active", "subcat", "Nope", "set", 1)},
	} {
		if rec := patch(entries...); rec.Code != 400 {
			t.Fatalf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
	eq(t, "no entries", e.keyReq(key, "PATCH", path, map[string]any{"entries": []any{}}).Code, 400)
	many := make([]any, 101)
	for i := range many {
		many[i] = en("category", "active", "add", 0)
	}
	eq(t, "too many entries", e.keyReq(key, "PATCH", path, map[string]any{"entries": many}).Code, 400)
	eq(t, "nothing applied", jsonOf(e.alice.mustStatus(200, "GET", "/v1/track/weeks/"+thisWeek, nil)), jsonOf(before))
	rec = patch(en("category", "active", "add", -2))
	if m := errMsg(rec); !strings.Contains(m, "below zero") {
		t.Fatalf("below zero message %q", m)
	}

	// The 168 h cap: exactly 168 in a row and in a category is fine.
	w = ok(en("category", "output", "set", "100:00"), en("category", "output", "subcat_id", nil, "add", "68:00"))
	eq(t, "168 h row", hours(w, "output"), `{"subcats":[],"unassigned":168}`)
	eq(t, "category cap with other rows", patch(en("category", "active", "set", 166)).Code, 400) // 2 h (Anime) + 1 h (archived) + 166
	ok(en("category", "active", "set", 165))
	// Rounded to hundredths; decimal strings; negative h:mm adds.
	w = ok(en("category", "output", "set", 1.234), en("category", "phonetic", "set", "0.5"), en("category", "phonetic", "add", "-0:15"))
	eq(t, "rounded", w["totals"].(map[string]any)["output"], any(1.23))
	eq(t, "negative h:mm", w["totals"].(map[string]any)["phonetic"], any(0.25))

	// Setting 0 removes the row; emptying everything unlogs the week unless something else is in it.
	ok(en("category", "phonetic", "set", 0))
	var rows int
	e.db.QueryRow(`SELECT COUNT(*) FROM track_hours WHERE user_id = ? AND category = 'phonetic'`, e.alice.id).Scan(&rows)
	eq(t, "zero row removed", rows, 0)

	past := "2026-09-26"
	pp := "/v1/api/track/weeks/" + past + "/hours"
	w = e.keyJSON(200, key, "PATCH", pp, map[string]any{"entries": []any{en("category", "soroban", "set", 2)}})
	eq(t, "past week logged", w["logged"], any(true))
	w = e.keyJSON(200, key, "PATCH", pp, map[string]any{"entries": []any{en("category", "soroban", "add", -2)}})
	eq(t, "emptied week unlogged", w["logged"], any(false))
	eq(t, "no row", len(e.alice.mustStatus(200, "GET", "/v1/track/weeks?from="+past+"&to="+past, nil)["weeks"].([]any)), 0)
	w = e.keyJSON(200, key, "PATCH", pp, map[string]any{"entries": []any{en("category", "soroban", "set", 0)}})
	eq(t, "zero on an empty week creates nothing", w["logged"], any(false))
	// A week with a rating survives losing its hours, rating intact.
	e.alice.saveWeek(past, map[string]any{"hours": []any{hrs("soroban", nil, 1)}, "ratings": map[string]any{"soroban": 3}})
	w = e.keyJSON(200, key, "PATCH", pp, map[string]any{"entries": []any{en("category", "soroban", "set", 0)}})
	eq(t, "rated week stays", w["logged"], any(true))
	eq(t, "rating kept", jsonOf(w["ratings"]), `{"soroban":3}`)

	eq(t, "future week", e.keyReq(key, "PATCH", "/v1/api/track/weeks/2026-10-17/hours", map[string]any{"entries": []any{en("category", "active", "set", 1)}}).Code, 400)
	eq(t, "wrong method", e.keyReq(key, "POST", path, map[string]any{"entries": []any{en("category", "active", "set", 1)}}).Code, 404)
}

func TestAPISummary(t *testing.T) {
	e := newEnv(t)
	key := e.alice.newKey("w", "write")
	p := "/v1/api/track/weeks/" + thisWeek + "/summary"
	w := e.keyJSON(200, key, "PUT", p, map[string]any{"summary": "  Watched a lot\r\nof anime  ", "private": true})
	eq(t, "new week", w["logged"], any(true))
	eq(t, "cleaned", w["summary"], any("Watched a lot\nof anime"))
	eq(t, "private", w["private"], any(true))
	w = e.keyJSON(200, key, "PUT", p, map[string]any{"summary": "Edited"})
	eq(t, "private kept when omitted", w["private"], any(true))
	w = e.keyJSON(200, key, "PUT", p, map[string]any{"summary": "Public now", "private": false})
	eq(t, "made public", w["private"], any(false))

	// Hours, ratings and goals untouched.
	e.alice.saveWeek(thisWeek, map[string]any{"hours": []any{hrs("active", nil, 2)}, "ratings": map[string]any{"active": 1}, "summary": "Public now"})
	e.alice.mustStatus(200, "POST", "/v1/track/weeks/"+thisWeek+"/goals", map[string]any{"goals": map[string]any{"active": 10}})
	w = e.keyJSON(200, key, "PUT", p, map[string]any{"summary": ""})
	eq(t, "cleared", w["summary"], any(""))
	eq(t, "hours kept", w["totals"].(map[string]any)["active"], any(2.0))
	eq(t, "rating kept", jsonOf(w["ratings"]), `{"active":1}`)
	eq(t, "goal kept", jsonOf(w["goals"]), `{"active":10}`)
	eq(t, "still logged", w["logged"], any(true))

	// A summary-only week is unlogged again when cleared.
	pp := "/v1/api/track/weeks/2026-09-26/summary"
	eq(t, "past summary", e.keyJSON(200, key, "PUT", pp, map[string]any{"summary": "old"})["logged"], any(true))
	eq(t, "cleared → unlogged", e.keyJSON(200, key, "PUT", pp, map[string]any{"summary": " "})["logged"], any(false))

	eq(t, "missing summary", e.keyReq(key, "PUT", p, map[string]any{"private": true}).Code, 400)
	eq(t, "too long", e.keyReq(key, "PUT", p, map[string]any{"summary": strings.Repeat("あ", 10001)}).Code, 400)
	eq(t, "10,000 ok", e.keyReq(key, "PUT", p, map[string]any{"summary": strings.Repeat("あ", 10000)}).Code, 200)
	eq(t, "body over 64 KB", e.keyReq(key, "PUT", p, map[string]any{"summary": strings.Repeat("a", 70000)}).Code, 413)
	eq(t, "bad JSON", e.keyReq(key, "PUT", p, "nope").Code, 400)
}

func TestAPIGoals(t *testing.T) {
	e := newEnv(t)
	key := e.alice.newKey("w", "write")
	p := "/v1/api/track/weeks/" + thisWeek + "/goals"
	w := e.keyJSON(200, key, "POST", p, map[string]any{"goals": map[string]any{"active": 35, "anki": 3.5}})
	eq(t, "goals", jsonOf(w["goals"]), `{"active":35,"anki":3.5}`)
	eq(t, "unlogged week", w["logged"], any(false))
	eq(t, "same value no-op", e.keyReq(key, "POST", p, map[string]any{"goals": map[string]any{"active": 35}}).Code, 200)
	rec := e.keyReq(key, "POST", p, map[string]any{"goals": map[string]any{"active": 40, "passive": 5}})
	eq(t, "write-once", rec.Code, 409)
	if !strings.Contains(errMsg(rec), "already set") {
		t.Fatalf("error %q", errMsg(rec))
	}
	eq(t, "nothing applied", jsonOf(e.keyJSON(200, key, "GET", "/v1/api/track/weeks/"+thisWeek, nil)["goals"]), `{"active":35,"anki":3.5}`)
	eq(t, "new category", jsonOf(e.keyJSON(200, key, "POST", p, map[string]any{"goals": map[string]any{"passive": 5}})["goals"]),
		`{"active":35,"anki":3.5,"passive":5}`)
	// Shared with the site: the site can't change an API-set goal.
	eq(t, "site write-once", e.alice.do("POST", "/v1/track/weeks/"+thisWeek+"/goals", map[string]any{"goals": map[string]any{"active": 1}}).Code, 409)
	eq(t, "past week", e.keyReq(key, "POST", "/v1/api/track/weeks/2026-10-03/goals", map[string]any{"goals": map[string]any{"active": 1}}).Code, 409)
	eq(t, "future week", e.keyReq(key, "POST", "/v1/api/track/weeks/2026-10-17/goals", map[string]any{"goals": map[string]any{"active": 1}}).Code, 400)
	eq(t, "zero", e.keyReq(key, "POST", p, map[string]any{"goals": map[string]any{"output": 0}}).Code, 400)
	eq(t, "null", e.keyReq(key, "POST", p, map[string]any{"goals": map[string]any{"output": nil}}).Code, 400)
	eq(t, "3 decimals", e.keyReq(key, "POST", p, map[string]any{"goals": map[string]any{"output": 1.005}}).Code, 400)
	eq(t, "unknown category", e.keyReq(key, "POST", p, map[string]any{"goals": map[string]any{"gaming": 1}}).Code, 400)
	eq(t, "empty", e.keyReq(key, "POST", p, map[string]any{"goals": map[string]any{}}).Code, 400)
}

// The whole flow over a real HTTP server, as a script would do it.
func TestAPIOverHTTP(t *testing.T) {
	e := newEnv(t)
	key := e.alice.newKey("script", "write")
	srv := httptest.NewServer(e.h)
	defer srv.Close()
	req, _ := http.NewRequest("PATCH", srv.URL+"/v1/api/track/weeks/"+thisWeek+"/hours",
		strings.NewReader(`{"entries":[{"category":"active","set":"2:15"}]}`))
	req.Header.Set("Authorization", "Bearer "+key)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var w map[string]any
	json.NewDecoder(res.Body).Decode(&w)
	eq(t, "status", res.StatusCode, 200)
	eq(t, "content type", res.Header.Get("Content-Type"), "application/json; charset=utf-8")
	eq(t, "hours", w["totals"].(map[string]any)["active"], any(2.25))
}
