package db

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// User is a row of users.
type User struct {
	ID      int64
	Email   string
	Name    string
	Role    string // RoleUser, RoleModerator or RoleAdmin
	Admin   bool   // Role == RoleAdmin
	Created time.Time
}

// Roles (users.role). users.is_admin is kept equal to role = 'admin' on every write, so an
// older binary still sees the right admins after a rollback (see migration 0007).
const (
	RoleUser      = "user"
	RoleModerator = "moderator"
	RoleAdmin     = "admin"
)

// ValidRole reports whether r is one of the roles.
func ValidRole(r string) bool { return r == RoleUser || r == RoleModerator || r == RoleAdmin }

// GhostID is the placeholder account (id 0, no email, never listed, can't log in) that keeps
// the "[deleted]" comments of a deleted account that others replied to, so their threads stay
// intact. It is created on first use by DeleteUser.
const GhostID = 0

var (
	ErrNotFound = errors.New("not found")
	ErrExists   = errors.New("already exists")
)

// NormalizeEmail trims and validates an address; it keeps case (lookups are COLLATE NOCASE).
func NormalizeEmail(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 254 {
		return "", errors.New("invalid email")
	}
	a, err := mail.ParseAddress(s)
	if err != nil || a.Address != s {
		return "", errors.New("invalid email")
	}
	return s, nil
}

// NormalizeName trims a display name and requires 1–40 runes with no control characters and
// no bidi embedding/override/isolate controls (an unclosed U+202E in a name reverses the text
// around it wherever the name is shown inline, e.g. "X liked your comment on …").
func NormalizeName(s string) (string, error) {
	s = strings.TrimSpace(s)
	if n := utf8.RuneCountInString(s); n < 1 || n > 40 {
		return "", errors.New("name must be 1-40 characters")
	}
	if !utf8.ValidString(s) || strings.ContainsFunc(s, unicode.IsControl) || strings.ContainsFunc(s, IsBidiControl) {
		return "", errors.New("name must not contain control characters")
	}
	return s, nil
}

// IsBidiControl reports the explicit bidi formatting characters: LRE/RLE/PDF/LRO/RLO
// (U+202A–U+202E) and LRI/RLI/FSI/PDI (U+2066–U+2069).
func IsBidiControl(r rune) bool {
	return r >= 0x202A && r <= 0x202E || r >= 0x2066 && r <= 0x2069
}

func scanUser(row interface{ Scan(...any) error }) (User, error) {
	var u User
	var created int64
	err := row.Scan(&u.ID, &u.Email, &u.Name, &u.Role, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	u.Admin = u.Role == RoleAdmin
	u.Created = time.Unix(created, 0).UTC()
	return u, err
}

const userCols = "id, email, name, role, created_at"

// UserByEmail looks a user up case-insensitively.
func UserByEmail(ctx context.Context, db *sql.DB, email string) (User, error) {
	return scanUser(db.QueryRowContext(ctx, "SELECT "+userCols+" FROM users WHERE email = ? AND id > 0", strings.TrimSpace(email)))
}

// ListUsers returns every user, oldest first.
func ListUsers(ctx context.Context, db *sql.DB) ([]User, error) {
	rows, err := db.QueryContext(ctx, "SELECT "+userCols+" FROM users WHERE id > 0 ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// AddUser creates a user; ErrExists if the email is taken.
func AddUser(ctx context.Context, db *sql.DB, email, name string, admin bool) (User, error) {
	email, err := NormalizeEmail(email)
	if err != nil {
		return User{}, err
	}
	if name, err = NormalizeName(name); err != nil {
		return User{}, err
	}
	role := RoleUser
	if admin {
		role = RoleAdmin
	}
	u, err := scanUser(db.QueryRowContext(ctx,
		"INSERT INTO users (id, email, name, role, is_admin, created_at) VALUES ("+NextUserID+", ?, ?, ?, ?, ?) ON CONFLICT (email) DO NOTHING RETURNING "+userCols,
		email, name, role, admin, time.Now().Unix()))
	if errors.Is(err, ErrNotFound) {
		return User{}, fmt.Errorf("%s: %w", email, ErrExists)
	}
	return u, err
}

// EnsureAdmin creates the admin account if missing, or makes sure the existing account is an
// admin. It never renames an existing account.
func EnsureAdmin(ctx context.Context, db *sql.DB, email, name string) error {
	email, err := NormalizeEmail(email)
	if err != nil {
		return err
	}
	if name, err = NormalizeName(name); err != nil {
		return err
	}
	_, err = db.ExecContext(ctx,
		"INSERT INTO users (id, email, name, role, is_admin, created_at) VALUES ("+NextUserID+", ?, ?, 'admin', 1, ?) "+
			"ON CONFLICT (email) DO UPDATE SET role = 'admin', is_admin = 1",
		email, name, time.Now().Unix())
	return err
}

// NextUserID is the SQL for a new account's id: one past both the highest live id and the highest
// id ever deleted (migration 0008), so a deleted account's id is never given to someone else.
const NextUserID = `(SELECT MAX(COALESCE((SELECT MAX(id) FROM users), 0),
	COALESCE((SELECT CAST(value AS INTEGER) FROM settings WHERE key = 'users_max_id'), 0)) + 1)`

// NewSecret returns 32 random bytes as base64url (the token handed out) and its sha256 (stored).
func NewSecret() (token string, hash []byte) {
	b := make([]byte, 32)
	rand.Read(b) // never fails (crypto/rand panics instead on unrecoverable errors)
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, HashSecret(token)
}

// HashSecret is the stored form of a login token or session cookie.
func HashSecret(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

// CreateLoginToken stores a single-use login token for userID, valid from now for ttl, and
// returns the raw token.
func CreateLoginToken(ctx context.Context, db *sql.DB, userID int64, next string, now time.Time, ttl time.Duration) (string, error) {
	tok, hash := NewSecret()
	_, err := db.ExecContext(ctx,
		"INSERT INTO login_tokens (hash, user_id, next, created_at, expires_at) VALUES (?, ?, ?, ?, ?)",
		hash, userID, next, now.Unix(), now.Add(ttl).Unix())
	return tok, err
}

// SignupTokenTTL is how long a sign-up confirmation link works.
const SignupTokenTTL = time.Hour

// CreateSignupToken stores a single-use confirmation token for a pending sign-up (no account
// exists until it is confirmed) and returns the raw token.
func CreateSignupToken(ctx context.Context, db *sql.DB, email, name, next string, now time.Time) (string, error) {
	tok, hash := NewSecret()
	_, err := db.ExecContext(ctx,
		"INSERT INTO signup_tokens (hash, email, name, next, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?)",
		hash, email, name, next, now.Unix(), now.Add(SignupTokenTTL).Unix())
	return tok, err
}

// DeleteUser removes an account and everything that belongs to it, inside tx: sessions, login
// and sign-up tokens, API keys, avatar, Track data (weeks, hours, ratings, goals, sub-
// categories), Kyōtsu sessions, likes given (on comments and articles), notifications to or
// from them, and their comments.
// A comment that nothing replies to is removed outright (with its likes and notifications),
// leaf first, so a chain of their own replies goes too. A comment others replied to stays as a
// "[deleted]" placeholder, exactly like a comment deleted by its author (body cleared,
// deleted_at set, likes and notifications dropped), and is handed to the GhostID account so no
// row refers to the user any more. Mutes they gave stay, with muted_by cleared.
//
// Each table is cleared explicitly rather than left to ON DELETE CASCADE, so the order is plain
// and nothing depends on cascades; the foreign keys still catch anything missed (the final
// DELETE fails rather than orphan a row). ErrNotFound if there is no such user.
func DeleteUser(ctx context.Context, tx *sql.Tx, id int64, now time.Time) error {
	if id <= GhostID {
		return ErrNotFound
	}
	var email string
	err := tx.QueryRowContext(ctx, `SELECT email FROM users WHERE id = ?`, id).Scan(&email)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	for { // comments without replies, leaf first
		res, err := tx.ExecContext(ctx, `DELETE FROM comments WHERE user_id = ?
			AND NOT EXISTS (SELECT 1 FROM comments k WHERE k.parent_id = comments.id)`, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			break
		}
	}
	var kept int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM comments WHERE user_id = ?`, id).Scan(&kept); err != nil {
		return err
	}
	if kept > 0 {
		for _, q := range []struct {
			sql  string
			args []any
		}{
			{`INSERT OR IGNORE INTO users (id, email, name, role, is_admin, track_public, created_at)
				VALUES (0, '', '[deleted]', 'user', 0, 0, 0)`, nil},
			{`DELETE FROM notifications WHERE comment_id IN (SELECT id FROM comments WHERE user_id = ?)`, []any{id}},
			{`DELETE FROM comment_likes WHERE comment_id IN (SELECT id FROM comments WHERE user_id = ?)`, []any{id}},
			{`UPDATE comments SET user_id = 0, body = '', deleted_at = IFNULL(deleted_at, ?) WHERE user_id = ?`, []any{now.Unix(), id}},
		} {
			if _, err := tx.ExecContext(ctx, q.sql, q.args...); err != nil {
				return fmt.Errorf("delete user %d: %s: %w", id, q.sql, err)
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM signup_tokens WHERE email = ?`, email); err != nil {
		return err
	}
	for _, q := range []string{
		`DELETE FROM sessions WHERE user_id = ?`,
		`DELETE FROM login_tokens WHERE user_id = ?`,
		`DELETE FROM api_keys WHERE user_id = ?`,
		`DELETE FROM avatars WHERE user_id = ?`,
		`DELETE FROM comment_likes WHERE user_id = ?`,
		`DELETE FROM page_likes WHERE user_id = ?`,
		`DELETE FROM notifications WHERE user_id = ?1 OR actor_id = ?1`,
		`DELETE FROM kyotsu_sessions WHERE user_id = ?`,
		`DELETE FROM watch_sessions WHERE user_id = ?`,
		`DELETE FROM track_hours WHERE user_id = ?`,
		`DELETE FROM track_ratings WHERE user_id = ?`,
		`DELETE FROM track_weeks WHERE user_id = ?`,
		`DELETE FROM track_goals WHERE user_id = ?`,
		`DELETE FROM track_week_subcats WHERE user_id = ?`,
		`DELETE FROM track_subcats WHERE user_id = ?`,
		`UPDATE users SET muted_by = NULL WHERE muted_by = ?`,
		`DELETE FROM users WHERE id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, q, id); err != nil {
			return fmt.Errorf("delete user %d: %s: %w", id, q, err)
		}
	}
	return nil
}
