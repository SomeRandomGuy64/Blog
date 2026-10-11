package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"fazon.nexus/backend/internal/db"
)

// publicUser is how any account appears in responses (comment authors, notification actors,
// profiles). It never carries the email.
type publicUser struct {
	ID      int64   `json:"id"`
	Name    string  `json:"name"`
	Admin   bool    `json:"admin"`  // role == "admin" (kept for older frontends)
	Role    string  `json:"role"`   // "user", "moderator" or "admin"
	Avatar  *string `json:"avatar"` // path relative to the API base, or null
	Level   int     `json:"level"`
	XP      int64   `json:"xp"`
	LevelXP int64   `json:"level_xp"`
	NextXP  *int64  `json:"next_xp"` // null at the top level
}

// selfUser is the requester's own account (/v1/me and the /v1/me/* and auth replies).
type selfUser struct {
	publicUser
	Email string `json:"email"`
	Muted bool   `json:"muted"` // blocked from commenting by a moderator
	// Hyojungo: the trainer's Tokyo mode is open to this account (see api/hyojungo.go)
	Hyojungo bool `json:"hyojungo"`
}

func avatarPath(userID, updated int64) string {
	return fmt.Sprintf("/v1/users/%d/avatar?v=%d", userID, updated)
}

// publicUsers loads the given accounts with their avatar and XP in one grouped query. XP is the
// number of likes other people gave the user's comments, plus, for the article author
// (Config.AuthorEmail), the likes other people gave the articles. It is counted live, so
// unlikes and deletions take it back. Unknown ids are absent from the map.
func (s *Server) publicUsers(ctx context.Context, ids []int64) (map[int64]*publicUser, error) {
	out := make(map[int64]*publicUser, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	idsJSON, _ := json.Marshal(ids)
	var author any // NULL matches no one (not even the [deleted] account's empty email)
	if s.cfg.AuthorEmail != "" {
		author = s.cfg.AuthorEmail
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT u.id, u.name, u.role, a.updated_at, COUNT(l.user_id) +
			CASE WHEN u.id > 0 AND u.email = ? THEN (SELECT COUNT(*) FROM page_likes p WHERE p.user_id != u.id) ELSE 0 END
		FROM users u
		LEFT JOIN avatars a ON a.user_id = u.id
		LEFT JOIN comments c ON c.user_id = u.id
		LEFT JOIN comment_likes l ON l.comment_id = c.id AND l.user_id != u.id
		WHERE u.id IN (SELECT value FROM json_each(?))
		GROUP BY u.id`, author, string(idsJSON))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var u publicUser
		var avatar sql.NullInt64
		if err := rows.Scan(&u.ID, &u.Name, &u.Role, &avatar, &u.XP); err != nil {
			return nil, err
		}
		u.Admin = u.Role == db.RoleAdmin
		if avatar.Valid {
			p := avatarPath(u.ID, avatar.Int64)
			u.Avatar = &p
		}
		u.Level, u.LevelXP, u.NextXP = levelOf(u.XP)
		out[u.ID] = &u
	}
	return out, rows.Err()
}

// publicUser loads one account; nil (no error) if it doesn't exist.
func (s *Server) publicUser(ctx context.Context, id int64) (*publicUser, error) {
	m, err := s.publicUsers(ctx, []int64{id})
	return m[id], err
}

func (s *Server) selfUser(ctx context.Context, u *user) (selfUser, error) {
	p, err := s.publicUser(ctx, u.ID)
	if err != nil {
		return selfUser{}, err
	}
	if p == nil {
		return selfUser{}, errors.New("user vanished")
	}
	return selfUser{*p, u.Email, u.Muted, u.Hyojungo}, nil
}

// writeSelf answers {"user": <the requester, freshly loaded>}.
func (s *Server) writeSelf(w http.ResponseWriter, r *http.Request, id int64) {
	u, err := s.loadUser(r.Context(), id)
	var self selfUser
	if err == nil {
		self, err = s.selfUser(r.Context(), u)
	}
	if err != nil {
		internalErr(w, "load user", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": self})
}

type recentComment struct {
	ID        int64  `json:"id"`
	Page      string `json:"page"`
	PageTitle string `json:"page_title"`
	Snippet   string `json:"snippet"`
	Created   string `json:"created"`
	Likes     int    `json:"likes"`
}

func (s *Server) profile(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	u, err := s.publicUser(ctx, id)
	if err != nil {
		internalErr(w, "load profile", err)
		return
	}
	if u == nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	out, err := s.profileBasics(r, u)
	if err != nil {
		internalErr(w, "load profile", err)
		return
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.id, c.page, COALESCE(p.title, ''), c.body, c.created_at,
			(SELECT COUNT(*) FROM comment_likes l WHERE l.comment_id = c.id)
		FROM comments c LEFT JOIN pages p ON p.path = c.page
		WHERE c.user_id = ? AND c.deleted_at IS NULL
		ORDER BY c.created_at DESC, c.id DESC LIMIT 20`, id)
	if err != nil {
		internalErr(w, "load profile comments", err)
		return
	}
	defer rows.Close()
	recent := []recentComment{}
	for rows.Next() {
		var c recentComment
		var created int64
		if err := rows.Scan(&c.ID, &c.Page, &c.PageTitle, &c.Snippet, &created, &c.Likes); err != nil {
			internalErr(w, "scan profile comment", err)
			return
		}
		c.Snippet = truncateRunes(c.Snippet, snippetRunes)
		c.Created = rfc3339(created)
		recent = append(recent, c)
	}
	if err := rows.Err(); err != nil {
		internalErr(w, "load profile comments", err)
		return
	}
	out["recent"] = recent
	writeJSON(w, http.StatusOK, out)
}

// profileBasics is what the profile page and the hover card share: user, bio, joined date,
// live comment count and likes received (= XP: on comments, and on articles for the author).
func (s *Server) profileBasics(r *http.Request, u *publicUser) (map[string]any, error) {
	var joined int64
	var comments int
	var bio string
	if err := s.db.QueryRowContext(r.Context(), `SELECT created_at, bio,
			(SELECT COUNT(*) FROM comments WHERE user_id = users.id AND deleted_at IS NULL)
		FROM users WHERE id = ?`, u.ID).Scan(&joined, &bio, &comments); err != nil {
		return nil, err
	}
	return map[string]any{"user": u, "bio": bio, "joined": rfc3339(joined), "comments": comments, "likes": u.XP}, nil
}

// card is the light version of the profile for hover cards; browsers may reuse it for a minute.
func (s *Server) card(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	u, err := s.publicUser(r.Context(), id)
	if err != nil {
		internalErr(w, "load card", err)
		return
	}
	if u == nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	out, err := s.profileBasics(r, u)
	if err != nil {
		internalErr(w, "load card", err)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=60")
	writeJSON(w, http.StatusOK, out)
}

const (
	maxBioRunes = 300
	maxBioLines = 6
)

// normalizeBio trims a bio and enforces plain text: ≤ 300 runes, ≤ 6 lines, newlines are the
// only control characters allowed.
func normalizeBio(s string) (string, error) {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n"))
	if strings.ContainsFunc(s, func(r rune) bool { return r != '\n' && unicode.IsControl(r) }) || !utf8.ValidString(s) {
		return "", errors.New("bio must be plain text (no tabs or control characters)")
	}
	if utf8.RuneCountInString(s) > maxBioRunes {
		return "", errors.New("bio must be at most 300 characters")
	}
	if strings.Count(s, "\n")+1 > maxBioLines {
		return "", errors.New("bio must be at most 6 lines")
	}
	return s, nil
}

func (s *Server) setProfile(w http.ResponseWriter, r *http.Request, u *user) {
	var in struct {
		Name *string `json:"name"`
		Bio  *string `json:"bio"`
	}
	if !decode(w, r, maxBody, &in) {
		return
	}
	if in.Name == nil && in.Bio == nil {
		writeErr(w, http.StatusBadRequest, "send name and/or bio")
		return
	}
	var err error
	if in.Name != nil {
		if *in.Name, err = db.NormalizeName(*in.Name); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if in.Bio != nil {
		if *in.Bio, err = normalizeBio(*in.Bio); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if !s.nameLimit.allow(fmt.Sprint(u.ID), s.now()) {
		writeErr(w, http.StatusTooManyRequests, "too many changes, slow down")
		return
	}
	if _, err := s.db.ExecContext(r.Context(), `UPDATE users SET name = IFNULL(?, name), bio = IFNULL(?, bio) WHERE id = ?`,
		in.Name, in.Bio, u.ID); err != nil {
		internalErr(w, "save profile", err)
		return
	}
	s.writeSelf(w, r, u.ID)
}
