// Package api is the fazon.nexus HTTP API: magic-link accounts with open sign-up, roles and
// moderation, article comments with likes and notifications, article likes, Kyōtsu trainer
// sessions, video watch time, Track and personal API keys.
package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"fazon.nexus/backend/internal/mail"
)

const (
	maxBody       = 64 << 10
	maxKyotsuBody = 1 << 20
)

// Config is what the API needs from the command line / environment.
type Config struct {
	Site         string   // base URL of the static site, for magic links
	Origins      []string // allowed CORS/CSRF origins; "host:*" = any port
	CookieSecure bool
	Mailer       mail.Mailer
	MailTimeout  time.Duration // per email; 0 = 1 minute
	// Pages fetches the site's comment-pages index; nil = HTTPPageFetcher(Site).
	Pages PageFetcher
	// AuthorEmail is the account that writes every article (FAZOND_ADMIN_EMAIL): article likes
	// are its XP and notify it. "" = no author (article likes count but give nobody XP).
	AuthorEmail string
	// HyojungoDir holds the trainer's Tokyo-mode data (index.json, audio/; see hyojungo.go).
	// "" or missing = Tokyo mode not set up (its endpoints answer 404).
	HyojungoDir string
}

type Server struct {
	db  *sql.DB
	cfg Config
	now func() time.Time // swapped in tests

	ipLimit, emailLimit    *limiter // login-link requests
	commentLimit, kyoLimit *limiter // per user
	watchLimit             *limiter // per user: watch-session heartbeats
	pageLikeLimit          *limiter // per user: article like/unlike
	avatarLimit, nameLimit *limiter // per user
	weekLimit, trackLimit  *limiter // per user: week saves; subcat/settings changes
	keyMgmtLimit           *limiter // per user: API key creation
	exportLimit            *limiter // per user: data exports
	keyReadLimit           *limiter // per API key: /v1/api/ reads
	keyWriteLimit          *limiter // per API key: /v1/api/ writes

	pages pageIndex // commentable pages and their titles, from the site

	mailWG sync.WaitGroup
}

func New(db *sql.DB, cfg Config) *Server {
	if cfg.MailTimeout == 0 {
		cfg.MailTimeout = time.Minute
	}
	if cfg.Pages == nil && cfg.Site != "" {
		cfg.Pages = HTTPPageFetcher(cfg.Site)
	}
	return &Server{
		db: db, cfg: cfg, now: time.Now,
		pages:        pageIndex{fetch: cfg.Pages},
		ipLimit:      newLimiter(10, time.Hour),
		emailLimit:   newLimiter(3, 15*time.Minute),
		commentLimit: newLimiter(30, 10*time.Minute),
		kyoLimit:     newLimiter(60, time.Hour),
		// a heartbeat a minute per playing video plus one per pause: room for several tabs
		watchLimit:   newLimiter(300, time.Hour),
		avatarLimit:  newLimiter(20, time.Hour),
		nameLimit:    newLimiter(20, time.Hour),
		weekLimit:    newLimiter(120, time.Hour),
		trackLimit:   newLimiter(120, time.Hour),
		keyMgmtLimit: newLimiter(20, time.Hour),
		exportLimit:  newLimiter(10, time.Hour),
		// per user: article like/unlike, generous for toggling but not a notification fire hose
		pageLikeLimit: newLimiter(60, 10*time.Minute),
		// per key; 429 with Retry-After (see keyAuthed)
		keyReadLimit:  newLimiter(120, time.Minute),
		keyWriteLimit: newLimiter(30, time.Minute),
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("POST /v1/auth/request", s.authRequest)
	mux.HandleFunc("POST /v1/auth/verify", s.authVerify)
	mux.HandleFunc("POST /v1/auth/logout", s.authLogout)
	mux.HandleFunc("POST /v1/auth/signup", s.authSignup)
	mux.HandleFunc("POST /v1/auth/signup/confirm", s.authSignupConfirm)
	mux.HandleFunc("GET /v1/me", s.me)
	mux.HandleFunc("DELETE /v1/me", s.authed(s.deleteMe))
	mux.HandleFunc("GET /v1/me/export", s.authed(s.exportMe))

	// The admin page (moderators and admins; see admin.go).
	mux.HandleFunc("GET /v1/admin/users", s.staff(s.listAdminUsers))
	mux.HandleFunc("POST /v1/admin/users/{id}/role", s.staff(s.adminSetRole))
	mux.HandleFunc("POST /v1/admin/users/{id}/mute", s.staff(s.adminMute))
	mux.HandleFunc("POST /v1/admin/users/{id}/unmute", s.staff(s.adminUnmute))
	mux.HandleFunc("DELETE /v1/admin/users/{id}", s.staff(s.adminDeleteUser))
	mux.HandleFunc("POST /v1/admin/users/{id}/hyojungo", s.staff(s.adminSetHyojungo))

	mux.HandleFunc("GET /v1/comments", s.listComments)
	mux.HandleFunc("POST /v1/comments", s.authed(s.postComment))
	mux.HandleFunc("DELETE /v1/comments/{id}", s.authed(s.deleteComment))
	mux.HandleFunc("POST /v1/comments/{id}/like", s.authed(s.likeComment))
	mux.HandleFunc("DELETE /v1/comments/{id}/like", s.authed(s.unlikeComment))

	mux.HandleFunc("GET /v1/pages/likes", s.pageLikes)
	mux.HandleFunc("POST /v1/pages/likes", s.authed(s.likePage))
	mux.HandleFunc("DELETE /v1/pages/likes", s.authed(s.unlikePage))

	mux.HandleFunc("GET /v1/notifications", s.authed(s.listNotifications))
	mux.HandleFunc("POST /v1/notifications/read", s.authed(s.readNotifications))

	mux.HandleFunc("POST /v1/kyotsu/sessions", s.authed(s.postKyotsu))
	mux.HandleFunc("GET /v1/kyotsu/sessions", s.authed(s.listKyotsu))
	mux.HandleFunc("GET /v1/kyotsu/sessions/{id}", s.authed(s.getKyotsu))
	mux.HandleFunc("POST /v1/watch/sessions", s.authed(s.postWatch))
	mux.HandleFunc("GET /v1/hyojungo/pairs", s.hyojungoAccess(s.hyojungoPairs))
	mux.HandleFunc("GET /v1/hyojungo/audio/{name}", s.hyojungoAccess(s.hyojungoAudio))

	mux.HandleFunc("GET /v1/levels", s.levels)
	mux.HandleFunc("GET /v1/users/{id}", s.profile)
	mux.HandleFunc("GET /v1/users/{id}/avatar", s.getAvatar)
	mux.HandleFunc("POST "+avatarUploadPath, s.authed(s.putAvatar))
	mux.HandleFunc("DELETE "+avatarUploadPath, s.authed(s.deleteAvatar))
	mux.HandleFunc("POST /v1/me/profile", s.authed(s.setProfile))

	mux.HandleFunc("GET /v1/track/config", s.authed(s.trackConfig))
	mux.HandleFunc("POST /v1/track/settings", s.authed(s.trackSettings))
	mux.HandleFunc("POST /v1/track/subcats", s.authed(s.createSubcat))
	mux.HandleFunc("POST /v1/track/subcats/{id}", s.authed(s.updateSubcat))
	mux.HandleFunc("DELETE /v1/track/subcats/{id}", s.authed(s.deleteSubcat))
	mux.HandleFunc("GET /v1/track/weeks", s.authed(s.listWeeks))
	mux.HandleFunc("GET /v1/track/weeks/{key}", s.authed(s.getWeek))
	mux.HandleFunc("POST /v1/track/weeks/{key}", s.authed(s.saveWeek))
	mux.HandleFunc("POST /v1/track/weeks/{key}/goals", s.authed(s.setGoals))
	mux.HandleFunc("POST /v1/track/admin/end-week", s.authed(s.endWeek))
	mux.HandleFunc("DELETE /v1/track/admin/end-week", s.authed(s.reopenWeek))
	mux.HandleFunc("GET /v1/track/leaderboard", s.leaderboard)
	mux.HandleFunc("GET /v1/track/leaderboard/goals", s.goalAchievers)
	mux.HandleFunc("GET /v1/track/summaries", s.summaries)
	mux.HandleFunc("GET /v1/users/{id}/track", s.userTrack)
	mux.HandleFunc("GET /v1/users/{id}/card", s.card)

	// Personal API keys: managed with the session cookie, used (only) on /v1/api/.
	mux.HandleFunc("GET "+apiKeysPath, s.authed(s.listAPIKeys))
	mux.HandleFunc("POST "+apiKeysPath, s.authed(s.createAPIKey))
	mux.HandleFunc("DELETE "+apiKeysPath+"/{id}", s.authed(s.revokeAPIKey))
	mux.HandleFunc("GET /v1/api/track/weeks", s.keyAuthed(s.apiListWeeks))
	mux.HandleFunc("GET /v1/api/track/weeks/{key}", s.keyAuthed(s.apiGetWeek))
	mux.HandleFunc("PATCH /v1/api/track/weeks/{key}/hours", s.keyAuthed(s.apiPatchHours))
	mux.HandleFunc("PUT /v1/api/track/weeks/{key}/summary", s.keyAuthed(s.apiPutSummary))
	mux.HandleFunc("POST /v1/api/track/weeks/{key}/goals", s.keyAuthed(s.apiSetGoals))
	mux.HandleFunc("GET /v1/api/track/subcats", s.keyAuthed(s.apiListSubcats))
	mux.HandleFunc("POST /v1/api/track/subcats", s.keyAuthed(s.apiCreateSubcat))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotFound, "not found") })
	return s.guard(mux)
}

// Wait blocks until in-flight emails are sent (or time out); call it on shutdown.
func (s *Server) Wait() { s.mailWG.Wait() }

// Sweep forgets rate-limit entries whose windows have passed.
func (s *Server) Sweep() {
	now := s.now()
	for _, l := range []*limiter{s.ipLimit, s.emailLimit, s.commentLimit, s.pageLikeLimit, s.kyoLimit, s.watchLimit, s.avatarLimit, s.nameLimit, s.weekLimit, s.trackLimit,
		s.keyMgmtLimit, s.keyReadLimit, s.keyWriteLimit, s.exportLimit} {
		l.sweep(now)
	}
}

// guard applies CORS to everything, answers /v1/ preflights, and enforces the CSRF rules:
// state-changing requests need an allowed Origin and, with a body, a JSON content type (a
// cross-site form can't send either, and a JSON fetch from elsewhere needs a preflight we refuse).
//
// The key API (/v1/api/…) is the exception: it ignores cookies and takes only an API key in the
// Authorization header, which a cross-site page can't attach for someone else, so there is
// nothing to forge — it needs no Origin (scripts send none) and takes any content type. It gets
// no CORS headers at all and its preflights are refused, so browsers on other sites can neither
// send it keys nor read its answers.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		allowed := origin != "" && originAllowed(s.cfg.Origins, origin)
		w.Header().Add("Vary", "Origin")
		// Defence in depth: no response (JSON, avatar, error) is ever sniffed or rendered as a
		// page that could run script, even if opened directly.
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; sandbox")
		if isKeyAPI(r) {
			if r.Method == http.MethodOptions {
				writeErr(w, http.StatusForbidden, "the /v1/api/ endpoints are for scripts and can't be called from web pages")
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		if allowed {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		switch r.Method {
		case http.MethodOptions:
			if strings.HasPrefix(r.URL.Path, "/v1/") {
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
				w.Header().Set("Access-Control-Max-Age", "86400")
				w.WriteHeader(http.StatusNoContent)
				return
			}
		case http.MethodGet, http.MethodHead:
		default:
			if !allowed {
				writeErr(w, http.StatusForbidden, "origin not allowed")
				return
			}
			if r.ContentLength != 0 {
				mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
				// The avatar upload is the one raw-body route. image/* isn't a CORS-safelisted
				// type either, so cross-site pages still can't send it without our preflight.
				if r.Method == http.MethodPost && r.URL.Path == avatarUploadPath {
					if !avatarTypes[mt] {
						writeErr(w, http.StatusUnsupportedMediaType, "content type must be image/jpeg or image/png")
						return
					}
				} else if mt != "application/json" {
					writeErr(w, http.StatusUnsupportedMediaType, "content type must be application/json")
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// originAllowed matches an Origin against the configured list; an entry ending in ":*"
// ("http://127.0.0.1:*") allows any port.
func originAllowed(allowed []string, origin string) bool {
	for _, a := range allowed {
		if a == origin {
			return true
		}
		if base, ok := strings.CutSuffix(a, ":*"); ok {
			if port, ok := strings.CutPrefix(origin, base+":"); ok && port != "" && strings.Trim(port, "0123456789") == "" {
				return true
			}
		}
	}
	return false
}

// clientIP is the peer address, or the first X-Forwarded-For hop when the peer is loopback
// (i.e. Caddy, which replaces any client-sent X-Forwarded-For unless told to trust it).
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			first, _, _ := strings.Cut(xff, ",")
			if ip := net.ParseIP(strings.TrimSpace(first)); ip != nil {
				return ip.String()
			}
		}
	}
	return host
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if w.Header().Get("Cache-Control") == "" {
		w.Header().Set("Cache-Control", "no-store")
	}
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func internalErr(w http.ResponseWriter, what string, err error) {
	slog.Error(what, "err", err)
	writeErr(w, http.StatusInternalServerError, "internal error")
}

// decode reads a JSON body of at most limit bytes into v, answering 413/400 itself on failure.
func decode(w http.ResponseWriter, r *http.Request, limit int64, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeErr(w, http.StatusRequestEntityTooLarge, "request body too large")
		} else {
			writeErr(w, http.StatusBadRequest, "invalid JSON body")
		}
		return false
	}
	return true
}

// pathID parses the {id} path segment; false (after a 404) if it isn't a positive integer.
func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusNotFound, "not found")
		return 0, false
	}
	return id, true
}

func rfc3339(unix int64) string { return time.Unix(unix, 0).UTC().Format(time.RFC3339) }

// tx runs fn in a (write-locking, see db.Open) transaction.
func (s *Server) tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}
