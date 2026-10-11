package api

import (
	"fmt"
	"testing"
	"time"
)

const likesPath = "/v1/pages/likes?page="

// xp is the account's XP as everyone sees it (the public profile).
func (e *env) xp(id int64) int64 {
	e.t.Helper()
	u := e.anon().mustStatus(200, "GET", fmt.Sprint("/v1/users/", id), nil)["user"].(map[string]any)
	return int64(u["xp"].(float64))
}

// pageLikes is "count liked" for p as c sees it.
func (c *client) pageLikes(p string) string {
	c.e.t.Helper()
	out := c.mustStatus(200, "GET", likesPath+p, nil)
	return fmt.Sprint(out["count"], " ", out["liked"])
}

func (c *client) likePage(method, p string) string {
	c.e.t.Helper()
	out := c.mustStatus(200, method, likesPath+p, nil)
	return fmt.Sprint(out["count"], " ", out["liked"])
}

func TestPageLikes(t *testing.T) {
	e := newEnv(t)
	eq(t, "nothing yet", e.anon().pageLikes(page), "0 false")
	eq(t, "logged out", e.anon().do("POST", likesPath+page, nil).Code, 401)
	eq(t, "logged out unlike", e.anon().do("DELETE", likesPath+page, nil).Code, 401)

	// Idempotent like; liked is per viewer.
	for i := 0; i < 2; i++ {
		eq(t, "alice likes", e.alice.likePage("POST", page), "1 true")
	}
	eq(t, "alice sees", e.alice.pageLikes(page), "1 true")
	eq(t, "bob sees", e.bob.pageLikes(page), "1 false")
	eq(t, "anon sees", e.anon().pageLikes(page), "1 false")
	eq(t, "bob likes", e.bob.likePage("POST", page), "2 true")
	eq(t, "other page", e.anon().pageLikes("/articles/other/"), "0 false")
	eq(t, "title stored", e.storedTitle(page), "Hello post")

	// Each like is a point for the author (the admin account), and notifies them once.
	eq(t, "author XP", e.xp(e.admin.id), int64(2))
	eq(t, "likers' XP", e.xp(e.alice.id)+e.xp(e.bob.id), int64(0))
	eqStrings(t, "author notified", e.admin.kinds(), []string{"page_like:Bob:0", "page_like:Alice:0"})
	list, unread := e.admin.notifications()
	eq(t, "unread", unread, 2)
	n := list[0]
	eq(t, "shape", fmt.Sprint(n["page"], "|", n["page_title"], "|", n["snippet"], "|", n["comment_id"], "|", n["read"]),
		page+"|Hello post||0|false")

	// Unlike is idempotent, takes the point back and withdraws the notification; a re-like
	// notifies again (once).
	for i := 0; i < 2; i++ {
		eq(t, "alice unlikes", e.alice.likePage("DELETE", page), "1 false")
	}
	eq(t, "author XP after unlike", e.xp(e.admin.id), int64(1))
	eqStrings(t, "notification withdrawn", e.admin.kinds(), []string{"page_like:Bob:0"})
	e.alice.likePage("POST", page)
	e.alice.likePage("DELETE", page)
	e.alice.likePage("POST", page)
	eqStrings(t, "re-like", e.admin.kinds(), []string{"page_like:Alice:0", "page_like:Bob:0"})

	// The author may like their own article: it counts, but gives no XP and no notification.
	eq(t, "self like", e.admin.likePage("POST", page), "3 true")
	eq(t, "no XP for a self like", e.xp(e.admin.id), int64(2))
	eq(t, "no self notification", len(e.admin.kinds()), 2)

	// XP is comment likes received plus article likes, for the author only.
	c := e.admin.post(map[string]any{"body": "thanks for reading"})
	e.bob.mustStatus(200, "POST", fmt.Sprint("/v1/comments/", c, "/like"), nil)
	a := e.alice.post(map[string]any{"body": "great post"})
	e.bob.mustStatus(200, "POST", fmt.Sprint("/v1/comments/", a, "/like"), nil)
	eq(t, "author XP with a comment like", e.xp(e.admin.id), int64(3))
	eq(t, "alice XP", e.xp(e.alice.id), int64(1))
	card := e.anon().mustStatus(200, "GET", fmt.Sprint("/v1/users/", e.admin.id, "/card"), nil)
	eq(t, "card", fmt.Sprint(card["likes"], " ", card["user"].(map[string]any)["xp"], " ", card["user"].(map[string]any)["level"]), "3 3 1")
	for i := 0; i < 3; i++ { // 5 XP = level 2
		u := e.user(fmt.Sprint("Reader", i))
		u.likePage("POST", "/articles/other/")
	}
	me := e.admin.mustStatus(200, "GET", "/v1/me", nil)["user"].(map[string]any)
	eq(t, "author level", fmt.Sprint(me["xp"], " ", me["level"]), "6 2")
	// Comment authors who show up next to the article get the same numbers.
	for _, cm := range e.anon().comments(page) {
		if au := cm["author"].(map[string]any); au["id"] == float64(e.admin.id) {
			eq(t, "comment author xp", au["xp"], any(6.0))
		}
	}

	// Muted accounts can still like (muting only blocks commenting).
	e.db.Exec(`UPDATE users SET muted_at = 1 WHERE id = ?`, e.bob.id)
	eq(t, "muted can't comment", e.bob.do("POST", "/v1/comments", map[string]any{"page": page, "body": "x"}).Code, 403)
	eq(t, "muted likes", e.bob.likePage("POST", "/articles/other/"), "4 true")
	eq(t, "muted unlikes", e.bob.likePage("DELETE", "/articles/other/"), "3 false")

	// With no author configured nobody gets XP or notifications from article likes.
	e.s.cfg.AuthorEmail = ""
	eq(t, "no author XP", e.xp(e.admin.id), int64(1)) // the comment like only
	before := len(e.admin.kinds())
	e.bob.likePage("POST", "/articles/other/")
	eq(t, "no author notification", len(e.admin.kinds()), before)
}

func TestPageLikesOnlyIndexedPages(t *testing.T) {
	e := newEnv(t)
	for _, m := range []string{"GET", "POST", "DELETE"} {
		eq(t, m+" unknown page", e.alice.do(m, likesPath+"/articles/nope/", nil).Code, 404)
		eq(t, m+" bad page", e.alice.do(m, likesPath+"/articles/x", nil).Code, 400)
		eq(t, m+" no page", e.alice.do(m, "/v1/pages/likes", nil).Code, 400)
		eq(t, m+" protocol-relative", e.alice.do(m, likesPath+"//evil.example/", nil).Code, 400)
	}
	eq(t, "no rows", e.count(`SELECT COUNT(*) FROM page_likes`), 0)

	// A newly published article is picked up by refreshing the index on a miss.
	e.setIndex(map[string]string{page: "Hello post", "/articles/new/": "Brand new"})
	e.clock = e.clock.Add(2 * pageMissRefresh)
	eq(t, "new article", e.alice.likePage("POST", "/articles/new/"), "1 true")
	eq(t, "its title", e.storedTitle("/articles/new/"), "Brand new")

	// No index has ever loaded: 503, not 404.
	e2 := newEnv(t)
	e2.idxMu.Lock()
	e2.indexErr = fmt.Errorf("site down")
	e2.idxMu.Unlock()
	eq(t, "index unavailable", e2.alice.do("POST", likesPath+page, nil).Code, 503)
}

func TestPageLikesCSRFAndRateLimit(t *testing.T) {
	e := newEnv(t)
	a := *e.alice
	a.origin = ""
	eq(t, "no origin", a.do("POST", likesPath+page, nil).Code, 403)
	a.origin = "https://evil.example"
	eq(t, "foreign origin", a.do("POST", likesPath+page, nil).Code, 403)
	eq(t, "foreign origin unlike", a.do("DELETE", likesPath+page, nil).Code, 403)
	eq(t, "form post", e.alice.do("POST", likesPath+page, "page="+page, "Content-Type", "application/x-www-form-urlencoded").Code, 415)
	eq(t, "nothing liked", e.anon().pageLikes(page), "0 false")
	// GETs need no Origin.
	a.origin = ""
	eq(t, "GET without origin", a.do("GET", likesPath+page, nil).Code, 200)

	// 60 likes/unlikes per 10 minutes per account.
	for i := 0; i < 60; i++ {
		m := "POST"
		if i%2 == 1 {
			m = "DELETE"
		}
		eq(t, fmt.Sprint("toggle ", i), e.alice.do(m, likesPath+page, nil).Code, 200)
	}
	eq(t, "limited", e.alice.do("POST", likesPath+page, nil).Code, 429)
	eq(t, "others unaffected", e.bob.do("POST", likesPath+page, nil).Code, 200)
	e.clock = e.clock.Add(10*time.Minute + time.Second)
	eq(t, "after the window", e.alice.do("POST", likesPath+page, nil).Code, 200)
}
