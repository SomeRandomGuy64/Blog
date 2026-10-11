package api

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"

	"fazon.nexus/backend/internal/db"
)

func TestLevelCurve(t *testing.T) {
	for l, want := range map[int]int64{1: 0, 2: 5, 3: 20, 4: 45, 5: 80, 6: 125, 10: 405, 50: 12005, 100: 49005} {
		eq(t, fmt.Sprint("need(", l, ")"), levelNeed[l-1], want)
	}
	eq(t, "levels", len(levelNeed), 100)
	for i := 2; i < len(levelNeed); i++ {
		if a, b := levelNeed[i-1]-levelNeed[i-2], levelNeed[i]-levelNeed[i-1]; b <= a {
			t.Fatalf("step to level %d (%d) not bigger than the previous (%d)", i+1, b, a)
		}
	}
	for _, c := range []struct {
		xp      int64
		level   int
		levelXP int64
		nextXP  int64 // -1 = nil
	}{{0, 1, 0, 5}, {4, 1, 0, 5}, {5, 2, 5, 20}, {19, 2, 5, 20}, {20, 3, 20, 45}, {49004, 99, 48020, 49005},
		{49005, 100, 49005, -1}, {1 << 40, 100, 49005, -1}} {
		l, lx, nx := levelOf(c.xp)
		next := int64(-1)
		if nx != nil {
			next = *nx
		}
		if l != c.level || lx != c.levelXP || next != c.nextXP {
			t.Fatalf("levelOf(%d) = %d,%d,%d want %d,%d,%d", c.xp, l, lx, next, c.level, c.levelXP, c.nextXP)
		}
	}

	e := newEnv(t)
	rec := e.anon().do("GET", "/v1/levels", nil)
	eq(t, "cache", rec.Header().Get("Cache-Control"), "public, max-age=86400")
	if !strings.HasPrefix(rec.Body.String(), `{"levels":[0,5,20,45,80,125,`) || !strings.HasSuffix(strings.TrimSpace(rec.Body.String()), ",49005]}") {
		t.Fatalf("levels body %s", rec.Body)
	}
}

// level summarises a decoded User object's level fields.
func level(t *testing.T, u any) string {
	t.Helper()
	m := u.(map[string]any)
	return fmt.Sprintf("L%v xp=%v level_xp=%v next_xp=%v", m["level"], m["xp"], m["level_xp"], m["next_xp"])
}

func TestXPAndUserFields(t *testing.T) {
	e := newEnv(t)
	id := e.alice.post(map[string]any{"title": "Hello", "body": "likeable"})
	like := fmt.Sprintf("/v1/comments/%d/like", id)
	me := func() string { return level(t, e.alice.mustStatus(200, "GET", "/v1/me", nil)["user"]) }
	eq(t, "start", me(), "L1 xp=0 level_xp=0 next_xp=5")

	e.alice.mustStatus(200, "POST", like, nil) // self-like: no XP
	eq(t, "self-like", me(), "L1 xp=0 level_xp=0 next_xp=5")
	var fans []*client
	for i := 0; i < 5; i++ {
		email := fmt.Sprintf("fan%d@example.com", i)
		if _, err := db.AddUser(context.Background(), e.db, email, fmt.Sprint("Fan", i), false); err != nil {
			t.Fatal(err)
		}
		fans = append(fans, e.login(email))
		fans[i].mustStatus(200, "POST", like, nil)
	}
	fans[0].mustStatus(200, "POST", like, nil) // idempotent
	want := "L2 xp=5 level_xp=5 next_xp=20"
	eq(t, "/v1/me", me(), want)
	eq(t, "/v1/me keeps email", e.alice.mustStatus(200, "GET", "/v1/me", nil)["user"].(map[string]any)["email"], any("alice@example.com"))
	eq(t, "comment author", level(t, e.anon().comments(page)[0]["author"]), want)
	eq(t, "profile", level(t, e.anon().mustStatus(200, "GET", fmt.Sprint("/v1/users/", e.alice.id), nil)["user"]), want)
	list, _ := e.admin.notifications()
	eq(t, "notification actor", level(t, list[0]["actor"]), want)
	reply := e.bob.mustStatus(201, "POST", "/v1/comments", map[string]any{"page": page, "parent": id, "body": "hi"})
	eq(t, "new comment author", level(t, reply["author"]), "L1 xp=0 level_xp=0 next_xp=5")
	a := e.anon().comments(page)[0]["author"].(map[string]any)
	eq(t, "avatar null", a["avatar"], any(nil))
	eq(t, "no email", a["email"], any(nil))

	fans[1].mustStatus(200, "DELETE", like, nil)
	eq(t, "unlike drops XP", me(), "L1 xp=4 level_xp=0 next_xp=5")
	e.alice.mustStatus(200, "DELETE", fmt.Sprint("/v1/comments/", id), nil)
	eq(t, "delete drops XP", me(), "L1 xp=0 level_xp=0 next_xp=5")
}

func TestPublicUsersBatch(t *testing.T) {
	e := newEnv(t)
	a := e.alice.post(map[string]any{"body": "a"})
	b := e.bob.post(map[string]any{"body": "b"})
	e.bob.mustStatus(200, "POST", fmt.Sprintf("/v1/comments/%d/like", a), nil)
	e.admin.mustStatus(200, "POST", fmt.Sprintf("/v1/comments/%d/like", a), nil)
	e.alice.mustStatus(200, "POST", fmt.Sprintf("/v1/comments/%d/like", b), nil)
	e.bob.mustStatus(200, "POST", fmt.Sprintf("/v1/comments/%d/like", b), nil) // self
	users, err := e.s.publicUsers(context.Background(), []int64{e.alice.id, e.bob.id, e.admin.id, e.alice.id, 9999})
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "count", len(users), 3)
	eq(t, "alice xp", users[e.alice.id].XP, 2)
	eq(t, "bob xp", users[e.bob.id].XP, 1)
	eq(t, "admin xp", users[e.admin.id].XP, 0)
	eq(t, "admin", users[e.admin.id].Admin, true)
	if users[9999] != nil {
		t.Fatal("unknown user returned")
	}
	if m, err := e.s.publicUsers(context.Background(), nil); err != nil || len(m) != 0 {
		t.Fatal("empty lookup", m, err)
	}
}

func encodeJPEG(t *testing.T, img image.Image) []byte {
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func encodePNG(t *testing.T, img image.Image) []byte {
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// banded is w×h: blue left/right thirds of the excess width, red centre square.
func banded(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.RGBA{0, 0, 255, 255}
			if x >= (w-h)/2 && x < (w-h)/2+h {
				c = color.RGBA{255, 0, 0, 255}
			}
			img.Set(x, y, c)
		}
	}
	return img
}

func TestAvatar(t *testing.T) {
	e := newEnv(t)
	upload := func(c *client, ctype string, body []byte) int {
		return c.do("POST", "/v1/me/avatar", body, "Content-Type", ctype).Code
	}
	// A JPEG carrying an EXIF segment (with a fake GPS tag) right after SOI.
	src := encodeJPEG(t, banded(900, 300))
	exif := append([]byte{0xFF, 0xE1, 0x00, 0x16}, []byte("Exif\x00\x00GPS-SECRET-1234")...)
	withExif := append(append(append([]byte{}, src[:2]...), exif...), src[2:]...)
	if _, err := jpeg.Decode(bytes.NewReader(withExif)); err != nil {
		t.Fatal("test JPEG broken:", err)
	}

	eq(t, "jpeg upload", upload(e.alice, "image/jpeg", withExif), 200)
	out := e.alice.mustStatus(200, "GET", "/v1/me", nil)["user"].(map[string]any)
	path, _ := out["avatar"].(string)
	if !strings.HasPrefix(path, fmt.Sprintf("/v1/users/%d/avatar?v=", e.alice.id)) {
		t.Fatalf("avatar %v", out["avatar"])
	}

	rec := e.anon().do("GET", path, nil)
	eq(t, "get", rec.Code, 200)
	eq(t, "type", rec.Header().Get("Content-Type"), "image/jpeg")
	eq(t, "nosniff", rec.Header().Get("X-Content-Type-Options"), "nosniff")
	eq(t, "immutable", rec.Header().Get("Cache-Control"), "public, max-age=31536000, immutable")
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("no ETag")
	}
	body := rec.Body.Bytes()
	if bytes.Contains(body, []byte("Exif")) || bytes.Contains(body, []byte("GPS-SECRET")) {
		t.Fatal("metadata survived")
	}
	img, err := jpeg.Decode(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "size", img.Bounds().Size(), image.Pt(256, 256))
	// Centre crop: the red middle square fills the frame, edge to edge.
	for _, x := range []int{3, 128, 252} {
		r, g, b, _ := img.At(x, 128).RGBA()
		if r>>8 < 200 || g>>8 > 60 || b>>8 > 60 {
			t.Fatalf("pixel %d,128 = %d,%d,%d; want red (centre crop)", x, r>>8, g>>8, b>>8)
		}
	}
	unversioned := fmt.Sprintf("/v1/users/%d/avatar", e.alice.id)
	eq(t, "no-cache", e.anon().do("GET", unversioned, nil).Header().Get("Cache-Control"), "no-cache")
	eq(t, "stale v", e.anon().do("GET", unversioned+"?v=1", nil).Header().Get("Cache-Control"), "no-cache")
	eq(t, "304", e.anon().do("GET", unversioned, nil, "If-None-Match", etag).Code, 304)

	// PNG (with transparency) works, and a re-upload in the same second still gets a new ?v=.
	trans := image.NewNRGBA(image.Rect(0, 0, 300, 500))
	eq(t, "png upload", upload(e.alice, "image/png", encodePNG(t, trans)), 200)
	path2 := e.alice.mustStatus(200, "GET", "/v1/me", nil)["user"].(map[string]any)["avatar"].(string)
	if path2 == path {
		t.Fatal("avatar URL unchanged after re-upload")
	}
	eq(t, "old etag now stale", e.anon().do("GET", unversioned, nil, "If-None-Match", etag).Code, 200)
	img, _ = jpeg.Decode(bytes.NewReader(e.anon().do("GET", path2, nil).Body.Bytes()))
	if r, g, b, _ := img.At(128, 128).RGBA(); r>>8 < 240 || g>>8 < 240 || b>>8 < 240 {
		t.Fatalf("transparent PNG not on white: %d,%d,%d", r>>8, g>>8, b>>8)
	}
	// Avatar shows on comments.
	e.alice.post(map[string]any{"body": "with face"})
	eq(t, "comment avatar", e.anon().comments(page)[0]["author"].(map[string]any)["avatar"], any(path2))

	// Rejections.
	var gb bytes.Buffer
	gif.Encode(&gb, image.NewPaletted(image.Rect(0, 0, 10, 10), []color.Color{color.Black}), nil)
	eq(t, "gif type", upload(e.bob, "image/gif", gb.Bytes()), 415)
	eq(t, "webp type", upload(e.bob, "image/webp", []byte("RIFF....WEBPVP8 ")), 415)
	eq(t, "json type", upload(e.bob, "application/json", []byte(`{}`)), 415)
	eq(t, "gif bytes as jpeg", upload(e.bob, "image/jpeg", gb.Bytes()), 400)
	eq(t, "garbage", upload(e.bob, "image/jpeg", []byte("not an image")), 400)
	eq(t, "png as image/jpeg is fine", upload(e.bob, "image/jpeg", encodePNG(t, banded(50, 50))), 200)
	eq(t, "too wide", upload(e.bob, "image/png", encodePNG(t, image.NewGray(image.Rect(0, 0, 4097, 4)))), 400)
	eq(t, "too tall", upload(e.bob, "image/png", encodePNG(t, image.NewGray(image.Rect(0, 0, 4, 4097)))), 400)
	eq(t, "max size ok", upload(e.bob, "image/png", encodePNG(t, image.NewGray(image.Rect(0, 0, 4096, 1)))), 200)
	eq(t, "body over 2 MB", upload(e.bob, "image/jpeg", make([]byte, 2<<20+1)), 413)
	eq(t, "logged out", upload(e.anon(), "image/jpeg", src), 401)
	e.bob.origin = ""
	eq(t, "no origin", upload(e.bob, "image/jpeg", src), 403)
	e.bob.origin = origin
	// The image content types are only allowed on the avatar route.
	eq(t, "jpeg to comments", e.bob.do("POST", "/v1/comments", src, "Content-Type", "image/jpeg").Code, 415)
	eq(t, "png to profile", e.bob.do("POST", "/v1/me/profile", src, "Content-Type", "image/png").Code, 415)

	// Delete.
	out = e.alice.mustStatus(200, "DELETE", "/v1/me/avatar", nil)
	eq(t, "deleted avatar", out["user"].(map[string]any)["avatar"], any(nil))
	eq(t, "get after delete", e.anon().do("GET", unversioned, nil).Code, 404)
	eq(t, "delete again", e.alice.do("DELETE", "/v1/me/avatar", nil).Code, 200)
	eq(t, "missing user avatar", e.anon().do("GET", "/v1/users/9999/avatar", nil).Code, 404)
}

func TestProfile(t *testing.T) {
	e := newEnv(t)
	first := e.alice.post(map[string]any{"body": strings.Repeat("長", 200)})
	gone := e.alice.post(map[string]any{"body": "deleted later"})
	for i := 0; i < 21; i++ {
		e.alice.post(map[string]any{"page": "/articles/other/", "body": fmt.Sprint("c", i)})
	}
	e.alice.mustStatus(200, "DELETE", fmt.Sprint("/v1/comments/", gone), nil)
	e.bob.mustStatus(200, "POST", fmt.Sprintf("/v1/comments/%d/like", first), nil)
	e.alice.mustStatus(200, "POST", fmt.Sprintf("/v1/comments/%d/like", first), nil)

	rec := e.anon().do("GET", fmt.Sprint("/v1/users/", e.alice.id), nil)
	eq(t, "status", rec.Code, 200)
	if strings.Contains(rec.Body.String(), "@example.com") || strings.Contains(rec.Body.String(), "email") {
		t.Fatalf("profile leaks email: %s", rec.Body)
	}
	out := e.anon().mustStatus(200, "GET", fmt.Sprint("/v1/users/", e.alice.id), nil)
	eq(t, "name", out["user"].(map[string]any)["name"], any("Alice"))
	eq(t, "comments", out["comments"], any(22.0))
	eq(t, "likes = xp", out["likes"], any(1.0))
	if !strings.HasSuffix(out["joined"].(string), "Z") {
		t.Fatalf("joined %v", out["joined"])
	}
	recent := out["recent"].([]any)
	eq(t, "recent count", len(recent), 20)
	eq(t, "newest first", recent[0].(map[string]any)["snippet"], any("c20"))
	eq(t, "page", recent[0].(map[string]any)["page"], any("/articles/other/"))

	// Bob has one comment: check the whole recent entry.
	e.bob.post(map[string]any{"body": strings.Repeat("語", 150)})
	r0 := e.anon().mustStatus(200, "GET", fmt.Sprint("/v1/users/", e.bob.id), nil)["recent"].([]any)[0].(map[string]any)
	eq(t, "page_title", r0["page_title"], any("Hello post"))
	eq(t, "snippet", r0["snippet"], any(strings.Repeat("語", 140)))
	eq(t, "likes", r0["likes"], any(0.0))
	eq(t, "admin profile", e.anon().mustStatus(200, "GET", fmt.Sprint("/v1/users/", e.admin.id), nil)["user"].(map[string]any)["admin"], any(true))

	eq(t, "unknown", e.anon().do("GET", "/v1/users/9999", nil).Code, 404)
	eq(t, "bad id", e.anon().do("GET", "/v1/users/abc", nil).Code, 404)
}

func TestSetName(t *testing.T) {
	e := newEnv(t)
	e.alice.post(map[string]any{"body": "hi"})
	out := e.alice.mustStatus(200, "POST", "/v1/me/profile", map[string]any{"name": "  アリス  "})
	u := out["user"].(map[string]any)
	eq(t, "name", u["name"], any("アリス"))
	eq(t, "level present", u["level"], any(1.0))
	eq(t, "on comments", e.anon().comments(page)[0]["author"].(map[string]any)["name"], any("アリス"))
	eq(t, "40 runes", e.alice.do("POST", "/v1/me/profile", map[string]any{"name": strings.Repeat("名", 40)}).Code, 200)
	for name, v := range map[string]any{
		"empty": "", "blank": "   ", "41 runes": strings.Repeat("名", 41), "newline": "Al\nice", "nul": "Al\x00ice",
		"tab": "Al\tice", "number": 5,
	} {
		eq(t, name, e.alice.do("POST", "/v1/me/profile", map[string]any{"name": v}).Code, 400)
	}
	eq(t, "logged out", e.anon().do("POST", "/v1/me/profile", map[string]any{"name": "X"}).Code, 401)
}
