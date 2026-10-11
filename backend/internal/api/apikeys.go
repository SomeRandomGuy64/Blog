package api

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"fazon.nexus/backend/internal/db"
)

// Personal API keys let a user script their own Track data. They are a separate credential
// with a deliberately small reach:
//
//   - Keys are managed (GET/POST /v1/me/api-keys, DELETE /v1/me/api-keys/{id}) with the normal
//     session cookie and Origin/CSRF rules, never with a key.
//   - Keys work only on /v1/api/…, and /v1/api/… accepts only keys (Authorization: Bearer
//     fz_…), never the session cookie. With no ambient credential, a cross-site page can't make
//     a request there on anyone's behalf, so guard exempts /v1/api/ from the Origin and
//     Content-Type rules (scripts send no Origin) and sends no CORS headers for it, so no other
//     website can read its answers either.
//   - A key is "fz_" + 32 random bytes in base64url; only its SHA-256 is stored (plus "fz_" and
//     the first 4 characters, to tell keys apart). Revoking deletes the row.
const (
	apiKeyPrefix   = "fz_"
	maxAPIKeys     = 5           // per user
	keyUsedRefresh = time.Minute // last_used_at is written at most this often per key
	apiKeysPath    = "/v1/me/api-keys"
	keyAPIPrefix   = "/v1/api/"
	maxRangeWeeks  = 260 // GET /v1/api/track/weeks
	defRangeWeeks  = 12  // its default: the last 12 weeks through the current one
	apiScopeRead   = "read"
	apiScopeWrite  = "write"
	keyHeaderHint  = "send your API key as Authorization: Bearer fz_…"
)

// isKeyAPI reports whether the request is for the key-only /v1/api/ namespace. The path is
// cleaned first, so "/v1/api/../me/…" doesn't count (the mux would redirect it anyway).
func isKeyAPI(r *http.Request) bool {
	p := path.Clean(r.URL.Path)
	return p == strings.TrimSuffix(keyAPIPrefix, "/") || strings.HasPrefix(p, keyAPIPrefix)
}

// newAPIKey returns a fresh key, its display prefix and its stored hash.
func newAPIKey() (key, prefix string, hash []byte) {
	tok, _ := db.NewSecret()
	key = apiKeyPrefix + tok
	return key, key[:len(apiKeyPrefix)+4], db.HashSecret(key)
}

type apiKeyJSON struct {
	ID       int64   `json:"id"`
	Name     string  `json:"name"`
	Scope    string  `json:"scope"`  // "read" or "write"
	Prefix   string  `json:"prefix"` // "fz_ab12"
	Created  string  `json:"created"`
	LastUsed *string `json:"last_used"`
}

func scanAPIKey(row interface{ Scan(...any) error }) (apiKeyJSON, error) {
	var k apiKeyJSON
	var created int64
	var used sql.NullInt64
	if err := row.Scan(&k.ID, &k.Name, &k.Scope, &k.Prefix, &created, &used); err != nil {
		return k, err
	}
	k.Created = rfc3339(created)
	if used.Valid {
		u := rfc3339(used.Int64)
		k.LastUsed = &u
	}
	return k, nil
}

// apiKeys lists a user's keys (metadata only), oldest first.
func (s *Server) apiKeys(ctx context.Context, userID int64) ([]apiKeyJSON, error) {
	keys := []apiKeyJSON{}
	err := s.each(ctx, func(rows *sql.Rows) error {
		k, err := scanAPIKey(rows)
		keys = append(keys, k)
		return err
	}, `SELECT id, name, scope, prefix, created_at, last_used_at FROM api_keys WHERE user_id = ? ORDER BY id`, userID)
	return keys, err
}

func (s *Server) listAPIKeys(w http.ResponseWriter, r *http.Request, u *user) {
	keys, err := s.apiKeys(r.Context(), u.ID)
	if err != nil {
		internalErr(w, "list api keys", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"keys": keys, "max": maxAPIKeys})
}

// createAPIKey (POST /v1/me/api-keys {name, scope}) answers 201 {"key":"fz_…","api_key":{…}}.
// The full key is in this response only.
func (s *Server) createAPIKey(w http.ResponseWriter, r *http.Request, u *user) {
	var in struct {
		Name  string `json:"name"`
		Scope string `json:"scope"`
	}
	if !decode(w, r, maxBody, &in) {
		return
	}
	name, err := db.NormalizeName(in.Name)
	if err != nil {
		writeErr(w, http.StatusBadRequest, strings.Replace(err.Error(), "name", "key name", 1))
		return
	}
	if in.Scope != apiScopeRead && in.Scope != apiScopeWrite {
		writeErr(w, http.StatusBadRequest, `scope must be "read" or "write"`)
		return
	}
	if !s.keyMgmtLimit.allow(strconv.FormatInt(u.ID, 10), s.now()) {
		writeErr(w, http.StatusTooManyRequests, "too many changes, slow down")
		return
	}
	key, prefix, hash := newAPIKey()
	now := s.now().Unix()
	var out apiKeyJSON
	err = s.tx(r.Context(), func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM api_keys WHERE user_id = ?`, u.ID).Scan(&n); err != nil {
			return err
		}
		if n >= maxAPIKeys {
			return errStatus{http.StatusConflict, "you can have at most 5 API keys; revoke one first"}
		}
		var err error
		out, err = scanAPIKey(tx.QueryRow(`INSERT INTO api_keys (user_id, name, prefix, hash, scope, created_at)
			VALUES (?, ?, ?, ?, ?, ?) RETURNING id, name, scope, prefix, created_at, last_used_at`,
			u.ID, name, prefix, hash, in.Scope, now))
		return err
	})
	if err != nil {
		writeTxErr(w, "create api key", err)
		return
	}
	slog.Info("api key created", "user", u.ID, "key", out.ID, "scope", out.Scope)
	writeJSON(w, http.StatusCreated, map[string]any{"key": key, "api_key": out})
}

func (s *Server) revokeAPIKey(w http.ResponseWriter, r *http.Request, u *user) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	res, err := s.db.ExecContext(r.Context(), `DELETE FROM api_keys WHERE id = ? AND user_id = ?`, id, u.ID)
	if err != nil {
		internalErr(w, "revoke api key", err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	slog.Info("api key revoked", "user", u.ID, "key", id)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// bearerKey extracts an API key from "Authorization: Bearer fz_…"; "" if there is none or it
// can't be one of ours.
func bearerKey(r *http.Request) string {
	scheme, tok, ok := strings.Cut(strings.TrimSpace(r.Header.Get("Authorization")), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	tok = strings.TrimSpace(tok)
	if !strings.HasPrefix(tok, apiKeyPrefix) || len(tok) > 128 {
		return ""
	}
	return tok
}

// keyAuthed wraps a /v1/api/ handler: it needs a valid API key (401), a write key for anything
// but GET/HEAD (403), and stays within the key's rate limit (429 with Retry-After). The session
// cookie is never looked at. The handler gets the key's owner (never with admin rights).
func (s *Server) keyAuthed(h func(http.ResponseWriter, *http.Request, *user)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		unauthorized := func(msg string) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="fazon.nexus"`)
			writeErr(w, http.StatusUnauthorized, msg)
		}
		if r.Header.Get("Authorization") == "" {
			unauthorized("API key required: " + keyHeaderHint)
			return
		}
		tok := bearerKey(r)
		if tok == "" {
			unauthorized("invalid API key: " + keyHeaderHint)
			return
		}
		hash := db.HashSecret(tok)
		var (
			id, uid  int64
			scope    string
			stored   []byte
			lastUsed sql.NullInt64
			u        user
		)
		// The lookup is by the key's SHA-256, so its timing says nothing useful about the key;
		// the constant-time compare after it is belt and braces.
		err := s.db.QueryRowContext(r.Context(), `SELECT k.id, k.hash, k.scope, k.last_used_at, u.id, u.email, u.name
			FROM api_keys k JOIN users u ON u.id = k.user_id WHERE k.hash = ?`, hash).
			Scan(&id, &stored, &scope, &lastUsed, &uid, &u.Email, &u.Name)
		if errors.Is(err, sql.ErrNoRows) || err == nil && subtle.ConstantTimeCompare(stored, hash) != 1 {
			unauthorized("invalid or revoked API key")
			return
		}
		if err != nil {
			internalErr(w, "load api key", err)
			return
		}
		u.ID = uid
		write := r.Method != http.MethodGet && r.Method != http.MethodHead
		if write && scope != apiScopeWrite {
			writeErr(w, http.StatusForbidden, "this API key is read-only; create a read & write key to make changes")
			return
		}
		lim, what := s.keyReadLimit, "reads"
		if write {
			lim, what = s.keyWriteLimit, "writes"
		}
		now := s.now()
		if ok, wait := lim.allowWait(strconv.FormatInt(id, 10), now); !ok {
			secs := int(math.Ceil(wait.Seconds()))
			if secs < 1 {
				secs = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(secs))
			writeErr(w, http.StatusTooManyRequests, "rate limit exceeded for this key's "+what+"; retry in "+strconv.Itoa(secs)+" s")
			return
		}
		if !lastUsed.Valid || now.Sub(time.Unix(lastUsed.Int64, 0)) >= keyUsedRefresh {
			if _, err := s.db.ExecContext(r.Context(), `UPDATE api_keys SET last_used_at = ? WHERE id = ?`, now.Unix(), id); err != nil {
				internalErr(w, "update api key", err)
				return
			}
		}
		slog.Debug("api key request", "key", id, "user", uid, "method", r.Method, "path", r.URL.Path)
		h(w, r, &u)
	}
}
