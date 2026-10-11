package api

import (
	"database/sql"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"fazon.nexus/backend/internal/db"
	"fazon.nexus/backend/internal/mail"
)

// Open sign-up. POST /v1/auth/signup {email, name, next} always answers 202 {"ok":true}, so it
// never tells whether an address has an account:
//
//   - a new address gets a confirmation link (a pending sign-up in signup_tokens: single use,
//     1 hour); no account exists until the link is opened;
//   - an address that already has an account gets an ordinary login link instead, with a line
//     saying so;
//   - a filled-in honeypot field ("website", hidden on the form) or a rate-limited request gets
//     nothing, silently (logged).
//
// It shares the login-link rate limits (per IP and per address), since both send email.
// POST /v1/auth/signup/confirm {token} consumes the link, creates the account (or, if the
// address got an account meanwhile, uses that one) and logs in like /v1/auth/verify.

// SignupLink is the confirmation link. Like the login link, the token rides in the fragment
// ("s" instead of "t") and the site's verify page POSTs it to /v1/auth/signup/confirm.
func SignupLink(site, token, next string) string {
	return strings.TrimSuffix(site, "/") + "/account/verify/#s=" + token + "&next=" + url.QueryEscape(next)
}

// SignupEmail asks a new address to confirm its sign-up.
func SignupEmail(to, name, link string) mail.Message {
	mins := int(db.SignupTokenTTL.Minutes())
	text := fmt.Sprintf("Hi %s,\n\nConfirm your email address to finish creating your Fazon Nexus account:\n\n%s\n\n"+
		"The link works once and expires in %d minutes. If you didn't sign up, you can ignore this email: "+
		"no account is created unless the link is opened.\n", name, link, mins)
	h := fmt.Sprintf(`<p>Hi %s,</p><p>Confirm your email address to finish creating your Fazon Nexus account:</p>`+
		`<p><a href="%s">Confirm and sign in</a></p>`+
		`<p>The link works once and expires in %d minutes. If you didn't sign up, you can ignore this email: `+
		`no account is created unless the link is opened.</p>`,
		html.EscapeString(name), html.EscapeString(link), mins)
	return mail.Message{To: to, Subject: "Confirm your Fazon Nexus account", Text: text, HTML: h}
}

// ExistingAccountEmail answers a sign-up for an address that already has an account.
func ExistingAccountEmail(to, name, link string) mail.Message {
	return loginEmail(to, name, link, "You already have an account. Someone (hopefully you) just tried to sign up "+
		"with this address, so here's a login link instead.")
}

func (s *Server) authSignup(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email   string `json:"email"`
		Name    string `json:"name"`
		Next    string `json:"next"`
		Website string `json:"website"` // honeypot: people never see the field, bots fill it in
	}
	if !decode(w, r, maxBody, &in) {
		return
	}
	accepted := func() { writeJSON(w, http.StatusAccepted, map[string]bool{"ok": true}) }
	now, ip := s.now(), clientIP(r)
	if in.Website != "" {
		slog.Warn("signup honeypot filled", "ip", ip)
		accepted()
		return
	}
	email, err := db.NormalizeEmail(in.Email)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid email")
		return
	}
	name, err := db.NormalizeName(in.Name)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.ipLimit.allow(ip, now) {
		slog.Warn("signup request rate-limited", "ip", ip)
		accepted()
		return
	}
	if !s.emailLimit.allow(strings.ToLower(email), now) {
		slog.Warn("signup request rate-limited", "email", email, "ip", ip)
		accepted()
		return
	}
	next := SafeNext(in.Next)
	u, err := db.UserByEmail(r.Context(), s.db, email)
	switch {
	case errors.Is(err, db.ErrNotFound):
		tok, err := db.CreateSignupToken(r.Context(), s.db, email, name, next, now)
		if err != nil {
			slog.Error("signup request: create token", "err", err)
			break
		}
		slog.Info("signup requested", "email", email, "ip", ip)
		s.sendAsync(SignupEmail(email, name, SignupLink(s.cfg.Site, tok, next)))
	case err != nil:
		slog.Error("signup request: user lookup", "err", err) // still 202: a 500 would tell
	default:
		tok, err := db.CreateLoginToken(r.Context(), s.db, u.ID, next, now, LoginTokenTTL)
		if err != nil {
			slog.Error("signup request: create login token", "err", err)
			break
		}
		slog.Info("signup requested for an existing account", "user", u.ID, "ip", ip)
		s.sendAsync(ExistingAccountEmail(u.Email, u.Name, MagicLink(s.cfg.Site, tok, next)))
	}
	accepted()
}

func (s *Server) authSignupConfirm(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token string `json:"token"`
	}
	if !decode(w, r, maxBody, &in) {
		return
	}
	if in.Token == "" || len(in.Token) > 128 {
		writeErr(w, http.StatusBadRequest, "invalid or expired link")
		return
	}
	now := s.now()
	var userID int64
	var next string
	created := false
	err := s.tx(r.Context(), func(tx *sql.Tx) error {
		var email, name string
		// One statement consumes the token, so two concurrent confirms can't both succeed.
		err := tx.QueryRow(`UPDATE signup_tokens SET used_at = ? WHERE hash = ? AND used_at IS NULL AND expires_at > ?
			RETURNING email, name, next`, now.Unix(), db.HashSecret(in.Token), now.Unix()).Scan(&email, &name, &next)
		if errors.Is(err, sql.ErrNoRows) {
			return errStatus{http.StatusBadRequest, "invalid or expired link"}
		}
		if err != nil {
			return err
		}
		err = tx.QueryRow(`INSERT INTO users (id, email, name, role, is_admin, created_at) VALUES (`+db.NextUserID+`, ?, ?, 'user', 0, ?)
			ON CONFLICT (email) DO NOTHING RETURNING id`, email, name, now.Unix()).Scan(&userID)
		switch {
		case errors.Is(err, sql.ErrNoRows): // the address got an account meanwhile: log into it
			if err := tx.QueryRow(`SELECT id FROM users WHERE email = ? AND id > 0`, email).Scan(&userID); err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			created = true
		}
		// Any other pending sign-up links for this address are spent too.
		_, err = tx.Exec(`DELETE FROM signup_tokens WHERE email = ? AND used_at IS NULL`, email)
		return err
	})
	if err != nil {
		writeTxErr(w, "confirm signup", err)
		return
	}
	self, ok := s.startSession(w, r, userID)
	if !ok {
		return
	}
	if created {
		slog.Info("signup confirmed: account created", "user", userID, "ip", clientIP(r))
	} else {
		slog.Info("signup confirmed for an existing account: login", "user", userID, "ip", clientIP(r))
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": self, "next": SafeNext(next), "created": created})
}
