package api

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png" // registers PNG with image.Decode
	"io"
	"net/http"
	"strconv"

	"golang.org/x/image/draw"
)

const (
	avatarUploadPath = "/v1/me/avatar"
	maxAvatarBody    = 2 << 20
	maxAvatarSide    = 4096
	avatarSize       = 256
	avatarQuality    = 85
)

var avatarTypes = map[string]bool{"image/jpeg": true, "image/png": true}

// makeAvatar turns an uploaded JPEG/PNG into a centre-cropped 256×256 JPEG. Re-encoding drops
// every bit of metadata (EXIF, GPS, ICC). Dimensions are checked from the header first so a
// tiny file claiming 50000×50000 pixels is refused before anything is allocated.
func makeAvatar(raw []byte) ([]byte, error) {
	// Check the sniffed format too: any package importing e.g. image/gif registers its decoder.
	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || format != "jpeg" && format != "png" {
		return nil, errors.New("not a readable JPEG or PNG image")
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > maxAvatarSide || cfg.Height > maxAvatarSide {
		return nil, fmt.Errorf("image must be at most %d×%d pixels", maxAvatarSide, maxAvatarSide)
	}
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, errors.New("not a readable JPEG or PNG image")
	}
	b := src.Bounds()
	side := min(b.Dx(), b.Dy())
	x0, y0 := b.Min.X+(b.Dx()-side)/2, b.Min.Y+(b.Dy()-side)/2
	crop := image.Rect(x0, y0, x0+side, y0+side)
	dst := image.NewRGBA(image.Rect(0, 0, avatarSize, avatarSize))
	// White underneath, so transparent PNG areas don't turn black in the JPEG.
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, crop, draw.Over, nil)
	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: avatarQuality}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// putAvatar takes the raw image body; the guard has already limited its content type to
// JPEG or PNG for this route.
func (s *Server) putAvatar(w http.ResponseWriter, r *http.Request, u *user) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxAvatarBody))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeErr(w, http.StatusRequestEntityTooLarge, "image must be at most 2 MB")
		} else {
			writeErr(w, http.StatusBadRequest, "could not read the upload")
		}
		return
	}
	if !s.avatarLimit.allow(strconv.FormatInt(u.ID, 10), s.now()) {
		writeErr(w, http.StatusTooManyRequests, "too many uploads, slow down")
		return
	}
	img, err := makeAvatar(raw)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// updated_at is the URL's cache-buster for an immutable response, so it must change on
	// every upload, even two in the same second.
	if _, err := s.db.ExecContext(r.Context(), `INSERT INTO avatars (user_id, data, updated_at) VALUES (?, ?, ?)
		ON CONFLICT (user_id) DO UPDATE SET data = excluded.data,
			updated_at = MAX(excluded.updated_at, avatars.updated_at + 1)`, u.ID, img, s.now().Unix()); err != nil {
		internalErr(w, "save avatar", err)
		return
	}
	s.writeSelf(w, r, u.ID)
}

func (s *Server) deleteAvatar(w http.ResponseWriter, r *http.Request, u *user) {
	if _, err := s.db.ExecContext(r.Context(), `DELETE FROM avatars WHERE user_id = ?`, u.ID); err != nil {
		internalErr(w, "delete avatar", err)
		return
	}
	s.writeSelf(w, r, u.ID)
}

func (s *Server) getAvatar(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var data []byte
	var updated int64
	err := s.db.QueryRowContext(r.Context(), `SELECT data, updated_at FROM avatars WHERE user_id = ?`, id).Scan(&data, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		internalErr(w, "load avatar", err)
		return
	}
	h := w.Header()
	etag := fmt.Sprintf(`"av%d-%d"`, id, updated)
	h.Set("ETag", etag)
	h.Set("X-Content-Type-Options", "nosniff")
	// A versioned URL never changes content (a new upload gets a new ?v=); anything else
	// must revalidate.
	if r.URL.Query().Get("v") == strconv.FormatInt(updated, 10) {
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		h.Set("Cache-Control", "no-cache")
	}
	if etagMatch(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set("Content-Type", "image/jpeg")
	h.Set("Content-Length", strconv.Itoa(len(data)))
	if r.Method != http.MethodHead {
		w.Write(data)
	}
}

func etagMatch(header, etag string) bool {
	for _, t := range bytes.Split([]byte(header), []byte(",")) {
		t = bytes.TrimPrefix(bytes.TrimSpace(t), []byte("W/"))
		if string(t) == etag || string(t) == "*" {
			return true
		}
	}
	return false
}
