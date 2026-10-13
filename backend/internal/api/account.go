package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"fazon.nexus/backend/internal/db"
	"fazon.nexus/backend/internal/week"
)

// deleteMe: DELETE /v1/me deletes the requester's account and all its data (db.DeleteUser),
// logs them out and clears the cookie. The last admin can't (409).
func (s *Server) deleteMe(w http.ResponseWriter, r *http.Request, u *user) {
	err := s.tx(r.Context(), func(tx *sql.Tx) error {
		var role string
		if err := tx.QueryRow(`SELECT role FROM users WHERE id = ?`, u.ID).Scan(&role); err != nil {
			return err
		}
		if role == db.RoleAdmin {
			admins, err := countAdmins(r.Context(), tx)
			if err != nil {
				return err
			}
			if admins <= 1 {
				return errStatus{http.StatusConflict, "you're the only admin: make someone else an admin before deleting your account"}
			}
		}
		return db.DeleteUser(r.Context(), tx, u.ID, s.now())
	})
	if err != nil {
		writeTxErr(w, "delete account", err)
		return
	}
	slog.Info("account deleted", "actor", u.ID, "action", "delete_own_account", "target", u.ID)
	s.setCookie(w, "", -1)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type exportComment struct {
	ID        int64  `json:"id"`
	Page      string `json:"page"`
	PageTitle string `json:"page_title"`
	Parent    *int64 `json:"parent"`
	Body      string `json:"body"`
	Created   string `json:"created"`
}

type exportLike struct {
	CommentID int64  `json:"comment_id"`
	Page      string `json:"page"`
	PageTitle string `json:"page_title"`
	Created   string `json:"created"`
}

type exportPageLike struct {
	Page      string `json:"page"`
	PageTitle string `json:"page_title"`
	Created   string `json:"created"`
}

type exportNotification struct {
	Kind      string `json:"kind"`
	Actor     string `json:"actor"` // their display name
	Page      string `json:"page"`
	PageTitle string `json:"page_title"`
	CommentID int64  `json:"comment_id"` // 0 for an article like
	Created   string `json:"created"`
	Read      bool   `json:"read"`
}

// exportMe: GET /v1/me/export downloads everything stored about the requester as one JSON file
// (fazon-nexus-export-YYYY-MM-DD.json): profile, comments, likes given (on comments and on
// articles), notifications, Track
// (sub-categories and every week with hours, ratings, goals, scores and summaries), Kyōtsu
// sessions and API key metadata (never the keys or their hashes). 10 an hour per user (429).
func (s *Server) exportMe(w http.ResponseWriter, r *http.Request, u *user) {
	now := s.now()
	if ok, wait := s.exportLimit.allowWait(strconv.FormatInt(u.ID, 10), now); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		writeErr(w, http.StatusTooManyRequests, "too many exports, try again later")
		return
	}
	out, err := s.export(r.Context(), r, u)
	if err != nil {
		internalErr(w, "export", err)
		return
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		internalErr(w, "export", err)
		return
	}
	slog.Info("data export", "user", u.ID)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="fazon-nexus-export-%s.json"`, now.UTC().Format("2006-01-02")))
	w.WriteHeader(http.StatusOK)
	w.Write(append(b, '\n'))
}

// apiBase is this API's public base URL as the request reached it (for the avatar link).
func apiBase(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func (s *Server) export(ctx context.Context, r *http.Request, u *user) (map[string]any, error) {
	now := s.now()
	// profile
	var joined int64
	var bio string
	var public bool
	var mutedAt sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT created_at, bio, track_public, muted_at FROM users WHERE id = ?`, u.ID).
		Scan(&joined, &bio, &public, &mutedAt); err != nil {
		return nil, err
	}
	pub, err := s.publicUser(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	if pub == nil {
		return nil, errors.New("user vanished")
	}
	var avatar *string
	if pub.Avatar != nil {
		a := apiBase(r) + *pub.Avatar
		avatar = &a
	}
	profile := map[string]any{"id": u.ID, "name": u.Name, "email": u.Email, "role": u.Role, "bio": bio,
		"joined": rfc3339(joined), "avatar": avatar, "level": pub.Level, "xp": pub.XP, "muted": mutedAt.Valid}

	// comments (live ones: a deleted comment keeps no text)
	comments := []exportComment{}
	err = s.each(ctx, func(rows *sql.Rows) error {
		var c exportComment
		var parent sql.NullInt64
		var created int64
		if err := rows.Scan(&c.ID, &c.Page, &c.PageTitle, &parent, &c.Body, &created); err != nil {
			return err
		}
		if parent.Valid {
			c.Parent = &parent.Int64
		}
		c.Created = rfc3339(created)
		comments = append(comments, c)
		return nil
	}, `SELECT c.id, c.page, COALESCE(p.title, ''), c.parent_id, c.body, c.created_at
		FROM comments c LEFT JOIN pages p ON p.path = c.page
		WHERE c.user_id = ? AND c.deleted_at IS NULL ORDER BY c.created_at, c.id`, u.ID)
	if err != nil {
		return nil, err
	}

	likes := []exportLike{}
	err = s.each(ctx, func(rows *sql.Rows) error {
		var l exportLike
		var created int64
		if err := rows.Scan(&l.CommentID, &l.Page, &l.PageTitle, &created); err != nil {
			return err
		}
		l.Created = rfc3339(created)
		likes = append(likes, l)
		return nil
	}, `SELECT l.comment_id, c.page, COALESCE(p.title, ''), l.created_at
		FROM comment_likes l JOIN comments c ON c.id = l.comment_id LEFT JOIN pages p ON p.path = c.page
		WHERE l.user_id = ? ORDER BY l.created_at, l.comment_id`, u.ID)
	if err != nil {
		return nil, err
	}

	articleLikes := []exportPageLike{}
	err = s.each(ctx, func(rows *sql.Rows) error {
		var l exportPageLike
		var created int64
		if err := rows.Scan(&l.Page, &l.PageTitle, &created); err != nil {
			return err
		}
		l.Created = rfc3339(created)
		articleLikes = append(articleLikes, l)
		return nil
	}, `SELECT l.page, COALESCE(p.title, ''), l.created_at
		FROM page_likes l LEFT JOIN pages p ON p.path = l.page
		WHERE l.user_id = ? ORDER BY l.created_at, l.page`, u.ID)
	if err != nil {
		return nil, err
	}

	notifications := []exportNotification{}
	err = s.each(ctx, func(rows *sql.Rows) error {
		var n exportNotification
		var created int64
		if err := rows.Scan(&n.Kind, &n.Actor, &n.Page, &n.PageTitle, &n.CommentID, &created, &n.Read); err != nil {
			return err
		}
		n.Created = rfc3339(created)
		notifications = append(notifications, n)
		return nil
	}, `SELECT n.kind, a.name, COALESCE(c.page, n.page, ''), COALESCE(p.title, ''), COALESCE(n.comment_id, 0),
			n.created_at, n.read_at IS NOT NULL
		FROM notifications n JOIN users a ON a.id = n.actor_id LEFT JOIN comments c ON c.id = n.comment_id
		LEFT JOIN pages p ON p.path = COALESCE(c.page, n.page)
		WHERE n.user_id = ? ORDER BY n.id`, u.ID)
	if err != nil {
		return nil, err
	}

	// Track: every week from the first one with data (logged or with goals) through this one.
	subcats, err := s.userSubcats(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	weeks := []apiWeek{}
	var first sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT MIN(week) FROM (SELECT week FROM track_weeks WHERE user_id = ?1
		UNION ALL SELECT week FROM track_goals WHERE user_id = ?1)`, u.ID).Scan(&first); err != nil {
		return nil, err
	}
	if first.Valid {
		from, err := week.Parse(first.String, now)
		if err != nil {
			from = week.Of(now) // a stored week from the future can't happen; be safe anyway
		}
		all, err := s.apiWeeks(ctx, u.ID, from, week.Of(now), true)
		if err != nil {
			return nil, err
		}
		for _, aw := range all {
			if aw.Logged || len(aw.Goals) > 0 {
				weeks = append(weeks, aw)
			}
		}
	}

	kyotsu := []kyotsuSummary{}
	err = s.each(ctx, func(rows *sql.Rows) error {
		var answers string
		k, err := scanKyotsu(rows, &answers)
		if err != nil {
			return err
		}
		k.Answers = json.RawMessage(answers)
		kyotsu = append(kyotsu, k)
		return nil
	}, `SELECT `+kyotsuCols+`, answers_json
		FROM kyotsu_sessions WHERE user_id = ? ORDER BY started_at, id`, u.ID)
	if err != nil {
		return nil, err
	}

	watch := []watchSummary{}
	err = s.each(ctx, func(rows *sql.Rows) error {
		ws, err := scanWatch(rows)
		watch = append(watch, ws)
		return err
	}, `SELECT `+watchCols+` FROM watch_sessions WHERE user_id = ? ORDER BY started_at, id`, u.ID)
	if err != nil {
		return nil, err
	}

	keys, err := s.apiKeys(ctx, u.ID)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"exported":        rfc3339(now.Unix()),
		"site":            s.cfg.Site,
		"profile":         profile,
		"comments":        comments,
		"likes":           likes,
		"article_likes":   articleLikes,
		"notifications":   notifications,
		"track":           map[string]any{"public": public, "subcats": subcats, "weeks": weeks},
		"kyotsu_sessions": kyotsu,
		"watch_sessions":  watch,
		"api_keys":        keys,
	}, nil
}

// each runs q and calls fn for every row.
func (s *Server) each(ctx context.Context, fn func(*sql.Rows) error, q string, args ...any) error {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := fn(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}
