package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"fazon.nexus/backend/internal/db"
	"fazon.nexus/backend/internal/mail"
)

// ---- sign-up ----

// signupFrom pulls the confirmation token and next path out of a sign-up email.
func signupFrom(t *testing.T, m mail.Message) (tok, next string) {
	t.Helper()
	i := strings.Index(m.Text, site+"/account/verify/#s=")
	if i < 0 {
		t.Fatalf("no sign-up link in %q", m.Text)
	}
	link := strings.Fields(m.Text[i:])[0]
	frag, _ := url.ParseQuery(link[strings.Index(link, "#")+1:])
	return frag.Get("s"), frag.Get("next")
}

func (e *env) signup(email, name, next string) {
	e.t.Helper()
	e.anon().mustStatus(202, "POST", "/v1/auth/signup", map[string]string{"email": email, "name": name, "next": next})
	e.s.Wait()
}

func (e *env) count(q string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.db.QueryRow(q, args...).Scan(&n); err != nil {
		e.t.Fatal(q, err)
	}
	return n
}

func TestSignupNoEnumeration(t *testing.T) {
	e := newEnv(t)
	c := e.anon()
	fresh := c.do("POST", "/v1/auth/signup", map[string]string{"email": " New.Person@Example.com ", "name": "  New Person ", "next": "/articles/hello/"})
	known := c.do("POST", "/v1/auth/signup", map[string]string{"email": "ALICE@example.com", "name": "Someone Else", "next": "/kyotsu/"})
	e.s.Wait()
	eq(t, "new status", fresh.Code, 202)
	eq(t, "existing status", known.Code, 202)
	eq(t, "same body", fresh.Body.String(), known.Body.String())
	eq(t, "body", strings.TrimSpace(fresh.Body.String()), `{"ok":true}`)

	msgs := e.mail.all()
	eq(t, "emails sent", len(msgs), 2)
	sort.Slice(msgs, func(i, j int) bool { return msgs[i].Subject < msgs[j].Subject }) // sent in the background: any order
	conf, login := msgs[0], msgs[1]

	// New address: a confirmation link (not a login link), nothing created yet.
	eq(t, "to", conf.To, "New.Person@Example.com")
	eq(t, "subject", conf.Subject, "Confirm your Fazon Nexus account")
	if !strings.Contains(conf.Text, "Hi New Person,") || !strings.Contains(conf.Text, "expires in 60 minutes") ||
		strings.Contains(conf.Text, "#t=") || !strings.Contains(conf.HTML, `href="https://fazon.nexus/account/verify/#s=`) {
		t.Fatalf("confirmation email: %q / %q", conf.Text, conf.HTML)
	}
	eq(t, "no account yet", e.count(`SELECT COUNT(*) FROM users WHERE email = 'new.person@example.com'`), 0)
	eq(t, "pending", e.count(`SELECT COUNT(*) FROM signup_tokens`), 1)

	// Existing address: an ordinary login link, saying so; the name sent is ignored.
	eq(t, "to", login.To, "alice@example.com")
	eq(t, "subject", login.Subject, "Your Fazon Nexus sign-in link")
	if !strings.Contains(login.Text, "Hi Alice,") || !strings.Contains(login.Text, "You already have an account") ||
		!strings.Contains(login.HTML, "You already have an account") || strings.Contains(login.Text, "#s=") {
		t.Fatalf("existing-account email: %q", login.Text)
	}
	ltok, lnext := tokenFrom(t, login)
	eq(t, "login next", lnext, "/kyotsu/")
	out := e.anon().mustStatus(200, "POST", "/v1/auth/verify", map[string]string{"token": ltok})
	eq(t, "logged in as", out["user"].(map[string]any)["name"], any("Alice"))

	// Confirming creates the account and logs in.
	tok, next := signupFrom(t, conf)
	eq(t, "next", next, "/articles/hello/")
	nc := e.anon()
	rec := nc.do("POST", "/v1/auth/signup/confirm", map[string]string{"token": tok})
	eq(t, "confirm", rec.Code, 200)
	var res struct {
		User    selfUser
		Next    string
		Created bool
	}
	json.Unmarshal(rec.Body.Bytes(), &res)
	eq(t, "created", res.Created, true)
	eq(t, "next", res.Next, "/articles/hello/")
	eq(t, "name", res.User.Name, "New Person")
	eq(t, "email", res.User.Email, "New.Person@Example.com")
	eq(t, "role", res.User.Role, "user")
	eq(t, "admin", res.User.Admin, false)
	ck := rec.Result().Cookies()
	if len(ck) != 1 || ck[0].Name != cookieName || !ck[0].HttpOnly || ck[0].MaxAge != 90*24*3600 {
		t.Fatalf("cookie %+v", ck)
	}
	me := nc.mustStatus(200, "GET", "/v1/me", nil)["user"].(map[string]any)
	eq(t, "me", me["name"], any("New Person"))
	eq(t, "me role", me["role"], any("user"))
	eq(t, "me muted", me["muted"], any(false))
	// The new account works like any other: it can comment and log in by email later.
	nc.post(map[string]any{"body": "hello from a new account"})
	e.anon().mustStatus(202, "POST", "/v1/auth/request", map[string]string{"email": "new.person@example.com"})
	e.s.Wait()
	eq(t, "login email", e.mail.all()[2].To, "New.Person@Example.com")
	// Signing up again with the now-existing address sends a login link.
	e.signup("new.person@example.com", "Again", "/")
	eq(t, "again", strings.Contains(e.mail.all()[3].Text, "You already have an account"), true)
	eq(t, "accounts", e.count(`SELECT COUNT(*) FROM users WHERE email = 'new.person@example.com'`), 1)

	for _, bad := range []map[string]string{
		{"email": "", "name": "X"}, {"email": "nope", "name": "X"}, {"email": "x@example.com", "name": " "},
		{"email": "x@example.com", "name": strings.Repeat("n", 41)}, {"email": "x@example.com", "name": "a\u202eb"},
	} {
		eq(t, fmt.Sprint("bad ", bad), e.anon().do("POST", "/v1/auth/signup", bad).Code, 400)
	}
	eq(t, "bad json", e.anon().do("POST", "/v1/auth/signup", "{").Code, 400)
	eq(t, "no origin", (&client{e: e, ip: "198.51.100.9"}).do("POST", "/v1/auth/signup", map[string]string{"email": "z@example.com", "name": "Z"}).Code, 403)
}

func TestSignupTokens(t *testing.T) {
	e := newEnv(t)
	e.signup("pat@example.com", "Pat", "//evil.com/")
	tok, next := signupFrom(t, e.mail.all()[0])
	eq(t, "unsafe next dropped", next, "/")
	// Stored hashed only.
	eq(t, "raw token stored", e.count(`SELECT COUNT(*) FROM signup_tokens WHERE hash = ?`, tok), 0)
	eq(t, "hash stored", e.count(`SELECT COUNT(*) FROM signup_tokens WHERE hash = ?`, db.HashSecret(tok)), 1)
	e.anon().mustStatus(200, "POST", "/v1/auth/signup/confirm", map[string]string{"token": tok})
	// Single use.
	rec := e.anon().do("POST", "/v1/auth/signup/confirm", map[string]string{"token": tok})
	eq(t, "reuse", rec.Code, 400)
	eq(t, "error", strings.TrimSpace(rec.Body.String()), `{"error":"invalid or expired link"}`)
	// A sign-up token is not a login token, nor the other way round.
	eq(t, "as login token", e.anon().do("POST", "/v1/auth/verify", map[string]string{"token": tok}).Code, 400)
	ltok, _ := db.CreateLoginToken(context.Background(), e.db, e.alice.id, "/", e.clock, LoginTokenTTL)
	eq(t, "login token as sign-up", e.anon().do("POST", "/v1/auth/signup/confirm", map[string]string{"token": ltok}).Code, 400)

	// Expiry: 1 hour.
	e.signup("quinn@example.com", "Quinn", "/")
	tok2, _ := signupFrom(t, e.mail.all()[1])
	e.clock = e.clock.Add(time.Hour + time.Second)
	eq(t, "expired", e.anon().do("POST", "/v1/auth/signup/confirm", map[string]string{"token": tok2}).Code, 400)
	eq(t, "no account", e.count(`SELECT COUNT(*) FROM users WHERE email = 'quinn@example.com'`), 0)
	e.signup("quinn@example.com", "Quinn", "/")
	tok3, _ := signupFrom(t, e.mail.all()[2])
	e.clock = e.clock.Add(59 * time.Minute)
	eq(t, "just in time", e.anon().do("POST", "/v1/auth/signup/confirm", map[string]string{"token": tok3}).Code, 200)

	// Two pending links: the first one used spends the other.
	e.signup("rae@example.com", "Rae", "/")
	e.signup("rae@example.com", "Rae Two", "/")
	ta, _ := signupFrom(t, e.mail.all()[3])
	tb, _ := signupFrom(t, e.mail.all()[4])
	e.anon().mustStatus(200, "POST", "/v1/auth/signup/confirm", map[string]string{"token": tb})
	eq(t, "other link spent", e.anon().do("POST", "/v1/auth/signup/confirm", map[string]string{"token": ta}).Code, 400)

	// The address got an account meanwhile (say, the admin's CLI): confirming logs into it.
	e.signup("sam@example.com", "Sam From Signup", "/x/")
	ts, _ := signupFrom(t, e.mail.all()[5])
	sam, err := db.AddUser(context.Background(), e.db, "SAM@example.com", "Sam", false)
	if err != nil {
		t.Fatal(err)
	}
	out := e.anon().mustStatus(200, "POST", "/v1/auth/signup/confirm", map[string]string{"token": ts})
	eq(t, "created", out["created"], any(false))
	eq(t, "same account", int64(out["user"].(map[string]any)["id"].(float64)), sam.ID)
	eq(t, "name kept", out["user"].(map[string]any)["name"], any("Sam"))
	eq(t, "one account", e.count(`SELECT COUNT(*) FROM users WHERE email = 'sam@example.com'`), 1)

	for _, bad := range []any{map[string]string{}, map[string]string{"token": "nope"}, map[string]string{"token": strings.Repeat("a", 200)}, "{"} {
		eq(t, fmt.Sprint("bad ", bad), e.anon().do("POST", "/v1/auth/signup/confirm", bad).Code, 400)
	}
}

func TestSignupHoneypotAndRateLimits(t *testing.T) {
	e := newEnv(t)
	sent := func() int { e.s.Wait(); return len(e.mail.all()) }

	// Honeypot filled: pretend success, send and store nothing (valid or not).
	for _, body := range []map[string]string{
		{"email": "bot@example.com", "name": "Bot", "website": "http://spam.example"},
		{"email": "alice@example.com", "name": "Bot", "website": "x"},
		{"email": "not-an-email", "name": "", "website": "x"},
	} {
		rec := e.anon().do("POST", "/v1/auth/signup", body)
		eq(t, "honeypot status", rec.Code, 202)
		eq(t, "honeypot body", strings.TrimSpace(rec.Body.String()), `{"ok":true}`)
	}
	eq(t, "honeypot emails", sent(), 0)
	eq(t, "honeypot tokens", e.count(`SELECT COUNT(*) FROM signup_tokens`)+e.count(`SELECT COUNT(*) FROM login_tokens`), 3) // the env's 3 logins

	// Per address: 3 per 15 minutes from any IP, shared with login-link requests.
	e.anon().do("POST", "/v1/auth/request", map[string]string{"email": "tess@example.com"}) // unknown: counts, sends nothing
	for i := 0; i < 4; i++ {
		eq(t, "status", e.anon().do("POST", "/v1/auth/signup", map[string]string{"email": "Tess@example.com", "name": "Tess"}).Code, 202)
	}
	eq(t, "per-email sends", sent(), 2)
	eq(t, "pending", e.count(`SELECT COUNT(*) FROM signup_tokens WHERE email = 'tess@example.com'`), 2)
	e.clock = e.clock.Add(16 * time.Minute)
	e.anon().do("POST", "/v1/auth/signup", map[string]string{"email": "tess@example.com", "name": "Tess"})
	eq(t, "after window", sent(), 3)

	// Per IP: 10 per hour, login requests included.
	c := e.anon()
	for i := 0; i < 5; i++ {
		c.do("POST", "/v1/auth/request", map[string]string{"email": fmt.Sprintf("x%d@example.com", i)})
		c.do("POST", "/v1/auth/signup", map[string]string{"email": fmt.Sprintf("y%d@example.com", i), "name": "Y"})
	}
	eq(t, "ten from one IP", sent(), 8)
	eq(t, "status", c.do("POST", "/v1/auth/signup", map[string]string{"email": "zed@example.com", "name": "Zed"}).Code, 202)
	eq(t, "per-IP limit", sent(), 8)
	eq(t, "no token over the limit", e.count(`SELECT COUNT(*) FROM signup_tokens WHERE email = 'zed@example.com'`), 0)
	e.anon().do("POST", "/v1/auth/signup", map[string]string{"email": "zed@example.com", "name": "Zed"})
	eq(t, "other IP", sent(), 9)
}

// ---- roles, muting and the admin API ----

// staffEnv adds a moderator (Mo) and a second regular user (Uma) to the usual admin/alice/bob.
func staffEnv(t *testing.T) (e *env, mod, uma *client) {
	e = newEnv(t)
	mod, uma = e.user("Mo"), e.user("Uma")
	if _, err := e.db.Exec(`UPDATE users SET role = 'moderator' WHERE id = ?`, mod.id); err != nil {
		t.Fatal(err)
	}
	return e, mod, uma
}

func TestRolesInUserObjects(t *testing.T) {
	e, mod, _ := staffEnv(t)
	for _, c := range []struct {
		who   *client
		role  string
		admin bool
	}{{e.admin, "admin", true}, {mod, "moderator", false}, {e.alice, "user", false}} {
		u := c.who.mustStatus(200, "GET", "/v1/me", nil)["user"].(map[string]any)
		eq(t, "role", u["role"], any(c.role))
		eq(t, "admin compat", u["admin"], any(c.admin))
		eq(t, "muted", u["muted"], any(false))
		p := e.anon().mustStatus(200, "GET", "/v1/users/"+itoa(c.who.id), nil)["user"].(map[string]any)
		eq(t, "public role", p["role"], any(c.role))
		if _, ok := p["muted"]; ok {
			t.Fatal("muted state is public")
		}
	}
	// Admins (only) hear about every comment.
	id := e.alice.post(map[string]any{"body": "hi"})
	eq(t, "admin notified", fmt.Sprint(e.admin.kinds()), fmt.Sprintf("[comment:Alice:%d]", id))
	eq(t, "moderator not", len(mod.kinds()), 0)
}

func TestAdminPermissionMatrix(t *testing.T) {
	e, mod, uma := staffEnv(t)
	admin2 := e.user("Ada")
	e.db.Exec(`UPDATE users SET role = 'admin', is_admin = 1 WHERE id = ?`, admin2.id)
	mod2 := e.user("Mona")
	e.db.Exec(`UPDATE users SET role = 'moderator' WHERE id = ?`, mod2.id)

	type act struct {
		method, path string
		body         any
	}
	role := func(id int64, r string) act {
		return act{"POST", "/v1/admin/users/" + itoa(id) + "/role", map[string]string{"role": r}}
	}
	mute := func(id int64) act { return act{"POST", "/v1/admin/users/" + itoa(id) + "/mute", nil} }
	unmute := func(id int64) act { return act{"POST", "/v1/admin/users/" + itoa(id) + "/unmute", nil} }
	del := func(id int64) act { return act{"DELETE", "/v1/admin/users/" + itoa(id), nil} }
	list := act{"GET", "/v1/admin/users", nil}
	try := func(who string, c *client, a act, want int) {
		t.Helper()
		if rec := c.do(a.method, a.path, a.body); rec.Code != want {
			t.Fatalf("%s %s %s: %d, want %d (%s)", who, a.method, a.path, rec.Code, want, rec.Body)
		}
	}

	// Logged out: 401. Regular users: 403 for everything.
	for _, a := range []act{list, role(uma.id, "moderator"), mute(uma.id), unmute(uma.id), del(uma.id)} {
		try("anon", e.anon(), a, 401)
		try("user", e.bob, a, 403)
	}
	// Moderators: list, mute/unmute regular users; nothing else.
	try("mod", mod, list, 200)
	try("mod", mod, mute(uma.id), 200)
	try("mod", mod, mute(uma.id), 200) // idempotent
	try("mod", mod, unmute(uma.id), 200)
	for _, target := range []*client{mod2, e.admin, admin2, mod} {
		try("mod", mod, mute(target.id), 403)
		try("mod", mod, unmute(target.id), 403)
	}
	for _, target := range []*client{uma, mod2, e.admin, mod} {
		try("mod", mod, role(target.id, "user"), 403)
		try("mod", mod, role(target.id, "admin"), 403)
		try("mod", mod, del(target.id), 403)
	}
	// Admins: everything, to anyone but themselves.
	try("admin", e.admin, list, 200)
	try("admin", e.admin, role(uma.id, "moderator"), 200)
	try("admin", e.admin, role(uma.id, "user"), 200)
	try("admin", e.admin, role(uma.id, "boss"), 400)
	try("admin", e.admin, mute(mod2.id), 200)
	try("admin", e.admin, unmute(mod2.id), 200)
	try("admin", e.admin, mute(admin2.id), 200)
	try("admin", e.admin, unmute(admin2.id), 200)
	try("admin", e.admin, role(e.admin.id, "user"), 403)
	try("admin", e.admin, role(e.admin.id, "admin"), 403)
	try("admin", e.admin, mute(e.admin.id), 403)
	try("admin", e.admin, del(e.admin.id), 403)
	try("admin", e.admin, role(999, "user"), 404)
	try("admin", e.admin, mute(999), 404)
	try("admin", e.admin, del(999), 404)
	try("admin", e.admin, del(0), 404) // the deleted-account placeholder is out of reach
	// Demoting the other admin leaves the actor as the last admin, who still can't demote,
	// mute or delete themselves.
	try("admin", e.admin, role(admin2.id, "moderator"), 200)
	eq(t, "admins", e.count(`SELECT COUNT(*) FROM users WHERE role = 'admin'`), 1)
	try("admin", e.admin, role(e.admin.id, "user"), 403)
	try("admin", e.admin, del(e.admin.id), 403)
	try("admin", e.admin, mute(e.admin.id), 403)
	// ...nor delete their own account.
	rec := e.admin.do("DELETE", "/v1/me", nil)
	eq(t, "last admin deletes self", rec.Code, 409)
	eq(t, "still there", e.count(`SELECT COUNT(*) FROM users WHERE id = ?`, e.admin.id), 1)
	// Promote someone: now the former last admin may leave (but not via the admin page).
	try("admin", e.admin, role(uma.id, "admin"), 200)
	eq(t, "is_admin kept in step", e.count(`SELECT is_admin FROM users WHERE id = ?`, uma.id), 1)
	try("admin", e.admin, del(e.admin.id), 403)
	try("new admin", uma, role(e.admin.id, "moderator"), 200)
	eq(t, "is_admin cleared", e.count(`SELECT is_admin FROM users WHERE id = ?`, e.admin.id), 0)
	// The former admin is a moderator now and their session follows at once.
	try("ex-admin", e.admin, role(e.bob.id, "moderator"), 403)
	try("ex-admin", e.admin, mute(e.bob.id), 200)

	// CSRF rules apply: no Origin, a foreign Origin, or a non-JSON body are refused.
	noOrigin := *uma
	noOrigin.origin = ""
	try("no origin", &noOrigin, role(e.bob.id, "moderator"), 403)
	evil := *uma
	evil.origin = "https://evil.example"
	try("evil origin", &evil, del(e.bob.id), 403)
	eq(t, "form body", uma.do("POST", "/v1/admin/users/"+itoa(e.bob.id)+"/role", "role=admin", "Content-Type", "application/x-www-form-urlencoded").Code, 415)

	// rightsOver: the last-admin rule also protects an admin from another admin (defence in
	// depth: with one admin the actor is that admin, so the self rule already applies).
	admin := &user{ID: 1, Role: db.RoleAdmin, Admin: true}
	eq(t, "last admin", rightsOver(admin, 2, db.RoleAdmin, 1), adminRights{})
	eq(t, "two admins", rightsOver(admin, 2, db.RoleAdmin, 2), adminRights{Role: true, Mute: true, Delete: true})
	eq(t, "mod over user", rightsOver(&user{ID: 1, Role: db.RoleModerator}, 2, db.RoleUser, 1), adminRights{Mute: true})
	eq(t, "user", rightsOver(&user{ID: 1, Role: db.RoleUser}, 2, db.RoleUser, 1), adminRights{})
}

func TestAdminList(t *testing.T) {
	e, mod, uma := staffEnv(t)
	e.alice.post(map[string]any{"body": "one"})
	e.alice.post(map[string]any{"body": "two"})
	gone := e.alice.post(map[string]any{"body": "three"})
	e.alice.mustStatus(200, "DELETE", "/v1/comments/"+itoa(gone), nil)
	e.bob.mustStatus(200, "POST", "/v1/track/settings", map[string]any{"public": false})
	mod.mustStatus(200, "POST", "/v1/admin/users/"+itoa(uma.id)+"/mute", nil)

	find := func(out map[string]any, id int64) map[string]any {
		for _, x := range out["users"].([]any) {
			if u := x.(map[string]any); int64(u["user"].(map[string]any)["id"].(float64)) == id {
				return u
			}
		}
		return nil
	}
	out := e.admin.mustStatus(200, "GET", "/v1/admin/users", nil)
	eq(t, "total", out["total"], any(5.0))
	eq(t, "admins", out["admins"], any(1.0))
	eq(t, "viewer", fmt.Sprint(out["viewer"]), fmt.Sprintf("map[id:%d role:admin]", e.admin.id))
	ids := []string{}
	for _, x := range out["users"].([]any) {
		ids = append(ids, x.(map[string]any)["user"].(map[string]any)["name"].(string))
	}
	eqStrings(t, "newest first", ids, []string{"Uma", "Mo", "Bob", "Alice", "Admin"})
	a := find(out, e.alice.id)
	eq(t, "email", a["email"], any("alice@example.com"))
	eq(t, "role", a["role"], any("user"))
	eq(t, "comments", a["comments"], any(2.0))
	if _, err := time.Parse(time.RFC3339, a["joined"].(string)); err != nil {
		t.Fatalf("joined %v", a["joined"])
	}
	eq(t, "last active", a["last_active"], any("2026-10-11T12:00:00Z"))
	eq(t, "track public", a["track_public"], any(true))
	eq(t, "can", fmt.Sprint(a["can"]), "map[delete:true hyojungo:true mute:true role:true]")
	eq(t, "bob track hidden", find(out, e.bob.id)["track_public"], any(false))
	um := find(out, uma.id)
	eq(t, "muted", um["muted"], any(true))
	eq(t, "muted at", um["muted_at"], any("2026-10-11T12:00:00Z"))
	eq(t, "muted by", fmt.Sprint(um["muted_by"]), fmt.Sprintf("map[id:%d name:Mo]", mod.id))
	eq(t, "self", fmt.Sprint(find(out, e.admin.id)["can"]), "map[delete:false hyojungo:false mute:false role:false]")
	if _, ok := find(out, e.alice.id)["user"].(map[string]any)["level"]; !ok {
		t.Fatal("rows carry the public user (avatar, level)")
	}

	// Moderators never see email addresses, and search names only.
	rec := mod.do("GET", "/v1/admin/users", nil)
	eq(t, "mod list", rec.Code, 200)
	if strings.Contains(rec.Body.String(), "@") || strings.Contains(rec.Body.String(), `"email"`) {
		t.Fatalf("emails shown to a moderator: %s", rec.Body)
	}
	var mo map[string]any
	json.Unmarshal(rec.Body.Bytes(), &mo)
	eq(t, "mod can on user", fmt.Sprint(find(mo, e.alice.id)["can"]), "map[delete:false hyojungo:false mute:true role:false]")
	eq(t, "mod can on admin", fmt.Sprint(find(mo, e.admin.id)["can"]), "map[delete:false hyojungo:false mute:false role:false]")
	eq(t, "mod search by email", mod.mustStatus(200, "GET", "/v1/admin/users?q=alice%40", nil)["total"], any(0.0))
	eq(t, "mod search by name", mod.mustStatus(200, "GET", "/v1/admin/users?q=ALI", nil)["total"], any(1.0))
	eq(t, "admin search by email", e.admin.mustStatus(200, "GET", "/v1/admin/users?q=alice%40example", nil)["total"], any(1.0))
	eq(t, "LIKE wildcards are literal", e.admin.mustStatus(200, "GET", "/v1/admin/users?q=%25", nil)["total"], any(0.0))

	// Paging.
	p := e.admin.mustStatus(200, "GET", "/v1/admin/users?limit=2&offset=2", nil)
	eq(t, "page size", len(p["users"].([]any)), 2)
	eq(t, "page total", p["total"], any(5.0))
	eq(t, "page start", p["users"].([]any)[0].(map[string]any)["user"].(map[string]any)["name"], any("Bob"))
	for _, q := range []string{"limit=0", "limit=x", "offset=-1"} {
		eq(t, q, e.admin.do("GET", "/v1/admin/users?"+q, nil).Code, 400)
	}
	eq(t, "limit capped", e.admin.mustStatus(200, "GET", "/v1/admin/users?limit=5000", nil)["limit"], any(200.0))

	// Actions answer the updated row, as the actor sees it.
	r := e.admin.mustStatus(200, "POST", "/v1/admin/users/"+itoa(e.bob.id)+"/role", map[string]string{"role": "moderator"})["user"].(map[string]any)
	eq(t, "new role", r["role"], any("moderator"))
	eq(t, "with email", r["email"], any("bob@example.com"))
	r = mod.mustStatus(200, "POST", "/v1/admin/users/"+itoa(uma.id)+"/unmute", nil)["user"].(map[string]any)
	eq(t, "unmuted", fmt.Sprint(r["muted"], r["muted_at"], r["muted_by"]), "false <nil> <nil>")
	if _, ok := r["email"]; ok {
		t.Fatal("moderator got an email in an action reply")
	}
}

func TestMute(t *testing.T) {
	e, mod, uma := staffEnv(t)
	top := e.alice.post(map[string]any{"body": "top"})
	mine := uma.post(map[string]any{"body": "before the mute"})
	mod.mustStatus(200, "POST", "/v1/admin/users/"+itoa(uma.id)+"/mute", nil)
	eq(t, "muted_by stored", e.count(`SELECT muted_by FROM users WHERE id = ?`, uma.id), int(mod.id))
	eq(t, "muted_at stored", e.count(`SELECT muted_at FROM users WHERE id = ?`, uma.id), int(e.clock.Unix()))

	eq(t, "me muted", uma.mustStatus(200, "GET", "/v1/me", nil)["user"].(map[string]any)["muted"], any(true))
	rec := uma.do("POST", "/v1/comments", map[string]any{"page": page, "body": "let me talk"})
	eq(t, "comment", rec.Code, 403)
	eq(t, "message", strings.TrimSpace(rec.Body.String()), `{"error":"You've been blocked from commenting."}`)
	eq(t, "reply", uma.do("POST", "/v1/comments", map[string]any{"page": page, "parent": top, "body": "reply"}).Code, 403)
	// Everything else still works: likes, deleting their own comment, profile, Track.
	uma.mustStatus(200, "POST", "/v1/comments/"+itoa(top)+"/like", nil)
	uma.mustStatus(200, "DELETE", "/v1/comments/"+itoa(top)+"/like", nil)
	uma.mustStatus(200, "POST", "/v1/me/profile", map[string]any{"bio": "still here"})
	uma.saveWeek(thisWeek, map[string]any{"hours": []any{hrs("active", nil, 1)}})
	uma.mustStatus(200, "DELETE", "/v1/comments/"+itoa(mine), nil)
	eq(t, "comments while muted", e.count(`SELECT COUNT(*) FROM comments WHERE user_id = ? AND deleted_at IS NULL`, uma.id), 0)

	mod.mustStatus(200, "POST", "/v1/admin/users/"+itoa(uma.id)+"/unmute", nil)
	eq(t, "me unmuted", uma.mustStatus(200, "GET", "/v1/me", nil)["user"].(map[string]any)["muted"], any(false))
	uma.post(map[string]any{"body": "back again", "parent": top})
}

func TestModeratorDeletesAnyComment(t *testing.T) {
	e, mod, _ := staffEnv(t)
	a := e.alice.post(map[string]any{"body": "alice's"})
	b := e.admin.post(map[string]any{"body": "the admin's"})
	reply := e.bob.post(map[string]any{"body": "reply", "parent": a})
	eq(t, "user can't", e.bob.do("DELETE", "/v1/comments/"+itoa(a), nil).Code, 403)

	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	mod.mustStatus(200, "DELETE", "/v1/comments/"+itoa(a), nil)
	mod.mustStatus(200, "DELETE", "/v1/comments/"+itoa(b), nil) // even an admin's
	e.admin.mustStatus(200, "DELETE", "/v1/comments/"+itoa(reply), nil)
	e.admin.mustStatus(200, "POST", "/v1/admin/users/"+itoa(e.bob.id)+"/mute", nil)
	slog.SetDefault(old)
	for _, want := range []string{
		fmt.Sprintf("msg=moderation actor=%d action=delete_comment comment=%d target=%d", mod.id, a, e.alice.id),
		fmt.Sprintf("msg=moderation actor=%d action=delete_comment comment=%d target=%d", e.admin.id, reply, e.bob.id),
		fmt.Sprintf(`msg="admin action" actor=%d action=mute target=%d`, e.admin.id, e.bob.id),
	} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("log lacks %q:\n%s", want, logs.String())
		}
	}
	eq(t, "all deleted", len(e.anon().comments(page)), 0)
}

// ---- deleting an account ----

// refs counts, per table, the rows that refer to the account id (email for signup_tokens).
// Every table in the schema must be listed (nil = holds no per-user data), so a new table
// fails this test until account deletion is taught about it.
func refs(t *testing.T, e *env, id int64, email string) map[string]int {
	t.Helper()
	where := map[string]string{
		"users":           "id = ?1 OR muted_by = ?1",
		"sessions":        "user_id = ?1",
		"login_tokens":    "user_id = ?1",
		"signup_tokens":   "email = ?2",
		"comments":        "user_id = ?1",
		"comment_likes":   "user_id = ?1",
		"page_likes":      "user_id = ?1",
		"notifications":   "user_id = ?1 OR actor_id = ?1",
		"kyotsu_sessions": "user_id = ?1",
		"watch_sessions":  "user_id = ?1",
		"avatars":         "user_id = ?1",
		"track_subcats":   "user_id = ?1",
		"track_weeks":     "user_id = ?1",
		"track_hours":     "user_id = ?1",
		"track_ratings":   "user_id = ?1",
		"track_goals":     "user_id = ?1",
		"api_keys":        "user_id = ?1",
		"pages":           "",
		"settings":        "",
	}
	rows, err := e.db.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var n string
		rows.Scan(&n)
		tables = append(tables, n)
	}
	rows.Close()
	out := map[string]int{}
	for _, tb := range tables {
		w, ok := where[tb]
		if !ok {
			t.Fatalf("table %s isn't covered by account deletion: add it to db.DeleteUser and this test", tb)
		}
		// Every foreign key to users must be one of the columns checked.
		fks, err := e.db.Query(`SELECT "from" FROM pragma_foreign_key_list(?) WHERE "table" = 'users'`, tb)
		if err != nil {
			t.Fatal(err)
		}
		for fks.Next() {
			var col string
			fks.Scan(&col)
			if !strings.Contains(w, col+" = ?1") {
				t.Fatalf("%s.%s references users but isn't checked", tb, col)
			}
		}
		fks.Close()
		if w != "" {
			out[tb] = e.count(`SELECT COUNT(*) FROM `+tb+` WHERE `+w, id, email)
		}
	}
	return out
}

func TestDeleteAccount(t *testing.T) {
	e, mod, _ := staffEnv(t)
	vic := e.user("Vic")
	// Vic is a moderator who muted Bob.
	e.admin.mustStatus(200, "POST", "/v1/admin/users/"+itoa(vic.id)+"/role", map[string]string{"role": "moderator"})
	vic.mustStatus(200, "POST", "/v1/admin/users/"+itoa(e.bob.id)+"/mute", nil)

	// Comments: c1 alone; c2 with Alice's reply r2 (liked by Alice); v3 a reply to Alice's a3;
	// c4 with Vic's own reply c5; c6 whose only reply (Bob's) Bob deleted.
	c1 := vic.post(map[string]any{"body": "c1"})
	c2 := vic.post(map[string]any{"body": "c2"})
	r2 := e.alice.post(map[string]any{"body": "r2", "parent": c2})
	e.alice.mustStatus(200, "POST", "/v1/comments/"+itoa(c2)+"/like", nil)
	a3 := e.alice.post(map[string]any{"body": "a3"})
	v3 := vic.post(map[string]any{"body": "v3", "parent": a3})
	c4 := vic.post(map[string]any{"body": "c4"})
	c5 := vic.post(map[string]any{"body": "c5", "parent": c4})
	c6 := vic.post(map[string]any{"body": "c6"})
	e.db.Exec(`UPDATE users SET muted_at = NULL WHERE id = ?`, e.bob.id) // Bob may reply
	b6 := e.bob.post(map[string]any{"body": "b6", "parent": c6})
	e.bob.mustStatus(200, "DELETE", "/v1/comments/"+itoa(b6), nil)
	e.db.Exec(`UPDATE users SET muted_at = 1 WHERE id = ?`, e.bob.id)
	vic.mustStatus(200, "POST", "/v1/comments/"+itoa(a3)+"/like", nil)
	vic.mustStatus(200, "POST", "/v1/comments/"+itoa(r2)+"/like", nil)
	// Article likes: Vic's go (and with them the author's points), Alice's stay.
	vic.mustStatus(200, "POST", "/v1/pages/likes?page="+page, nil)
	vic.mustStatus(200, "POST", "/v1/pages/likes?page=/articles/other/", nil)
	e.alice.mustStatus(200, "POST", "/v1/pages/likes?page="+page, nil)
	adminXP := e.xp(e.admin.id)
	_ = c1
	_ = v3
	_ = c5

	// Track, Kyōtsu, watch time, avatar, API key, a pending login link and a stray sign-up link.
	sub := vic.subcat("active", "Anime")
	vic.saveWeek(thisWeek, map[string]any{"hours": []any{hrs("active", sub, 2), hrs("passive", nil, 1)},
		"summary": "my week", "ratings": map[string]any{"active": 4}})
	eq(t, "goals", vic.goals(thisWeek, map[string]any{"active": 5}), 200)
	e.kyotsuAt(vic, e.clock.Add(-time.Hour), 10, 7)
	vic.watch(201, "watch-vic-01", e.clock.Add(-time.Hour), 600, "")
	eq(t, "avatar", vic.do("POST", "/v1/me/avatar", encodeJPEG(t, banded(64, 64)), "Content-Type", "image/jpeg").Code, 200)
	vic.newKey("script", "write")
	db.CreateLoginToken(context.Background(), e.db, vic.id, "/", e.clock, LoginTokenTTL)
	db.CreateSignupToken(context.Background(), e.db, "VIC@example.com", "Vic", "/", e.clock)

	before := refs(t, e, vic.id, "vic@example.com")
	for tb, n := range before {
		if n == 0 {
			t.Fatalf("test setup: no %s rows for the user (%v)", tb, before)
		}
	}
	others := e.count(`SELECT COUNT(*) FROM comments WHERE user_id != ?`, vic.id)
	aliceLikes := e.count(`SELECT COUNT(*) FROM comment_likes WHERE user_id = ?`, e.alice.id)

	// Deleting needs the usual Origin; then the session is gone and the cookie cleared.
	noOrigin := *vic
	noOrigin.origin = ""
	eq(t, "no origin", noOrigin.do("DELETE", "/v1/me", nil).Code, 403)
	eq(t, "logged out", e.anon().do("DELETE", "/v1/me", nil).Code, 401)
	oldCookie := vic.cookie
	vic.mustStatus(200, "DELETE", "/v1/me", nil)
	eq(t, "cookie cleared", vic.cookie, "")
	vic.cookie = oldCookie
	if vic.mustStatus(200, "GET", "/v1/me", nil)["user"] != nil {
		t.Fatal("session survived account deletion")
	}

	for tb, n := range refs(t, e, vic.id, "vic@example.com") {
		if n != 0 {
			t.Fatalf("%d %s rows still refer to the deleted account", n, tb)
		}
	}
	if got := dumpCheck(t, e, `PRAGMA foreign_key_check`); got != "" {
		t.Fatalf("foreign_key_check: %s", got)
	}
	eq(t, "Bob still muted", e.count(`SELECT muted_at IS NOT NULL AND muted_by IS NULL FROM users WHERE id = ?`, e.bob.id), 1)
	eq(t, "others' comments kept", e.count(`SELECT COUNT(*) FROM comments WHERE user_id NOT IN (?, 0)`, vic.id), others)
	eq(t, "Alice's like on c2 gone, others kept", e.count(`SELECT COUNT(*) FROM comment_likes WHERE user_id = ?`, e.alice.id), aliceLikes-1)
	eq(t, "article likes", e.anon().mustStatus(200, "GET", "/v1/pages/likes?page="+page, nil)["count"], any(1.0))
	eq(t, "author XP back", e.xp(e.admin.id), adminXP-2)

	// Threads: c2 is a placeholder holding Alice's reply; c6 (only a tombstone under it) and
	// everything without replies are gone from the database.
	eq(t, "rows left", fmt.Sprint(e.count(`SELECT COUNT(*) FROM comments WHERE id IN (?, ?, ?, ?, ?)`, c1, v3, c4, c5, b6)), "1") // b6 (Bob's tombstone) stays
	eq(t, "placeholders", e.count(`SELECT COUNT(*) FROM comments WHERE user_id = 0 AND deleted_at IS NOT NULL AND body = '' AND id IN (?, ?)`, c2, c6), 2)
	list := e.anon().comments(page)
	got := []string{}
	for _, c := range list {
		s := fmt.Sprint(int64(c["id"].(float64)), ":", c["body"], ":", c["deleted"])
		if c["author"] != nil {
			s += ":" + c["author"].(map[string]any)["name"].(string)
		}
		got = append(got, s)
	}
	eqStrings(t, "thread", got, []string{fmt.Sprint(c2, "::true"), fmt.Sprint(r2, ":r2:false:Alice"), fmt.Sprint(a3, ":a3:false:Alice")})
	// The placeholder account is invisible: not listed, not a profile, not loggable-in.
	out := e.admin.mustStatus(200, "GET", "/v1/admin/users", nil)
	eq(t, "listed", out["total"], any(5.0)) // admin, alice, bob, mo, uma
	eq(t, "no profile", e.anon().do("GET", "/v1/users/0", nil).Code, 404)
	_ = mod

	// The address is free again: signing up makes a brand-new account.
	n := len(e.mail.all())
	e.signup("vic@example.com", "Vic Again", "/")
	tok, _ := signupFrom(t, e.mail.all()[n])
	res := e.anon().mustStatus(200, "POST", "/v1/auth/signup/confirm", map[string]string{"token": tok})
	eq(t, "new account", res["created"], any(true))
	eq(t, "fresh start", fmt.Sprintf("%v %v", res["user"].(map[string]any)["role"], res["user"].(map[string]any)["xp"]), "user 0")

	// A second deletion with a placeholder reuses the same placeholder account.
	zed := e.user("Zed")
	z := zed.post(map[string]any{"body": "z"})
	e.alice.post(map[string]any{"body": "re z", "parent": z})
	// The admin page deletes through the same function.
	e.admin.mustStatus(200, "DELETE", "/v1/admin/users/"+itoa(zed.id), nil)
	for tb, n := range refs(t, e, zed.id, "zed@example.com") {
		if n != 0 {
			t.Fatalf("%d %s rows still refer to the account deleted by the admin", n, tb)
		}
	}
	eq(t, "one placeholder account", e.count(`SELECT COUNT(*) FROM users WHERE id = 0`), 1)
	eq(t, "placeholders", e.count(`SELECT COUNT(*) FROM comments WHERE user_id = 0`), 3)
	eq(t, "zed's session", zed.do("GET", "/v1/notifications", nil).Code, 401)
}

func dumpCheck(t *testing.T, e *env, q string) string {
	t.Helper()
	rows, err := e.db.Query(q)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var b strings.Builder
	cols, _ := rows.Columns()
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		rows.Scan(ptrs...)
		fmt.Fprintln(&b, vals...)
	}
	return b.String()
}

// ---- export ----

// sp formats its operands separated by spaces (fmt.Sprint leaves none next to strings).
func sp(vals ...any) string { return strings.TrimSuffix(fmt.Sprintln(vals...), "\n") }

func TestExport(t *testing.T) {
	e := newEnv(t)
	a := e.alice
	top := a.post(map[string]any{"body": "Alice's <b>first</b>"})
	bobs := e.bob.post(map[string]any{"body": "bob here"})
	reply := a.post(map[string]any{"body": "a reply", "parent": bobs})
	gone := a.post(map[string]any{"body": "deleted later"})
	a.mustStatus(200, "DELETE", "/v1/comments/"+itoa(gone), nil)
	a.mustStatus(200, "POST", "/v1/comments/"+itoa(bobs)+"/like", nil)
	e.bob.mustStatus(200, "POST", "/v1/comments/"+itoa(top)+"/like", nil)
	a.mustStatus(200, "POST", "/v1/pages/likes?page=/articles/other/", nil)
	e.clock = e.clock.Add(time.Minute)
	a.mustStatus(200, "POST", "/v1/pages/likes?page="+page, nil)
	e.clock = e.clock.Add(-time.Minute)
	e.bob.mustStatus(200, "POST", "/v1/pages/likes?page="+page, nil) // not Alice's
	a.mustStatus(200, "POST", "/v1/me/profile", map[string]any{"bio": "Learning Kansai-ben"})
	sub := a.subcat("anki", "Mining")
	a.saveWeek(lastWeek, map[string]any{"hours": []any{hrs("anki", sub, 1.5), hrs("active", nil, 3)},
		"summary": "a **private** week", "summary_private": true, "ratings": map[string]any{"anki": 3},
		"kotu": nums(80, nil, 90)})
	eq(t, "goals", a.goals(thisWeek, map[string]any{"active": 10}), 200)
	e.kyotsuAt(a, e.clock.Add(-time.Hour), 4, 3)
	eq(t, "avatar", a.do("POST", "/v1/me/avatar", encodeJPEG(t, banded(64, 64)), "Content-Type", "image/jpeg").Code, 200)
	key := a.newKey("sync script", "read")

	rec := a.do("GET", "/v1/me/export", nil)
	eq(t, "status", rec.Code, 200)
	eq(t, "disposition", rec.Header().Get("Content-Disposition"), `attachment; filename="fazon-nexus-export-2026-10-11.json"`)
	eq(t, "type", rec.Header().Get("Content-Type"), "application/json; charset=utf-8")
	eq(t, "no-store", rec.Header().Get("Cache-Control"), "no-store")
	body := rec.Body.String()
	for _, bad := range []string{key, key[len(apiKeyPrefix):], `"hash"`, "deleted later", "bob@example.com", "admin@example.com"} {
		if strings.Contains(body, bad) {
			t.Fatalf("export contains %q", bad)
		}
	}
	var x struct {
		Exported string
		Profile  struct {
			ID                     int64
			Name, Email, Role, Bio string
			Joined                 string
			Avatar                 *string
			Muted                  bool
		}
		Comments      []exportComment
		Likes         []exportLike
		ArticleLikes  []exportPageLike `json:"article_likes"`
		Notifications []exportNotification
		Track         struct {
			Public  bool
			Subcats []subcatJSON
			Weeks   []struct {
				Week     string
				Logged   bool
				Hours    map[string]apiCatHours
				Ratings  map[string]int
				Goals    map[string]float64
				Summary  string
				Private  bool
				Kotu     *float64
				KotuDays []*float64 `json:"kotu_days"`
			}
		}
		KyotsuSessions []kyotsuSummary `json:"kyotsu_sessions"`
		APIKeys        []apiKeyJSON    `json:"api_keys"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &x); err != nil {
		t.Fatal(err)
	}
	eq(t, "exported", x.Exported, "2026-10-11T12:00:00Z")
	eq(t, "profile", fmt.Sprint(x.Profile.ID, x.Profile.Name, x.Profile.Email, x.Profile.Role, x.Profile.Bio, x.Profile.Muted),
		fmt.Sprint(a.id, "Alice", "alice@example.com", "user", "Learning Kansai-ben", false))
	if _, err := time.Parse(time.RFC3339, x.Profile.Joined); err != nil {
		t.Fatalf("joined %q", x.Profile.Joined)
	}
	if x.Profile.Avatar == nil || !strings.HasPrefix(*x.Profile.Avatar, fmt.Sprintf("http://example.com/v1/users/%d/avatar?v=", a.id)) {
		t.Fatalf("avatar %v", x.Profile.Avatar)
	}
	eq(t, "comments", len(x.Comments), 2)
	eq(t, "comment", fmt.Sprint(x.Comments[0].ID, x.Comments[0].Page, x.Comments[0].PageTitle, x.Comments[0].Body, x.Comments[0].Parent == nil),
		fmt.Sprint(top, page, "Hello post", "Alice's <b>first</b>", true))
	eq(t, "reply parent", *x.Comments[1].Parent, bobs)
	eq(t, "reply id", x.Comments[1].ID, reply)
	eq(t, "likes", fmt.Sprint(len(x.Likes), x.Likes[0].CommentID, x.Likes[0].PageTitle), fmt.Sprint(1, bobs, "Hello post"))
	eq(t, "article likes", fmt.Sprint(x.ArticleLikes), "[{/articles/other/ Other post 2026-10-11T12:00:00Z} {"+page+" Hello post 2026-10-11T12:01:00Z}]")
	kinds := []string{}
	for _, n := range x.Notifications {
		kinds = append(kinds, n.Kind+":"+n.Actor)
	}
	eqStrings(t, "notifications", kinds, []string{"like:Bob"})
	eq(t, "track public", x.Track.Public, true)
	eq(t, "subcats", sp(len(x.Track.Subcats), x.Track.Subcats[0].Name), "1 Mining")
	eq(t, "weeks", len(x.Track.Weeks), 2) // last week (logged) and this week (goals only)
	w := x.Track.Weeks[0]
	eq(t, "week", w.Week, lastWeek)
	eq(t, "hours", sp(w.Hours["anki"].Subcats[0].Name, w.Hours["anki"].Subcats[0].Hours, w.Hours["active"].Unassigned), "Mining 1.5 3")
	eq(t, "summary", sp(w.Summary, w.Private), "a **private** week true")
	eq(t, "ratings", fmt.Sprint(w.Ratings), "map[anki:3]")
	eq(t, "kotu", *w.Kotu, 85.0)
	if len(w.KotuDays) != 7 || w.KotuDays[0] == nil || *w.KotuDays[0] != 80 {
		t.Fatalf("days %+v", w.KotuDays)
	}
	eq(t, "this week goals", sp(x.Track.Weeks[1].Week, x.Track.Weeks[1].Goals, x.Track.Weeks[1].Logged), thisWeek+" map[active:10] false")
	eq(t, "kyotsu", fmt.Sprint(len(x.KyotsuSessions), x.KyotsuSessions[0].Total, x.KyotsuSessions[0].Correct, len(x.KyotsuSessions[0].Answers) > 10), "1 4 3 true")
	eq(t, "keys", sp(len(x.APIKeys), x.APIKeys[0].Name, x.APIKeys[0].Scope, x.APIKeys[0].Prefix), "1 sync script read "+key[:7])

	// An empty account exports fine; logged out is 401; 10 an hour.
	empty := e.bob.mustStatus(200, "GET", "/v1/me/export", nil)
	eq(t, "bob's track", fmt.Sprint(empty["track"]), "map[public:true subcats:[] weeks:[]]")
	eq(t, "logged out", e.anon().do("GET", "/v1/me/export", nil).Code, 401)
	for i := 0; i < 9; i++ { // 10 in all
		eq(t, "export", a.do("GET", "/v1/me/export", nil).Code, 200)
	}
	rec = a.do("GET", "/v1/me/export", nil)
	eq(t, "limited", rec.Code, 429)
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("no Retry-After")
	}
	e.clock = e.clock.Add(time.Hour + time.Second)
	eq(t, "after an hour", a.do("GET", "/v1/me/export", nil).Code, 200)
}
