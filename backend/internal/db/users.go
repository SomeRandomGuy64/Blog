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
	Admin   bool
	Created time.Time
}

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

// NormalizeName trims a display name and requires 1–40 runes with no control characters.
func NormalizeName(s string) (string, error) {
	s = strings.TrimSpace(s)
	if n := utf8.RuneCountInString(s); n < 1 || n > 40 {
		return "", errors.New("name must be 1-40 characters")
	}
	if !utf8.ValidString(s) || strings.ContainsFunc(s, unicode.IsControl) {
		return "", errors.New("name must not contain control characters")
	}
	return s, nil
}

func scanUser(row interface{ Scan(...any) error }) (User, error) {
	var u User
	var created int64
	err := row.Scan(&u.ID, &u.Email, &u.Name, &u.Admin, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	u.Created = time.Unix(created, 0).UTC()
	return u, err
}

const userCols = "id, email, name, is_admin, created_at"

// UserByEmail looks a user up case-insensitively.
func UserByEmail(ctx context.Context, db *sql.DB, email string) (User, error) {
	return scanUser(db.QueryRowContext(ctx, "SELECT "+userCols+" FROM users WHERE email = ?", strings.TrimSpace(email)))
}

// ListUsers returns every user, oldest first.
func ListUsers(ctx context.Context, db *sql.DB) ([]User, error) {
	rows, err := db.QueryContext(ctx, "SELECT "+userCols+" FROM users ORDER BY id")
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
	u, err := scanUser(db.QueryRowContext(ctx,
		"INSERT INTO users (email, name, is_admin, created_at) VALUES (?, ?, ?, ?) ON CONFLICT (email) DO NOTHING RETURNING "+userCols,
		email, name, admin, time.Now().Unix()))
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
		"INSERT INTO users (email, name, is_admin, created_at) VALUES (?, ?, 1, ?) ON CONFLICT (email) DO UPDATE SET is_admin = 1",
		email, name, time.Now().Unix())
	return err
}

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
