package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"fazon.nexus/backend/internal/db"
	"fazon.nexus/backend/internal/mail"
)

const (
	cookieName     = "fz_session"
	sessionTTL     = 90 * 24 * time.Hour
	sessionRefresh = 24 * time.Hour // slide the expiry at most this often
	LoginTokenTTL  = 15 * time.Minute
)

// user is the logged-in requester.
type user struct {
	ID    int64
	Email string
	Name  string
	Role  string // db.RoleUser, db.RoleModerator or db.RoleAdmin
	Admin bool   // Role == db.RoleAdmin
	Muted bool   // can't post comments
	// Hyojungo: may use the trainer's Tokyo mode (api/hyojungo.go) — admins, and accounts an
	// admin gave it to (users.hyojungo).
	Hyojungo bool
}

// userCols / scan load a user row into a user (keep the two in step).
const userCols = `u.id, u.email, u.name, u.role, u.muted_at IS NOT NULL, u.hyojungo`

func (u *user) scan(row interface{ Scan(...any) error }, extra ...any) error {
	err := row.Scan(append([]any{&u.ID, &u.Email, &u.Name, &u.Role, &u.Muted, &u.Hyojungo}, extra...)...)
	u.Admin = u.Role == db.RoleAdmin
	u.Hyojungo = u.Hyojungo || u.Admin
	return err
}

// moderator reports whether u may moderate: moderators and admins.
func (u *user) moderator() bool { return u.Role == db.RoleModerator || u.Role == db.RoleAdmin }

// loadUser loads the account id (sql.ErrNoRows if missing).
func (s *Server) loadUser(ctx context.Context, id int64) (*user, error) {
	var u user
	if err := u.scan(s.db.QueryRowContext(ctx, `SELECT `+userCols+` FROM users u WHERE u.id = ?`, id)); err != nil {
		return nil, err
	}
	return &u, nil
}

// SafeNext keeps only same-site relative paths ("/x"), never "//host" or "/\host", which
// browsers treat as protocol-relative URLs.
func SafeNext(next string) string {
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.HasPrefix(next, `/\`) ||
		len(next) > 1000 || strings.ContainsFunc(next, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return "/"
	}
	return next
}

// MagicLink is the email link: the token rides in the fragment so it never reaches a server
// log, and the site's verify page POSTs it to /v1/auth/verify.
func MagicLink(site, token, next string) string {
	return strings.TrimSuffix(site, "/") + "/account/verify/#t=" + token + "&next=" + url.QueryEscape(next)
}

func (s *Server) authRequest(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email string `json:"email"`
		Next  string `json:"next"`
	}
	if !decode(w, r, maxBody, &in) {
		return
	}
	email, err := db.NormalizeEmail(in.Email)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid email")
		return
	}
	accepted := func() { writeJSON(w, http.StatusAccepted, map[string]bool{"ok": true}) }
	now, ip := s.now(), clientIP(r)
	if !s.ipLimit.allow(ip, now) {
		slog.Warn("login request rate-limited", "ip", ip)
		accepted()
		return
	}
	if !s.emailLimit.allow(strings.ToLower(email), now) {
		slog.Warn("login request rate-limited", "email", email, "ip", ip)
		accepted()
		return
	}
	u, err := db.UserByEmail(r.Context(), s.db, email)
	if errors.Is(err, db.ErrNotFound) {
		slog.Info("login requested for unknown email", "email", email, "ip", ip)
		accepted()
		return
	}
	if err != nil {
		slog.Error("login request: user lookup", "err", err)
		accepted() // a 500 here would reveal that the account exists
		return
	}
	next := SafeNext(in.Next)
	tok, err := db.CreateLoginToken(r.Context(), s.db, u.ID, next, now, LoginTokenTTL)
	if err != nil {
		slog.Error("login request: create token", "err", err)
		accepted()
		return
	}
	s.sendAsync(LoginEmail(u.Email, u.Name, MagicLink(s.cfg.Site, tok, next)))
	accepted()
}

// LoginEmail is the magic-link message.
func LoginEmail(to, name, link string) mail.Message { return loginEmail(to, name, link, "") }

// loginEmail is the magic-link message with an optional first paragraph (plain text).
func loginEmail(to, name, link, intro string) mail.Message {
	var textIntro, htmlIntro string
	if intro != "" {
		textIntro, htmlIntro = intro+"\n\n", "<p>"+html.EscapeString(intro)+"</p>"
	}
	text := fmt.Sprintf("Hi %s,\n\n%sUse this link to sign in to Fazon Nexus:\n\n%s\n\n"+
		"It works once and expires in %d minutes. If you didn't ask for it, you can ignore this email.\n",
		name, textIntro, link, int(LoginTokenTTL.Minutes()))
	h := fmt.Sprintf(`<p>Hi %s,</p>%s<p><a href="%s">Sign in to Fazon Nexus</a></p>`+
		`<p>The link works once and expires in %d minutes. If you didn't ask for it, you can ignore this email.</p>`,
		html.EscapeString(name), htmlIntro, html.EscapeString(link), int(LoginTokenTTL.Minutes()))
	return mail.Message{To: to, Subject: "Your Fazon Nexus sign-in link", Text: text, HTML: h}
}

// sendAsync sends in the background so the request returns at once (and timing doesn't tell
// whether an account exists); failures are only logged.
func (s *Server) sendAsync(m mail.Message) {
	s.mailWG.Add(1)
	go func() {
		defer s.mailWG.Done()
		ctx, cancel := context.WithTimeout(context.Background(), s.cfg.MailTimeout)
		defer cancel()
		if err := s.cfg.Mailer.Send(ctx, m); err != nil {
			slog.Error("send email", "to", m.To, "subject", m.Subject, "err", err)
			return
		}
		slog.Info("email sent", "to", m.To, "subject", m.Subject)
	}()
}

func (s *Server) authVerify(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token string `json:"token"`
	}
	if !decode(w, r, maxBody, &in) {
		return
	}
	bad := func() { writeErr(w, http.StatusBadRequest, "invalid or expired link") }
	if in.Token == "" || len(in.Token) > 128 {
		bad()
		return
	}
	now := s.now()
	var userID int64
	var next string
	// One statement consumes the token, so two concurrent verifies can't both succeed.
	err := s.db.QueryRowContext(r.Context(),
		`UPDATE login_tokens SET used_at = ? WHERE hash = ? AND used_at IS NULL AND expires_at > ? RETURNING user_id, next`,
		now.Unix(), db.HashSecret(in.Token), now.Unix()).Scan(&userID, &next)
	if errors.Is(err, sql.ErrNoRows) {
		bad()
		return
	}
	if err != nil {
		internalErr(w, "verify token", err)
		return
	}
	self, ok := s.startSession(w, r, userID)
	if !ok {
		return
	}
	slog.Info("login", "user", userID, "ip", clientIP(r))
	writeJSON(w, http.StatusOK, map[string]any{"user": self, "next": SafeNext(next)})
}

// startSession creates a session for userID and sets its cookie; false after answering 500.
func (s *Server) startSession(w http.ResponseWriter, r *http.Request, userID int64) (selfUser, bool) {
	now := s.now()
	tok, hash := db.NewSecret()
	if _, err := s.db.ExecContext(r.Context(),
		`INSERT INTO sessions (hash, user_id, created_at, expires_at, last_seen) VALUES (?, ?, ?, ?, ?)`,
		hash, userID, now.Unix(), now.Add(sessionTTL).Unix(), now.Unix()); err != nil {
		internalErr(w, "create session", err)
		return selfUser{}, false
	}
	u, err := s.loadUser(r.Context(), userID)
	var self selfUser
	if err == nil {
		self, err = s.selfUser(r.Context(), u)
	}
	if err != nil {
		internalErr(w, "load user", err)
		return selfUser{}, false
	}
	s.setCookie(w, tok, int(sessionTTL.Seconds()))
	return self, true
}

func (s *Server) authLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil && c.Value != "" {
		if _, err := s.db.ExecContext(r.Context(), `DELETE FROM sessions WHERE hash = ?`, db.HashSecret(c.Value)); err != nil {
			internalErr(w, "delete session", err)
			return
		}
	}
	s.setCookie(w, "", -1)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	u, err := s.session(w, r)
	if err != nil {
		internalErr(w, "load session", err)
		return
	}
	if u == nil {
		writeJSON(w, http.StatusOK, map[string]any{"user": nil})
		return
	}
	unread, err := s.unread(r.Context(), u.ID)
	if err != nil {
		internalErr(w, "count unread", err)
		return
	}
	self, err := s.selfUser(r.Context(), u)
	if err != nil {
		internalErr(w, "load user", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": self, "unread": unread})
}

func (s *Server) setCookie(w http.ResponseWriter, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: value, Path: "/", MaxAge: maxAge,
		HttpOnly: true, Secure: s.cfg.CookieSecure, SameSite: http.SameSiteLaxMode})
}

// session returns the requester's user, or nil if not logged in. It slides the session's
// expiry (and re-sends the cookie) at most once per sessionRefresh.
func (s *Server) session(w http.ResponseWriter, r *http.Request) (*user, error) {
	c, err := r.Cookie(cookieName)
	if err != nil || c.Value == "" || len(c.Value) > 128 {
		return nil, nil
	}
	now := s.now()
	hash := db.HashSecret(c.Value)
	var u user
	var lastSeen int64
	err = u.scan(s.db.QueryRowContext(r.Context(), `
		SELECT `+userCols+`, s.last_seen
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.hash = ? AND s.expires_at > ?`, hash, now.Unix()), &lastSeen)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if now.Sub(time.Unix(lastSeen, 0)) >= sessionRefresh {
		if _, err := s.db.ExecContext(r.Context(), `UPDATE sessions SET last_seen = ?, expires_at = ? WHERE hash = ?`,
			now.Unix(), now.Add(sessionTTL).Unix(), hash); err != nil {
			return nil, err
		}
		s.setCookie(w, c.Value, int(sessionTTL.Seconds()))
	}
	return &u, nil
}

// authed wraps a handler that needs a logged-in user (401 otherwise).
func (s *Server) authed(h func(http.ResponseWriter, *http.Request, *user)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, err := s.session(w, r)
		if err != nil {
			internalErr(w, "load session", err)
			return
		}
		if u == nil {
			writeErr(w, http.StatusUnauthorized, "login required")
			return
		}
		h(w, r, u)
	}
}
