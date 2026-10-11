package api

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strconv"
	"strings"
	"testing"
)

// Every response carries nosniff and a script-free CSP, so nothing the API returns (JSON with
// user text, error bodies, avatars) can be rendered as an active page.
func TestSecurityHeaders(t *testing.T) {
	e := newEnv(t)
	var buf bytes.Buffer
	png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 8, 8)))
	if code := e.alice.do("POST", "/v1/me/avatar", buf.Bytes(), "Content-Type", "image/png").Code; code != 200 {
		t.Fatalf("avatar upload %d", code)
	}
	for _, c := range []struct {
		who  *client
		path string
		code int
	}{
		{e.alice, "/v1/me", 200},
		{e.anon(), "/v1/comments?page=" + page, 200},
		{e.anon(), "/v1/nope", 404},
		{e.anon(), "/v1/comments?page=<script>", 400},
		{e.anon(), "/v1/users/" + itoa(e.alice.id) + "/avatar", 200},
		{e.anon(), "/healthz", 200},
	} {
		rec := c.who.do("GET", c.path, nil)
		eq(t, c.path+" status", rec.Code, c.code)
		eq(t, c.path+" nosniff", rec.Header().Get("X-Content-Type-Options"), "nosniff")
		eq(t, c.path+" CSP", rec.Header().Get("Content-Security-Policy"), "default-src 'none'; frame-ancestors 'none'; sandbox")
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// User text comes back verbatim (the client renders it as text), and JSON escapes <, > and &,
// so even a response rendered as HTML would hold no markup.
func TestUserTextVerbatimAndHTMLEscaped(t *testing.T) {
	e := newEnv(t)
	payload := `<script>alert(1)</script> "><svg onload=alert(1)> </textarea> javascript:alert(1) {{ 7*7 }}`
	e.alice.post(map[string]any{"body": payload})
	rec := e.anon().do("GET", "/v1/comments?page="+page, nil)
	raw := rec.Body.String()
	if strings.ContainsAny(raw, "<>") {
		t.Fatalf("raw JSON contains < or >: %s", raw)
	}
	eq(t, "body round-trips", e.anon().comments(page)[0]["body"].(string), payload)
}

// Bidi embedding/override/isolate controls are refused in names (display and sub-category)
// and dropped from (published) page titles: an unclosed U+202E would reverse the surrounding notification text.
func TestBidiControlsInNamesAndTitles(t *testing.T) {
	e := newEnv(t)
	for _, n := range []string{"Eve‮txt.exe", "a‪b", "x⁦y", "⁩"} {
		eq(t, "profile name "+n, e.alice.do("POST", "/v1/me/profile", map[string]any{"name": n}).Code, 400)
		eq(t, "subcat name "+n, e.alice.do("POST", "/v1/track/subcats", map[string]any{"category": "active", "name": n}).Code, 400)
	}
	// ordinary right-to-left text is fine
	eq(t, "hebrew name", e.alice.do("POST", "/v1/me/profile", map[string]any{"name": "שלום Alice"}).Code, 200)

	e.setIndex(map[string]string{page: "Hello‮ world⁦"})
	e.bob.post(map[string]any{"body": "hi"})
	list, _ := e.admin.notifications()
	eq(t, "notifications", len(list), 1)
	eq(t, "title without bidi controls", list[0]["page_title"].(string), "Hello  world")
}

// Only real JPEG/PNG images are accepted, and what's stored is a fresh JPEG: SVG/HTML renamed to
// .png are refused, and bytes appended to a valid PNG (a polyglot) don't survive re-encoding.
func TestAvatarRejectsNonImagesAndPolyglots(t *testing.T) {
	e := newEnv(t)
	up := func(body []byte, ct string) int {
		return e.alice.do("POST", "/v1/me/avatar", body, "Content-Type", ct).Code
	}
	eq(t, "svg as png", up([]byte(`<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"/>`), "image/png"), 400)
	eq(t, "html as jpeg", up([]byte(`<!doctype html><script>alert(1)</script>`), "image/jpeg"), 400)
	eq(t, "svg type", up([]byte(`<svg/>`), "image/svg+xml"), 415)
	eq(t, "html type", up([]byte(`<p>`), "text/html"), 415)

	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	img.Set(1, 1, color.RGBA{255, 0, 0, 255})
	var buf bytes.Buffer
	png.Encode(&buf, img)
	buf.WriteString("<html><script>alert('polyglot')</script></html>")
	eq(t, "polyglot png", up(buf.Bytes(), "image/png"), 200)
	rec := e.anon().do("GET", "/v1/users/"+itoa(e.alice.id)+"/avatar", nil)
	eq(t, "avatar type", rec.Header().Get("Content-Type"), "image/jpeg")
	if b := rec.Body.Bytes(); !bytes.HasPrefix(b, []byte{0xFF, 0xD8, 0xFF}) || bytes.Contains(b, []byte("script")) {
		t.Fatalf("stored avatar is not a clean JPEG (%d bytes)", len(b))
	}
}
