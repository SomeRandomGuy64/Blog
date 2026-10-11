package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
)

type notificationJSON struct {
	ID        int64       `json:"id"`
	Kind      string      `json:"kind"`
	Actor     *publicUser `json:"actor"`
	Page      string      `json:"page"`
	PageTitle string      `json:"page_title"`
	CommentID int64       `json:"comment_id"` // 0 for an article like
	Snippet   string      `json:"snippet"`
	Created   string      `json:"created"`
	Read      bool        `json:"read"`
}

func (s *Server) unread(ctx context.Context, userID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notifications WHERE user_id = ? AND read_at IS NULL`, userID).Scan(&n)
	return n, err
}

func (s *Server) listNotifications(w http.ResponseWriter, r *http.Request, u *user) {
	q := r.URL.Query()
	limit, before := int64(50), int64(0)
	for _, p := range []struct {
		name string
		dst  *int64
	}{{"limit", &limit}, {"before", &before}} {
		if v := q.Get(p.name); v != "" {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil || n < 1 {
				writeErr(w, http.StatusBadRequest, "invalid "+p.name)
				return
			}
			*p.dst = n
		}
	}
	limit = min(limit, 100)
	// A comment notification points at its comment (and shows a snippet of it); an article
	// like ('page_like') has no comment, only the page (comment_id 0, snippet "").
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT n.id, n.kind, n.actor_id, COALESCE(c.page, n.page, ''), COALESCE(p.title, ''),
			COALESCE(n.comment_id, 0), COALESCE(c.body, ''), n.created_at, n.read_at IS NOT NULL
		FROM notifications n
		LEFT JOIN comments c ON c.id = n.comment_id
		LEFT JOIN pages p ON p.path = COALESCE(c.page, n.page)
		WHERE n.user_id = ? AND (? = 0 OR n.id < ?)
		ORDER BY n.id DESC LIMIT ?`, u.ID, before, before, limit)
	if err != nil {
		internalErr(w, "list notifications", err)
		return
	}
	defer rows.Close()
	out := []notificationJSON{}
	var actors []int64
	for rows.Next() {
		var n notificationJSON
		var created, actor int64
		if err := rows.Scan(&n.ID, &n.Kind, &actor, &n.Page, &n.PageTitle,
			&n.CommentID, &n.Snippet, &created, &n.Read); err != nil {
			internalErr(w, "scan notification", err)
			return
		}
		n.Snippet = truncateRunes(n.Snippet, snippetRunes)
		n.Created = rfc3339(created)
		n.Actor = &publicUser{ID: actor}
		actors = append(actors, actor)
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		internalErr(w, "list notifications", err)
		return
	}
	users, err := s.publicUsers(r.Context(), actors)
	if err != nil {
		internalErr(w, "load notification actors", err)
		return
	}
	for i := range out {
		out[i].Actor = users[out[i].Actor.ID]
	}
	unread, err := s.unread(r.Context(), u.ID)
	if err != nil {
		internalErr(w, "count unread", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"notifications": out, "unread": unread})
}

func (s *Server) readNotifications(w http.ResponseWriter, r *http.Request, u *user) {
	var in struct {
		IDs []int64 `json:"ids"`
		All bool    `json:"all"`
	}
	if !decode(w, r, maxBody, &in) {
		return
	}
	now := s.now().Unix()
	var err error
	switch {
	case in.All:
		_, err = s.db.ExecContext(r.Context(),
			`UPDATE notifications SET read_at = ? WHERE user_id = ? AND read_at IS NULL`, now, u.ID)
	case len(in.IDs) > 0 && len(in.IDs) <= 1000:
		ids, _ := json.Marshal(in.IDs)
		_, err = s.db.ExecContext(r.Context(), `UPDATE notifications SET read_at = ?
			WHERE user_id = ? AND read_at IS NULL AND id IN (SELECT value FROM json_each(?))`, now, u.ID, string(ids))
	default:
		writeErr(w, http.StatusBadRequest, `send {"ids":[...]} (1-1000 ids) or {"all":true}`)
		return
	}
	if err != nil {
		internalErr(w, "mark notifications read", err)
		return
	}
	unread, err := s.unread(r.Context(), u.ID)
	if err != nil {
		internalErr(w, "count unread", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"unread": unread})
}
