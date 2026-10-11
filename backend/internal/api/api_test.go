package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"fazon.nexus/backend/internal/db"
	"fazon.nexus/backend/internal/mail"
)

const (
	site   = "https://fazon.nexus"
	origin = "https://fazon.nexus"
	page   = "/articles/hello/"
)

type fakeMailer struct {
	mu   sync.Mutex
	msgs []mail.Message
}

func (f *fakeMailer) Send(_ context.Context, m mail.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.msgs = append(f.msgs, m)
	return nil
}

func (f *fakeMailer) all() []mail.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]mail.Message(nil), f.msgs...)
}

type env struct {
	t     *testing.T
	s     *Server
	db    *sql.DB
	h     http.Handler
	mail  *fakeMailer
	clock time.Time
	ips   int

	admin, alice, bob *client
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := db.EnsureAdmin(ctx, d, "admin@example.com", "Admin"); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"Alice", "Bob"} {
		if _, err := db.AddUser(ctx, d, strings.ToLower(n)+"@example.com", n, false); err != nil {
			t.Fatal(err)
		}
	}
	fm := &fakeMailer{}
	e := &env{t: t, db: d, mail: fm, clock: time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)}
	e.s = New(d, Config{Site: site, Origins: []string{origin, "http://localhost:1313", "http://127.0.0.1:*"},
		CookieSecure: true, Mailer: fm})
	e.s.now = func() time.Time { return e.clock }
	e.h = e.s.Handler()
	e.admin = e.login("admin@example.com")
	e.alice = e.login("alice@example.com")
	e.bob = e.login("bob@example.com")
	return e
}

type client struct {
	e      *env
	cookie string
	origin string
	ip     string
	id     int64
	last   *httptest.ResponseRecorder
}

// anon is a logged-out browser on fazon.nexus with its own IP.
func (e *env) anon() *client {
	e.ips++
	return &client{e: e, origin: origin, ip: fmt.Sprintf("203.0.113.%d", e.ips)}
}

// login signs in through /v1/auth/verify with a token minted directly (no rate limits).
func (e *env) login(email string) *client {
	e.t.Helper()
	u, err := db.UserByEmail(context.Background(), e.db, email)
	if err != nil {
		e.t.Fatal(err)
	}
	tok, err := db.CreateLoginToken(context.Background(), e.db, u.ID, "/", e.clock, LoginTokenTTL)
	if err != nil {
		e.t.Fatal(err)
	}
	c := e.anon()
	c.mustStatus(200, "POST", "/v1/auth/verify", map[string]string{"token": tok})
	c.id = u.ID
	return c
}

func (c *client) do(method, path string, body any, hdr ...string) *httptest.ResponseRecorder {
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		rd = strings.NewReader(b)
	case []byte:
		rd = bytes.NewReader(b)
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			c.e.t.Fatal(err)
		}
		rd = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, rd)
	if rd != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.origin != "" {
		req.Header.Set("Origin", c.origin)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	if c.cookie != "" {
		req.AddCookie(&http.Cookie{Name: cookieName, Value: c.cookie})
	}
	req.RemoteAddr = c.ip + ":40000"
	rec := httptest.NewRecorder()
	c.e.h.ServeHTTP(rec, req)
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == cookieName {
			c.cookie = ck.Value
			if ck.MaxAge < 0 {
				c.cookie = ""
			}
		}
	}
	c.last = rec
	return rec
}

// mustStatus does the request, checks the status and decodes the JSON response into a map.
func (c *client) mustStatus(want int, method, path string, body any) map[string]any {
	c.e.t.Helper()
	rec := c.do(method, path, body)
	if rec.Code != want {
		c.e.t.Fatalf("%s %s: status %d, want %d; body %s", method, path, rec.Code, want, rec.Body)
	}
	var out map[string]any
	if strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			c.e.t.Fatalf("%s %s: bad JSON %q: %v", method, path, rec.Body, err)
		}
	}
	return out
}

func (c *client) post(body map[string]any) int64 {
	c.e.t.Helper()
	if body["page"] == nil {
		body["page"] = page
	}
	out := c.mustStatus(201, "POST", "/v1/comments", body)
	return int64(out["id"].(float64))
}

func (c *client) comments(p string) []map[string]any {
	c.e.t.Helper()
	out := c.mustStatus(200, "GET", "/v1/comments?page="+url.QueryEscape(p), nil)
	var list []map[string]any
	for _, x := range out["comments"].([]any) {
		list = append(list, x.(map[string]any))
	}
	return list
}

func (c *client) notifications() (list []map[string]any, unread int) {
	c.e.t.Helper()
	out := c.mustStatus(200, "GET", "/v1/notifications", nil)
	for _, x := range out["notifications"].([]any) {
		list = append(list, x.(map[string]any))
	}
	return list, int(out["unread"].(float64))
}

// kinds summarises notifications as "kind:actorName:commentID", newest first.
func (c *client) kinds() []string {
	c.e.t.Helper()
	list, _ := c.notifications()
	out := []string{}
	for _, n := range list {
		out = append(out, fmt.Sprintf("%s:%s:%d", n["kind"], n["actor"].(map[string]any)["name"], int64(n["comment_id"].(float64))))
	}
	return out
}

func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	os.Exit(m.Run())
}

func eq[T comparable](t *testing.T, what string, got, want T) {
	t.Helper()
	if got != want {
		t.Fatalf("%s = %v, want %v", what, got, want)
	}
}

func eqStrings(t *testing.T, what string, got, want []string) {
	t.Helper()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("%s = %q, want %q", what, got, want)
	}
}

func tokenFrom(t *testing.T, m mail.Message) (tok, next string) {
	t.Helper()
	i := strings.Index(m.Text, site+"/account/verify/#t=")
	if i < 0 {
		t.Fatalf("no magic link in %q", m.Text)
	}
	link := strings.Fields(m.Text[i:])[0]
	frag, _ := url.ParseQuery(link[strings.Index(link, "#")+1:])
	return frag.Get("t"), frag.Get("next")
}

// ---- auth ----

func TestAuthRequestNoEnumeration(t *testing.T) {
	e := newEnv(t)
	c := e.anon()
	known := c.do("POST", "/v1/auth/request", map[string]string{"email": "  ALICE@Example.com ", "next": "/articles/hello/"})
	unknown := c.do("POST", "/v1/auth/request", map[string]string{"email": "nobody@example.com", "next": "/"})
	e.s.Wait()
	eq(t, "known status", known.Code, 202)
	eq(t, "unknown status", unknown.Code, 202)
	eq(t, "same body", known.Body.String(), unknown.Body.String())
	msgs := e.mail.all()
	eq(t, "emails sent", len(msgs), 1)
	eq(t, "to", msgs[0].To, "alice@example.com")
	if !strings.Contains(msgs[0].Text, site+"/account/verify/#t=") || !strings.Contains(msgs[0].Text, "&next=%2Farticles%2Fhello%2F") {
		t.Fatalf("bad link in %q", msgs[0].Text)
	}
	if !strings.Contains(msgs[0].HTML, `href="https://fazon.nexus/account/verify/#t=`) {
		t.Fatalf("bad html %q", msgs[0].HTML)
	}
	tok, next := tokenFrom(t, msgs[0])
	eq(t, "next", next, "/articles/hello/")
	out := c.mustStatus(200, "POST", "/v1/auth/verify", map[string]string{"token": tok})
	eq(t, "verify next", out["next"], any("/articles/hello/"))

	eq(t, "missing email", c.do("POST", "/v1/auth/request", map[string]string{"email": ""}).Code, 400)
	eq(t, "bad email", c.do("POST", "/v1/auth/request", map[string]string{"email": "not an email"}).Code, 400)
	eq(t, "bad json", c.do("POST", "/v1/auth/request", "{").Code, 400)
}

func TestSafeNext(t *testing.T) {
	for in, want := range map[string]string{
		"/articles/x/": "/articles/x/", "/kyotsu/?a=1#b": "/kyotsu/?a=1#b", "": "/", "articles": "/",
		"//evil.com": "/", `/\evil.com`: "/", "https://evil.com": "/", "/a\nb": "/",
	} {
		eq(t, "SafeNext("+in+")", SafeNext(in), want)
	}
	e := newEnv(t)
	c := e.anon()
	c.do("POST", "/v1/auth/request", map[string]string{"email": "bob@example.com", "next": "//evil.com/"})
	e.s.Wait()
	_, next := tokenFrom(t, e.mail.all()[0])
	eq(t, "next", next, "/")
}

func TestAuthRequestRateLimit(t *testing.T) {
	e := newEnv(t)
	sent := func() int { e.s.Wait(); return len(e.mail.all()) }

	// Per email: 3 per 15 minutes, from any IP; over the limit is still 202.
	for i := 0; i < 5; i++ {
		eq(t, "status", e.anon().do("POST", "/v1/auth/request", map[string]string{"email": "alice@example.com"}).Code, 202)
	}
	eq(t, "per-email sends", sent(), 3)
	e.clock = e.clock.Add(16 * time.Minute)
	e.anon().do("POST", "/v1/auth/request", map[string]string{"email": "alice@example.com"})
	eq(t, "after window", sent(), 4)

	// Per IP: 10 per hour, counting unknown emails too.
	c := e.anon()
	for i := 0; i < 10; i++ {
		c.do("POST", "/v1/auth/request", map[string]string{"email": fmt.Sprintf("x%d@example.com", i)})
	}
	eq(t, "status", c.do("POST", "/v1/auth/request", map[string]string{"email": "bob@example.com"}).Code, 202)
	eq(t, "per-IP limit", sent(), 4)

	// Behind Caddy (loopback peer) the first X-Forwarded-For hop is the client...
	proxy := &client{e: e, origin: origin, ip: "127.0.0.1"}
	for i := 0; i < 10; i++ {
		proxy.do("POST", "/v1/auth/request", map[string]string{"email": fmt.Sprintf("y%d@example.com", i)},
			"X-Forwarded-For", "198.51.100.1, 10.0.0.1")
	}
	proxy.do("POST", "/v1/auth/request", map[string]string{"email": "bob@example.com"}, "X-Forwarded-For", "198.51.100.1")
	eq(t, "XFF client limited", sent(), 4)
	proxy.do("POST", "/v1/auth/request", map[string]string{"email": "bob@example.com"}, "X-Forwarded-For", "198.51.100.2")
	eq(t, "other XFF client", sent(), 5)
	// ...but a direct peer can't dodge its limit by sending one.
	c.do("POST", "/v1/auth/request", map[string]string{"email": "admin@example.com"}, "X-Forwarded-For", "192.0.2.99")
	eq(t, "spoofed XFF ignored", sent(), 5)
}

func TestVerify(t *testing.T) {
	e := newEnv(t)
	c := e.anon()
	c.do("POST", "/v1/auth/request", map[string]string{"email": "bob@example.com", "next": "/kyotsu/"})
	e.s.Wait()
	tok, _ := tokenFrom(t, e.mail.all()[0])

	rec := c.do("POST", "/v1/auth/verify", map[string]string{"token": tok})
	eq(t, "status", rec.Code, 200)
	var out struct {
		User struct {
			ID    int64
			Name  string
			Email string
			Admin bool
		}
		Next string
	}
	json.Unmarshal(rec.Body.Bytes(), &out)
	eq(t, "name", out.User.Name, "Bob")
	eq(t, "email", out.User.Email, "bob@example.com")
	eq(t, "admin", out.User.Admin, false)
	eq(t, "next", out.Next, "/kyotsu/")
	ck := rec.Result().Cookies()[0]
	eq(t, "cookie name", ck.Name, cookieName)
	eq(t, "HttpOnly", ck.HttpOnly, true)
	eq(t, "Secure", ck.Secure, true)
	eq(t, "SameSite", ck.SameSite, http.SameSiteLaxMode)
	eq(t, "Path", ck.Path, "/")
	eq(t, "MaxAge", ck.MaxAge, 90*24*3600)
	if len(ck.Value) != 43 {
		t.Fatalf("session token %q not 32 bytes base64url", ck.Value)
	}

	// Single use.
	rec = e.anon().do("POST", "/v1/auth/verify", map[string]string{"token": tok})
	eq(t, "reuse", rec.Code, 400)
	eq(t, "error body", strings.TrimSpace(rec.Body.String()), `{"error":"invalid or expired link"}`)

	// Expiry.
	c.do("POST", "/v1/auth/request", map[string]string{"email": "bob@example.com"})
	e.s.Wait()
	tok2, _ := tokenFrom(t, e.mail.all()[1])
	e.clock = e.clock.Add(LoginTokenTTL + time.Second)
	eq(t, "expired", e.anon().do("POST", "/v1/auth/verify", map[string]string{"token": tok2}).Code, 400)

	eq(t, "garbage", e.anon().do("POST", "/v1/auth/verify", map[string]string{"token": "nope"}).Code, 400)
	eq(t, "empty", e.anon().do("POST", "/v1/auth/verify", map[string]string{}).Code, 400)
	// Tokens are stored hashed: the raw token is nowhere in the table.
	var n int
	e.db.QueryRow(`SELECT COUNT(*) FROM login_tokens WHERE hash = ?`, tok).Scan(&n)
	eq(t, "raw token stored", n, 0)
}

func TestSessionMeLogout(t *testing.T) {
	e := newEnv(t)
	eq(t, "anon me", strings.TrimSpace(e.anon().do("GET", "/v1/me", nil).Body.String()), `{"user":null}`)

	out := e.alice.mustStatus(200, "GET", "/v1/me", nil)
	u := out["user"].(map[string]any)
	eq(t, "name", u["name"], any("Alice"))
	eq(t, "email", u["email"], any("alice@example.com"))
	eq(t, "admin", u["admin"], any(false))
	eq(t, "unread", out["unread"], any(0.0))
	eq(t, "admin flag", e.admin.mustStatus(200, "GET", "/v1/me", nil)["user"].(map[string]any)["admin"], any(true))

	// Sliding expiry: no refresh within a day, refresh (and a new cookie) after.
	e.clock = e.clock.Add(time.Hour)
	e.alice.do("GET", "/v1/me", nil)
	eq(t, "no refresh", len(e.alice.last.Result().Cookies()), 0)
	e.clock = e.clock.Add(89 * 24 * time.Hour) // still inside the first 90 days, last seen >1 day ago
	e.alice.do("GET", "/v1/me", nil)
	eq(t, "refreshed cookie", len(e.alice.last.Result().Cookies()), 1)
	var exp int64
	e.db.QueryRow(`SELECT expires_at FROM sessions WHERE hash = ?`, db.HashSecret(e.alice.cookie)).Scan(&exp)
	eq(t, "expiry slid", exp, e.clock.Add(sessionTTL).Unix())
	e.clock = e.clock.Add(30 * 24 * time.Hour) // past the original 90 days
	if e.alice.mustStatus(200, "GET", "/v1/me", nil)["user"] == nil {
		t.Fatal("refreshed session expired")
	}
	// Bob never came back: his session lapsed.
	if e.bob.mustStatus(200, "GET", "/v1/me", nil)["user"] != nil {
		t.Fatal("expired session still valid")
	}
	eq(t, "expired session 401", e.bob.do("GET", "/v1/notifications", nil).Code, 401)

	// Logout deletes the session and clears the cookie.
	old := e.alice.cookie
	rec := e.alice.do("POST", "/v1/auth/logout", nil)
	eq(t, "logout", rec.Code, 200)
	eq(t, "cookie cleared", e.alice.cookie, "")
	e.alice.cookie = old
	if e.alice.mustStatus(200, "GET", "/v1/me", nil)["user"] != nil {
		t.Fatal("session survived logout")
	}
	eq(t, "logout when logged out", e.anon().do("POST", "/v1/auth/logout", nil).Code, 200)
	eq(t, "bad cookie", e.anon().do("GET", "/v1/me", nil, "Cookie", cookieName+"=forged").Code, 200)
}

// ---- CSRF / CORS ----

func TestCSRF(t *testing.T) {
	e := newEnv(t)
	body := map[string]any{"page": page, "body": "hi"}
	e.alice.origin = ""
	eq(t, "no origin", e.alice.do("POST", "/v1/comments", body).Code, 403)
	e.alice.origin = "https://evil.example"
	eq(t, "foreign origin", e.alice.do("POST", "/v1/comments", body).Code, 403)
	eq(t, "foreign origin delete", e.alice.do("DELETE", "/v1/comments/1", nil).Code, 403)
	e.alice.origin = "http://127.0.0.1:5173" // host:* wildcard
	eq(t, "wildcard port", e.alice.do("POST", "/v1/comments", body).Code, 201)
	e.alice.origin = "http://127.0.0.1:x"
	eq(t, "non-numeric port", e.alice.do("POST", "/v1/comments", body).Code, 403)
	e.alice.origin = origin
	eq(t, "form post", e.alice.do("POST", "/v1/comments", "page=/articles/hello/&body=hi",
		"Content-Type", "application/x-www-form-urlencoded").Code, 415)
	eq(t, "text/plain", e.alice.do("POST", "/v1/comments", `{"page":"/articles/hello/","body":"hi"}`, "Content-Type", "text/plain").Code, 415)
	eq(t, "json with charset", e.alice.do("POST", "/v1/comments", `{"page":"/articles/hello/","body":"hi"}`,
		"Content-Type", "application/json; charset=utf-8").Code, 201)
	// GETs need no Origin.
	e.alice.origin = ""
	eq(t, "GET without origin", e.alice.do("GET", "/v1/me", nil).Code, 200)
}

func TestCORS(t *testing.T) {
	e := newEnv(t)
	c := e.anon()
	c.origin = "http://localhost:1313"
	rec := c.do("GET", "/v1/comments?page="+page, nil)
	eq(t, "ACAO", rec.Header().Get("Access-Control-Allow-Origin"), "http://localhost:1313")
	eq(t, "ACAC", rec.Header().Get("Access-Control-Allow-Credentials"), "true")
	eq(t, "Vary", rec.Header().Get("Vary"), "Origin")

	c.origin = "https://evil.example"
	rec = c.do("GET", "/v1/comments?page="+page, nil)
	eq(t, "foreign ACAO", rec.Header().Get("Access-Control-Allow-Origin"), "")
	eq(t, "foreign ACAC", rec.Header().Get("Access-Control-Allow-Credentials"), "")
	eq(t, "foreign Vary", rec.Header().Get("Vary"), "Origin")

	c.origin = origin
	rec = c.do("OPTIONS", "/v1/comments/5/like", nil, "Access-Control-Request-Method", "DELETE")
	eq(t, "preflight status", rec.Code, 204)
	eq(t, "preflight ACAO", rec.Header().Get("Access-Control-Allow-Origin"), origin)
	eq(t, "methods", rec.Header().Get("Access-Control-Allow-Methods"), "GET, POST, DELETE, OPTIONS")
	eq(t, "headers", rec.Header().Get("Access-Control-Allow-Headers"), "Content-Type")
	eq(t, "max-age", rec.Header().Get("Access-Control-Max-Age"), "86400")
	c.origin = ""
	eq(t, "preflight w/o origin", c.do("OPTIONS", "/v1/kyotsu/sessions", nil).Code, 204)
}

func TestMiscRoutes(t *testing.T) {
	e := newEnv(t)
	c := e.anon()
	rec := c.do("GET", "/healthz", nil)
	eq(t, "healthz", rec.Body.String(), "ok")
	rec = c.do("GET", "/v1/nope", nil)
	eq(t, "404", rec.Code, 404)
	eq(t, "404 json", strings.TrimSpace(rec.Body.String()), `{"error":"not found"}`)
	eq(t, "bad id", e.alice.do("DELETE", "/v1/comments/abc", nil).Code, 404)
}

// ---- comments ----

func TestCommentsCRUD(t *testing.T) {
	e := newEnv(t)
	anon := e.anon()
	eq(t, "logged-out post", anon.do("POST", "/v1/comments", map[string]any{"page": page, "body": "x"}).Code, 401)

	id := e.alice.post(map[string]any{"title": "  Hello  ", "body": "  first\r\nline two  "})
	list := anon.comments(page)
	eq(t, "count", len(list), 1)
	c := list[0]
	eq(t, "body", c["body"], any("first\nline two"))
	eq(t, "parent", c["parent"], any(nil))
	eq(t, "author", c["author"].(map[string]any)["name"], any("Alice"))
	eq(t, "author email hidden", c["author"].(map[string]any)["email"], any(nil))
	eq(t, "mine (anon)", c["mine"], any(false))
	eq(t, "deleted", c["deleted"], any(false))
	eq(t, "likes", c["likes"], any(0.0))
	if _, err := time.Parse(time.RFC3339, c["created"].(string)); err != nil || !strings.HasSuffix(c["created"].(string), "Z") {
		t.Fatalf("created %v not RFC 3339 UTC", c["created"])
	}
	eq(t, "mine (alice)", e.alice.comments(page)[0]["mine"], any(true))
	eq(t, "other page empty", len(anon.comments("/articles/other/")), 0)
	var title string
	e.db.QueryRow(`SELECT title FROM pages WHERE path = ?`, page).Scan(&title)
	eq(t, "page title", title, "Hello")

	// Replies.
	reply := e.bob.post(map[string]any{"parent": id, "body": "reply"})
	list = anon.comments(page)
	eq(t, "count", len(list), 2)
	eq(t, "reply parent", list[1]["parent"], any(float64(id)))
	eq(t, "nested reply", e.alice.do("POST", "/v1/comments", map[string]any{"page": page, "parent": reply, "body": "deeper"}).Code, 201)
	eq(t, "parent on other page", e.bob.do("POST", "/v1/comments", map[string]any{"page": "/articles/other/", "parent": id, "body": "x"}).Code, 400)
	eq(t, "missing parent", e.bob.do("POST", "/v1/comments", map[string]any{"page": page, "parent": 9999, "body": "x"}).Code, 400)

	// Validation.
	for _, p := range []any{"", "articles/x/", "/articles/x", "//", "/a b/", "/<script>/", "/" + strings.Repeat("a", 200) + "/", "//evil.com/", "/a//b/", 5, nil} {
		if code := e.alice.do("POST", "/v1/comments", map[string]any{"page": p, "body": "x"}).Code; code != 400 {
			t.Fatalf("page %v: status %d, want 400", p, code)
		}
	}
	for _, p := range []string{"", "articles/x/", "/a b/", "/x"} {
		eq(t, "GET page "+p, anon.do("GET", "/v1/comments?page="+url.QueryEscape(p), nil).Code, 400)
	}
	eq(t, "empty body", e.alice.do("POST", "/v1/comments", map[string]any{"page": page, "body": " \n\t "}).Code, 400)
	eq(t, "5001 runes", e.alice.do("POST", "/v1/comments", map[string]any{"page": page, "body": strings.Repeat("あ", 5001)}).Code, 400)
	eq(t, "5000 runes", e.alice.do("POST", "/v1/comments", map[string]any{"page": page, "body": strings.Repeat("あ", 5000)}).Code, 201)
	eq(t, "bad json", e.alice.do("POST", "/v1/comments", "{nope").Code, 400)
	eq(t, "too large", e.alice.do("POST", "/v1/comments", map[string]any{"page": page, "body": strings.Repeat("a", 70<<10)}).Code, 413)

	// Delete: author or admin only.
	eq(t, "delete other's", e.bob.do("DELETE", fmt.Sprint("/v1/comments/", id), nil).Code, 403)
	eq(t, "delete logged out", anon.do("DELETE", fmt.Sprint("/v1/comments/", id), nil).Code, 401)
	eq(t, "delete missing", e.bob.do("DELETE", "/v1/comments/9999", nil).Code, 404)
	eq(t, "delete own", e.bob.do("DELETE", fmt.Sprint("/v1/comments/", reply), nil).Code, 200)
	eq(t, "delete again", e.bob.do("DELETE", fmt.Sprint("/v1/comments/", reply), nil).Code, 200)
	eq(t, "admin deletes", e.admin.do("DELETE", fmt.Sprint("/v1/comments/", id), nil).Code, 200)
	var body string
	e.db.QueryRow(`SELECT body FROM comments WHERE id = ?`, id).Scan(&body)
	eq(t, "body cleared", body, "")
	eq(t, "reply to deleted", e.bob.do("POST", "/v1/comments", map[string]any{"page": page, "parent": id, "body": "x"}).Code, 409)

	// Rate limit.
	for i := 0; ; i++ {
		code := e.bob.do("POST", "/v1/comments", map[string]any{"page": "/articles/spam/", "body": "spam"}).Code
		if code == 429 {
			break
		}
		if i > 40 {
			t.Fatal("no comment rate limit")
		}
	}
}

func TestDeletedVisibility(t *testing.T) {
	e := newEnv(t)
	a := e.alice.post(map[string]any{"body": "A"}) // deleted, live child B -> tombstone
	e.bob.post(map[string]any{"parent": a, "body": "B"})
	c := e.alice.post(map[string]any{"body": "C"}) // deleted, no children -> hidden
	d := e.alice.post(map[string]any{"body": "D"}) // deleted, only deleted child E -> both hidden
	eID := e.bob.post(map[string]any{"parent": d, "body": "E"})
	f := e.alice.post(map[string]any{"body": "F"}) // deleted -> G deleted -> H live: F, G tombstones
	g := e.bob.post(map[string]any{"parent": f, "body": "G"})
	e.alice.post(map[string]any{"parent": g, "body": "H"})
	e.bob.mustStatus(200, "POST", fmt.Sprintf("/v1/comments/%d/like", a), nil)
	for _, del := range []struct {
		c  *client
		id int64
	}{{e.alice, a}, {e.alice, c}, {e.alice, d}, {e.bob, eID}, {e.alice, f}, {e.bob, g}} {
		del.c.mustStatus(200, "DELETE", fmt.Sprint("/v1/comments/", del.id), nil)
	}
	var got []string
	for _, x := range e.bob.comments(page) {
		if x["deleted"] == true {
			if x["body"] != "" || x["author"] != nil || x["likes"] != 0.0 || x["liked"] != false || x["mine"] != false {
				t.Fatalf("tombstone leaks data: %v", x)
			}
			got = append(got, "deleted")
		} else {
			got = append(got, x["body"].(string))
		}
	}
	eqStrings(t, "visible", got, []string{"deleted", "B", "deleted", "deleted", "H"})
}

func TestLikes(t *testing.T) {
	e := newEnv(t)
	id := e.alice.post(map[string]any{"body": "like me"})
	path := fmt.Sprintf("/v1/comments/%d/like", id)
	baseline := len(e.alice.kinds())

	for i := 0; i < 2; i++ {
		out := e.bob.mustStatus(200, "POST", path, nil)
		eq(t, "likes", out["likes"], any(1.0))
		eq(t, "liked", out["liked"], any(true))
	}
	eq(t, "one like notification", len(e.alice.kinds()), baseline+1)
	eq(t, "liked in list", e.bob.comments(page)[0]["liked"], any(true))
	eq(t, "not liked by alice", e.alice.comments(page)[0]["liked"], any(false))
	eq(t, "count in list", e.anon().comments(page)[0]["likes"], any(1.0))

	// Liking your own comment counts but doesn't notify.
	eq(t, "self like", e.alice.mustStatus(200, "POST", path, nil)["likes"], any(2.0))
	eq(t, "no self notification", len(e.alice.kinds()), baseline+1)

	for i := 0; i < 2; i++ {
		out := e.bob.mustStatus(200, "DELETE", path, nil)
		eq(t, "likes after unlike", out["likes"], any(1.0))
		eq(t, "liked after unlike", out["liked"], any(false))
	}
	eq(t, "unlike removes notification", len(e.alice.kinds()), baseline)
	// Re-like notifies again (fresh notification, not a duplicate).
	e.bob.mustStatus(200, "POST", path, nil)
	eq(t, "re-like", len(e.alice.kinds()), baseline+1)

	eq(t, "logged out", e.anon().do("POST", path, nil).Code, 401)
	eq(t, "missing", e.bob.do("POST", "/v1/comments/9999/like", nil).Code, 404)
	eq(t, "missing unlike", e.bob.do("DELETE", "/v1/comments/9999/like", nil).Code, 404)
	e.alice.mustStatus(200, "DELETE", fmt.Sprint("/v1/comments/", id), nil)
	eq(t, "like deleted", e.bob.do("POST", path, nil).Code, 409)
	eq(t, "unlike deleted", e.bob.mustStatus(200, "DELETE", path, nil)["likes"], any(0.0))
}

// ---- notifications ----

func TestNotificationRules(t *testing.T) {
	e := newEnv(t)
	// Alice comments: admin gets "comment"; Alice gets nothing.
	a1 := e.alice.post(map[string]any{"title": "Hello post", "body": "A1"})
	eqStrings(t, "admin", e.admin.kinds(), []string{fmt.Sprint("comment:Alice:", a1)})
	eqStrings(t, "alice", e.alice.kinds(), nil)

	// Bob replies to Alice: Alice gets "reply", admin gets "comment".
	b1 := e.bob.post(map[string]any{"parent": a1, "body": "B1"})
	eqStrings(t, "alice after reply", e.alice.kinds(), []string{fmt.Sprint("reply:Bob:", b1)})
	eqStrings(t, "admin after reply", e.admin.kinds()[:1], []string{fmt.Sprint("comment:Bob:", b1)})

	// Alice replies to herself: no self-notification.
	a2 := e.alice.post(map[string]any{"parent": a1, "body": "A2"})
	eq(t, "alice self-reply", len(e.alice.kinds()), 1)
	eqStrings(t, "admin", e.admin.kinds()[:1], []string{fmt.Sprint("comment:Alice:", a2)})

	// Admin comments: nobody is notified (admin doesn't notify themself).
	adminCount := len(e.admin.kinds())
	x1 := e.admin.post(map[string]any{"body": "X1"})
	eq(t, "admin own comment", len(e.admin.kinds()), adminCount)

	// Bob replies to the admin: exactly one notification, the reply.
	b2 := e.bob.post(map[string]any{"parent": x1, "body": "B2"})
	eqStrings(t, "admin reply only", e.admin.kinds()[:2], []string{fmt.Sprint("reply:Bob:", b2), fmt.Sprint("comment:Alice:", a2)})

	// Admin replies to Alice: Alice gets the reply, admin nothing.
	x2 := e.admin.post(map[string]any{"parent": a1, "body": "X2"})
	eqStrings(t, "alice from admin", e.alice.kinds()[:1], []string{fmt.Sprint("reply:Admin:", x2)})
	eq(t, "admin unchanged", len(e.admin.kinds()), adminCount+1)

	// Shape.
	list, unread := e.alice.notifications()
	eq(t, "alice unread", unread, 2)
	n := list[0]
	eq(t, "page", n["page"], any(page))
	eq(t, "page_title", n["page_title"], any("Hello post"))
	eq(t, "snippet", n["snippet"], any("X2"))
	eq(t, "read", n["read"], any(false))
	eq(t, "actor admin", n["actor"].(map[string]any)["admin"], any(true))
	if _, err := time.Parse(time.RFC3339, n["created"].(string)); err != nil {
		t.Fatal(err)
	}
	eq(t, "me unread", e.alice.mustStatus(200, "GET", "/v1/me", nil)["unread"], any(2.0))

	// Snippet is the first 140 runes of the comment.
	long := strings.Repeat("語", 200)
	e.bob.post(map[string]any{"parent": a1, "body": long})
	list, _ = e.alice.notifications()
	eq(t, "snippet length", list[0]["snippet"], any(strings.Repeat("語", 140)))

	// Deleting a comment removes notifications pointing at it.
	e.bob.mustStatus(200, "DELETE", fmt.Sprint("/v1/comments/", b1), nil)
	for _, k := range append(e.alice.kinds(), e.admin.kinds()...) {
		if strings.HasSuffix(k, fmt.Sprint(":", b1)) {
			t.Fatalf("notification %s survived delete", k)
		}
	}
	eq(t, "logged out", e.anon().do("GET", "/v1/notifications", nil).Code, 401)
}

func TestNotificationsReadAndPaging(t *testing.T) {
	e := newEnv(t)
	var ids []int64
	for i := 0; i < 5; i++ {
		e.alice.post(map[string]any{"body": fmt.Sprint("c", i)})
	}
	list, unread := e.admin.notifications()
	eq(t, "unread", unread, 5)
	for _, n := range list {
		ids = append(ids, int64(n["id"].(float64)))
	}
	if ids[0] < ids[4] {
		t.Fatalf("not newest first: %v", ids)
	}

	out := e.admin.mustStatus(200, "GET", "/v1/notifications?limit=2", nil)
	eq(t, "limit", len(out["notifications"].([]any)), 2)
	out = e.admin.mustStatus(200, "GET", fmt.Sprint("/v1/notifications?limit=2&before=", ids[1]), nil)
	page2 := out["notifications"].([]any)
	eq(t, "before", int64(page2[0].(map[string]any)["id"].(float64)), ids[2])
	eq(t, "bad limit", e.admin.do("GET", "/v1/notifications?limit=x", nil).Code, 400)
	eq(t, "bad before", e.admin.do("GET", "/v1/notifications?before=-1", nil).Code, 400)

	// Mark by id; someone else's ids are ignored.
	bobsReply := e.bob.post(map[string]any{"parent": e.alice.post(map[string]any{"body": "x"}), "body": "y"})
	_ = bobsReply
	aliceList, aliceUnread := e.alice.notifications()
	eq(t, "alice unread", aliceUnread, 1)
	aliceID := aliceList[0]["id"]
	out = e.admin.mustStatus(200, "POST", "/v1/notifications/read", map[string]any{"ids": []any{ids[0], ids[1], aliceID}})
	eq(t, "admin unread after ids", out["unread"], any(5.0)) // 7 total (2 new) - 2 read
	_, aliceUnread = e.alice.notifications()
	eq(t, "alice untouched", aliceUnread, 1)
	list, _ = e.admin.notifications()
	for _, n := range list {
		id := int64(n["id"].(float64))
		eq(t, fmt.Sprint("read flag ", id), n["read"], any(id == ids[0] || id == ids[1]))
	}

	out = e.admin.mustStatus(200, "POST", "/v1/notifications/read", map[string]any{"all": true})
	eq(t, "all read", out["unread"], any(0.0))
	eq(t, "empty request", e.admin.do("POST", "/v1/notifications/read", map[string]any{}).Code, 400)
	eq(t, "empty ids", e.admin.do("POST", "/v1/notifications/read", map[string]any{"ids": []int{}}).Code, 400)
	eq(t, "bad ids", e.admin.do("POST", "/v1/notifications/read", map[string]any{"ids": "1"}).Code, 400)
}

// ---- kyōtsu ----

func answers(n, correct int) []map[string]any {
	out := make([]map[string]any, n)
	for i := range out {
		out[i] = map[string]any{"reading": "アメ", "surface": "雨", "pattern": "HL", "cat": "HA", "speaker": "yura", "ok": i < correct}
	}
	return out
}

func TestKyotsu(t *testing.T) {
	e := newEnv(t)
	started := e.clock.Add(-20 * time.Minute).Format(time.RFC3339)
	out := e.alice.mustStatus(201, "POST", "/v1/kyotsu/sessions",
		map[string]any{"started": started, "layout": "kotu", "advanced": true, "answers": answers(10, 7)})
	id1 := int64(out["id"].(float64))

	e.clock = e.clock.Add(time.Hour)
	// A future start (client clock ahead) is clamped to the end time.
	out = e.alice.mustStatus(201, "POST", "/v1/kyotsu/sessions",
		map[string]any{"started": e.clock.Add(time.Hour).Format(time.RFC3339), "layout": "classic", "answers": answers(4, 4)})
	id2 := int64(out["id"].(float64))

	list := e.alice.mustStatus(200, "GET", "/v1/kyotsu/sessions", nil)["sessions"].([]any)
	eq(t, "count", len(list), 2)
	s1, s2 := list[0].(map[string]any), list[1].(map[string]any)
	eq(t, "oldest first", int64(s1["id"].(float64)), id1)
	eq(t, "started", s1["started"], any(started))
	eq(t, "total", s1["total"], any(10.0))
	eq(t, "correct", s1["correct"], any(7.0))
	eq(t, "layout", s1["layout"], any("kotu"))
	eq(t, "advanced", s1["advanced"], any(true))
	eq(t, "no answers in list", s1["answers"], any(nil))
	eq(t, "clamped start", s2["started"], s2["ended"])

	one := e.alice.mustStatus(200, "GET", fmt.Sprint("/v1/kyotsu/sessions/", id1), nil)
	eq(t, "detail id", int64(one["id"].(float64)), id1)
	ans := one["answers"].([]any)
	eq(t, "answers", len(ans), 10)
	a0 := ans[0].(map[string]any)
	eq(t, "answer fields", fmt.Sprintln(a0["reading"], a0["surface"], a0["pattern"], a0["cat"], a0["speaker"], a0["ok"]), "アメ 雨 HL HA yura true\n")

	// Owner only.
	eq(t, "bob reads alice's", e.bob.do("GET", fmt.Sprint("/v1/kyotsu/sessions/", id2), nil).Code, 404)
	eq(t, "bob's list", len(e.bob.mustStatus(200, "GET", "/v1/kyotsu/sessions", nil)["sessions"].([]any)), 0)
	eq(t, "missing", e.alice.do("GET", "/v1/kyotsu/sessions/9999", nil).Code, 404)
	eq(t, "logged out", e.anon().do("GET", "/v1/kyotsu/sessions", nil).Code, 401)
	eq(t, "logged out post", e.anon().do("POST", "/v1/kyotsu/sessions", map[string]any{}).Code, 401)

	// Validation.
	valid := func() map[string]any {
		return map[string]any{"started": started, "layout": "kotu", "answers": answers(3, 1)}
	}
	bad := map[string]func(map[string]any){
		"bad layout":   func(m map[string]any) { m["layout"] = "qwerty" },
		"bad started":  func(m map[string]any) { m["started"] = "yesterday" },
		"no started":   func(m map[string]any) { delete(m, "started") },
		"no answers":   func(m map[string]any) { m["answers"] = []any{} },
		"5001 answers": func(m map[string]any) { m["answers"] = answers(5001, 0) },
		"bad cat":      func(m map[string]any) { m["answers"].([]map[string]any)[1]["cat"] = "XX" },
		"long field":   func(m map[string]any) { m["answers"].([]map[string]any)[0]["surface"] = strings.Repeat("字", 65) },
	}
	for name, mutate := range bad {
		m := valid()
		mutate(m)
		eq(t, name, e.alice.do("POST", "/v1/kyotsu/sessions", m).Code, 400)
	}
	m := valid()
	m["answers"] = answers(5000, 5000)
	eq(t, "5000 answers", e.alice.do("POST", "/v1/kyotsu/sessions", m).Code, 201)
	big := fmt.Sprintf(`{"started":%q,"layout":"kotu","answers":[{"reading":%q,"cat":"HA"}]}`, started, strings.Repeat("a", 1<<20))
	eq(t, "over 1 MB", e.alice.do("POST", "/v1/kyotsu/sessions", big).Code, 413)
}
