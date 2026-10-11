package api

import (
	"database/sql"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
)

// Tokyo (標準語) mode of the Kyōtsu trainer: Kotu's minimal pairs (kotu.io), which are NHK accent
// dictionary recordings. They aren't ours to publish, so they never go on the static site: they
// live in Config.HyojungoDir (built by deploy/hyojungo_build.py — index.json + audio/*.aac) and
// are served only to accounts with access: admins, and anyone an admin gave it to.

var hyojungoAudioRe = regexp.MustCompile(`^[0-9a-z]{1,8}-[0-9]\.aac$`)

// hyojungoAccess wraps a handler for accounts with Tokyo-mode access (401 / 403 otherwise).
func (s *Server) hyojungoAccess(h func(http.ResponseWriter, *http.Request, *user)) http.HandlerFunc {
	return s.authed(func(w http.ResponseWriter, r *http.Request, u *user) {
		if !u.Hyojungo {
			writeErr(w, http.StatusForbidden, "Tokyo mode isn't enabled for your account")
			return
		}
		h(w, r, u)
	})
}

// GET /v1/hyojungo/pairs → index.json as built.
func (s *Server) hyojungoPairs(w http.ResponseWriter, r *http.Request, _ *user) {
	s.hyojungoFile(w, r, "index.json", "application/json", 3600)
}

// GET /v1/hyojungo/audio/{name} → one clip, e.g. 0-1.aac.
func (s *Server) hyojungoAudio(w http.ResponseWriter, r *http.Request, _ *user) {
	name := r.PathValue("name")
	if !hyojungoAudioRe.MatchString(name) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	s.hyojungoFile(w, r, filepath.Join("audio", name), "audio/aac", 30*24*3600)
}

// hyojungoFile serves name from HyojungoDir (ranges and conditional requests included), cached
// by the browser only: the files never change for a name until the data is rebuilt.
func (s *Server) hyojungoFile(w http.ResponseWriter, r *http.Request, name, ctype string, maxAge int) {
	if s.cfg.HyojungoDir == "" {
		writeErr(w, http.StatusNotFound, "Tokyo mode isn't set up on this server")
		return
	}
	f, err := os.Open(filepath.Join(s.cfg.HyojungoDir, name))
	if errors.Is(err, fs.ErrNotExist) {
		msg := "not found"
		if name == "index.json" {
			msg = "Tokyo mode isn't set up on this server"
		}
		writeErr(w, http.StatusNotFound, msg)
		return
	}
	if err != nil {
		internalErr(w, "open hyojungo file", err)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		internalErr(w, "stat hyojungo file", err)
		return
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Cache-Control", "private, max-age="+strconv.Itoa(maxAge))
	http.ServeContent(w, r, "", st.ModTime(), f)
}

// adminSetHyojungo: POST /v1/admin/users/{id}/hyojungo {"on": bool} (admins only). Admins
// always have Tokyo mode, so their own flag can't be changed.
func (s *Server) adminSetHyojungo(w http.ResponseWriter, r *http.Request, u *user) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		On *bool `json:"on"`
	}
	if !decode(w, r, maxBody, &in) {
		return
	}
	if in.On == nil {
		writeErr(w, http.StatusBadRequest, "on must be true or false")
		return
	}
	if !u.Admin {
		writeErr(w, http.StatusForbidden, "only admins can change Tokyo-mode access")
		return
	}
	err := s.tx(r.Context(), func(tx *sql.Tx) error {
		var role string
		err := tx.QueryRowContext(r.Context(), `SELECT role FROM users WHERE id = ? AND id > 0`, id).Scan(&role)
		if errors.Is(err, sql.ErrNoRows) {
			return errStatus{http.StatusNotFound, "not found"}
		}
		if err != nil {
			return err
		}
		if !rightsOver(u, id, role, 0).Hyojungo {
			return errStatus{http.StatusConflict, "admins always have Tokyo mode"}
		}
		_, err = tx.Exec(`UPDATE users SET hyojungo = ? WHERE id = ?`, *in.On, id)
		return err
	})
	if err != nil {
		writeTxErr(w, "admin: set hyojungo", err)
		return
	}
	slog.Info("admin action", "actor", u.ID, "action", "hyojungo", "target", id, "on", *in.On)
	s.writeAdminUser(w, r, u, id)
}
