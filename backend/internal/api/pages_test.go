package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func (e *env) storedTitle(path string) string {
	e.t.Helper()
	var title string
	if err := e.db.QueryRow(`SELECT title FROM pages WHERE path = ?`, path).Scan(&title); err != nil {
		e.t.Fatalf("title of %s: %v", path, err)
	}
	return title
}

// A client-sent title never reaches the database, notifications or profiles.
func TestClientTitleIgnored(t *testing.T) {
	e := newEnv(t)
	spoof := "Re-login at evil.example"
	a := e.alice.post(map[string]any{"title": spoof, "body": "first"})
	e.bob.post(map[string]any{"title": spoof, "parent": a, "body": "reply"})
	eq(t, "stored title", e.storedTitle(page), "Hello post")
	list, _ := e.alice.notifications()
	eq(t, "reply notification title", list[0]["page_title"], any("Hello post"))
	list, _ = e.admin.notifications()
	eq(t, "admin notification title", list[0]["page_title"], any("Hello post"))
	recent := e.anon().mustStatus(200, "GET", fmt.Sprint("/v1/users/", e.bob.id), nil)["recent"].([]any)
	eq(t, "profile title", recent[0].(map[string]any)["page_title"], any("Hello post"))
	if strings.Contains(e.alice.last.Body.String(), "evil") {
		t.Fatal("spoofed title leaked")
	}
}

// Only pages in the site's index take comments; reading any page still works.
func TestUnknownPageRejected(t *testing.T) {
	e := newEnv(t)
	for _, p := range []string{"/articles/made-up/", "/kyotsu/", "/re-login-at-evil-example/"} {
		rec := e.alice.do("POST", "/v1/comments", map[string]any{"page": p, "title": "x", "body": "hi"})
		eq(t, "post on "+p, rec.Code, 404)
		if !strings.Contains(rec.Body.String(), "doesn't take comments") {
			t.Fatalf("unclear error: %s", rec.Body)
		}
		eq(t, "GET "+p, len(e.anon().comments(p)), 0)
	}
	var n int
	e.db.QueryRow(`SELECT (SELECT COUNT(*) FROM pages) + (SELECT COUNT(*) FROM comments)`).Scan(&n)
	eq(t, "nothing stored", n, 0)
	eq(t, "admin not notified", len(e.admin.kinds()), 0)
}

// A page missing from the index (a just-published article) refetches it, at most once a minute;
// known pages never fetch.
func TestIndexRefreshOnMiss(t *testing.T) {
	e := newEnv(t)
	post := func(p string) int {
		return e.alice.do("POST", "/v1/comments", map[string]any{"page": p, "body": "hi"}).Code
	}
	eq(t, "known page", post(page), 201)
	eq(t, "first post loads the index", e.fetchCount(), 1)
	eq(t, "known page again", post(page), 201)
	eq(t, "no fetch for a known page", e.fetchCount(), 1)

	withNew := map[string]string{"/articles/new/": "Brand new"}
	for p, title := range testPages {
		withNew[p] = title
	}
	e.setIndex(withNew)
	eq(t, "new page within the minute", post("/articles/new/"), 404)
	e.clock = e.clock.Add(59 * time.Second)
	eq(t, "still within the minute", post("/articles/new/"), 404)
	eq(t, "rate limited", e.fetchCount(), 1)
	e.clock = e.clock.Add(time.Second)
	eq(t, "after a minute", post("/articles/new/"), 201)
	eq(t, "refetched", e.fetchCount(), 2)
	eq(t, "new page title", e.storedTitle("/articles/new/"), "Brand new")

	// Unknown pages: one fetch a minute however many requests.
	for i := 0; i < 5; i++ {
		eq(t, "unknown", post("/articles/nope/"), 404)
	}
	eq(t, "no refetch inside the minute", e.fetchCount(), 2)
	e.clock = e.clock.Add(time.Minute)
	for i := 0; i < 5; i++ {
		eq(t, "unknown", post("/articles/nope/"), 404)
	}
	eq(t, "one refetch the next minute", e.fetchCount(), 3)
}

// Loading the index rewrites stored titles: ones a client spoofed before this check existed,
// pages no longer in the index (blanked), and renamed articles.
func TestStoredTitlesCorrectedOnReload(t *testing.T) {
	e := newEnv(t)
	now := e.clock.Unix()
	for _, q := range []string{
		`INSERT INTO pages (path, title) VALUES ('/articles/hello/', 'Re-login at evil.example'), ('/fake/', 'Evil'), ('/articles/other/', 'Other post')`,
		fmt.Sprintf(`INSERT INTO comments (id, page, user_id, body, created_at) VALUES (1, '/articles/hello/', %d, 'a', %d), (2, '/fake/', %d, 'b', %d)`,
			e.bob.id, now, e.bob.id, now),
		fmt.Sprintf(`INSERT INTO notifications (user_id, kind, actor_id, comment_id, created_at) VALUES (%d, 'comment', %d, 1, %d), (%d, 'comment', %d, 2, %d)`,
			e.admin.id, e.bob.id, now, e.admin.id, e.bob.id, now),
	} {
		if _, err := e.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.s.RefreshPages(context.Background()); err != nil {
		t.Fatal(err)
	}
	eq(t, "spoofed title corrected", e.storedTitle(page), "Hello post")
	eq(t, "unpublished page blanked", e.storedTitle("/fake/"), "")
	eq(t, "correct title kept", e.storedTitle("/articles/other/"), "Other post")
	list, _ := e.admin.notifications()
	titles := map[any]any{}
	for _, n := range list {
		titles[n["page"]] = n["page_title"]
	}
	eq(t, "notification title", titles[page], any("Hello post"))
	eq(t, "notification on unknown page", titles["/fake/"], any(""))

	e.setIndex(map[string]string{page: "Hello, renamed", "/articles/other/": "Other post"})
	if err := e.s.RefreshPages(context.Background()); err != nil {
		t.Fatal(err)
	}
	eq(t, "renamed", e.storedTitle(page), "Hello, renamed")
}

// A broken index never replaces a good one; with none loaded yet, posting fails closed (503)
// while reads keep working, and it recovers once the site answers.
func TestBadIndex(t *testing.T) {
	e := newEnv(t)
	post := func(p string) *httptest.ResponseRecorder {
		return e.alice.do("POST", "/v1/comments", map[string]any{"page": p, "body": "hi"})
	}
	e.idxMu.Lock()
	e.indexErr = errors.New("site down")
	e.idxMu.Unlock()
	rec := post(page)
	eq(t, "never loaded", rec.Code, 503)
	if !strings.Contains(rec.Body.String(), "temporarily unavailable") {
		t.Fatalf("unclear error: %s", rec.Body)
	}
	eq(t, "reads still work", len(e.anon().comments(page)), 0)
	e.setIndex(testPages)
	eq(t, "retry is rate limited", post(page).Code, 503)
	e.clock = e.clock.Add(time.Minute)
	eq(t, "recovers", post(page).Code, 201)

	huge := []byte(`{"pages":{"/articles/hello/":"` + strings.Repeat("a", maxPageIndexBytes) + `"}}`)
	for name, raw := range map[string]string{
		"html":          "<!doctype html><title>404</title>",
		"empty":         "",
		"null":          "null",
		"array":         `{"pages":[]}`,
		"no pages":      `{"pages":{}}`,
		"wrong field":   `{"page":{"/articles/hello/":"x"}}`,
		"non-string":    `{"pages":{"/articles/hello/":5}}`,
		"invalid paths": `{"pages":{"//evil.example/":"x","nope":"y","/a b/":"z"}}`,
		"truncated":     `{"pages":{"/articles/hello/":"Hel`,
		"oversized":     string(huge),
	} {
		e.setIndex([]byte(raw))
		if err := e.s.RefreshPages(context.Background()); err == nil {
			t.Fatalf("%s: index accepted", name)
		}
		eq(t, name+": old index kept", post("/articles/other/").Code, 201)
		eq(t, name+": title kept", e.storedTitle(page), "Hello post")
	}

	// Bad entries are dropped, the rest load; titles are cleaned.
	e.setIndex(map[string]string{page: "Line\none‮" + strings.Repeat("x", 300), "//evil.example/": "x", "/fine/": "Fine"})
	if err := e.s.RefreshPages(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := e.storedTitle(page); got != "Line one "+strings.Repeat("x", maxTitleRunes-9) {
		t.Fatalf("title not cleaned: %q", got)
	}
	_, ok, _ := e.s.pages.lookup("//evil.example/")
	eq(t, "invalid path dropped", ok, false)
	eq(t, "valid path kept", post("/fine/").Code, 201)
}

// The real fetcher: path, cache-busting query, status and size checks.
func TestHTTPPageFetcher(t *testing.T) {
	var body string
	var gotPath, gotQuery string
	status := 200
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		if f, ok := w.(http.Flusher); ok && strings.HasPrefix(body, "chunked:") {
			w.WriteHeader(status)
			w.Write([]byte(strings.TrimPrefix(body, "chunked:")))
			f.Flush() // no Content-Length: the size cap has to stop the read
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	defer ts.Close()
	fetch := HTTPPageFetcher(ts.URL + "/")
	ctx := context.Background()

	body = `{"pages":{"/articles/x/":"X"}}`
	b, err := fetch(ctx)
	if err != nil || string(b) != body {
		t.Fatalf("fetch: %q %v", b, err)
	}
	eq(t, "path", gotPath, pageIndexPath)
	if !strings.HasPrefix(gotQuery, "t=") {
		t.Fatalf("no cache-busting query: %q", gotQuery)
	}
	status = 404
	if _, err := fetch(ctx); err == nil {
		t.Fatal("404 accepted")
	}
	status = 200
	body = strings.Repeat("a", maxPageIndexBytes+1)
	if _, err := fetch(ctx); err == nil {
		t.Fatal("oversized (Content-Length) accepted")
	}
	body = "chunked:" + strings.Repeat("a", maxPageIndexBytes+1)
	if _, err := fetch(ctx); err == nil {
		t.Fatal("oversized (chunked) accepted")
	}
	body = "chunked:" + strings.Repeat("a", maxPageIndexBytes)
	if b, err := fetch(ctx); err != nil || len(b) != maxPageIndexBytes {
		t.Fatalf("exactly at the cap: %d %v", len(b), err)
	}
}
