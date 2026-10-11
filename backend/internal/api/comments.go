package api

import (
	"cmp"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// mutedMsg answers a muted user's comment or reply (403).
const mutedMsg = "You've been blocked from commenting."

const (
	maxCommentRunes = 5000
	maxTitleRunes   = 200
	snippetRunes    = 140
)

var pageRE = regexp.MustCompile(`^/[A-Za-z0-9._~%/-]{1,200}$`)

// validPage applies the spec's pattern, and also refuses "//" anywhere: notification links are
// built from the page, and "//host/" would be a protocol-relative link off-site.
func validPage(p string) bool {
	return pageRE.MatchString(p) && strings.HasSuffix(p, "/") && !strings.Contains(p, "//")
}

type commentJSON struct {
	ID      int64       `json:"id"`
	Parent  *int64      `json:"parent"`
	Author  *publicUser `json:"author"`
	Body    string      `json:"body"`
	Created string      `json:"created"`
	Deleted bool        `json:"deleted"`
	Likes   int         `json:"likes"`
	Liked   bool        `json:"liked"`
	Mine    bool        `json:"mine"`
}

// errStatus carries an HTTP status out of a transaction.
type errStatus struct {
	code int
	msg  string
}

func (e errStatus) Error() string { return e.msg }

// writeTxErr answers an error returned from s.tx: its own status if it is an errStatus.
func writeTxErr(w http.ResponseWriter, what string, err error) {
	var es errStatus
	if errors.As(err, &es) {
		writeErr(w, es.code, es.msg)
		return
	}
	internalErr(w, what, err)
}

func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// cleanBody normalises newlines, drops control characters other than newline and tab, and trims.
func cleanBody(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || !unicode.IsControl(r) {
			return r
		}
		return -1
	}, s)
	return strings.TrimSpace(s)
}

func (s *Server) listComments(w http.ResponseWriter, r *http.Request) {
	page := r.URL.Query().Get("page")
	if !validPage(page) {
		writeErr(w, http.StatusBadRequest, "invalid page")
		return
	}
	viewer, err := s.session(w, r)
	if err != nil {
		internalErr(w, "load session", err)
		return
	}
	var viewerID int64
	if viewer != nil {
		viewerID = viewer.ID
	}
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT c.id, c.parent_id, c.user_id, c.body, c.created_at, c.deleted_at IS NOT NULL,
			(SELECT COUNT(*) FROM comment_likes l WHERE l.comment_id = c.id),
			EXISTS (SELECT 1 FROM comment_likes l WHERE l.comment_id = c.id AND l.user_id = ?)
		FROM comments c
		WHERE c.page = ?
		ORDER BY c.created_at, c.id`, viewerID, page)
	if err != nil {
		internalErr(w, "list comments", err)
		return
	}
	defer rows.Close()
	all := []commentJSON{}
	for rows.Next() {
		var c commentJSON
		var parent sql.NullInt64
		var author publicUser // only the id for now; filled in below
		var created int64
		if err := rows.Scan(&c.ID, &parent, &author.ID, &c.Body, &created, &c.Deleted, &c.Likes, &c.Liked); err != nil {
			internalErr(w, "scan comment", err)
			return
		}
		if parent.Valid {
			c.Parent = &parent.Int64
		}
		c.Created = rfc3339(created)
		c.Author = &author
		c.Mine = viewerID != 0 && author.ID == viewerID
		all = append(all, c)
	}
	if err := rows.Err(); err != nil {
		internalErr(w, "list comments", err)
		return
	}
	list := visibleComments(all)
	var ids []int64
	for _, c := range list {
		if c.Author != nil {
			ids = append(ids, c.Author.ID)
		}
	}
	authors, err := s.publicUsers(r.Context(), ids)
	if err != nil {
		internalErr(w, "load comment authors", err)
		return
	}
	for i := range list {
		if list[i].Author != nil {
			list[i].Author = authors[list[i].Author.ID]
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"comments": list})
}

// visibleComments drops deleted comments with no live descendants and blanks the rest
// (tombstones keep the thread under them attached).
func visibleComments(all []commentJSON) []commentJSON {
	// A child always has a larger id than its parent, so one pass from the newest comment
	// back marks every ancestor of something visible.
	byID := make(map[int64]int, len(all))
	order := make([]int, len(all))
	for i, c := range all {
		byID[c.ID] = i
		order[i] = i
	}
	slices.SortFunc(order, func(a, b int) int { return cmp.Compare(all[b].ID, all[a].ID) })
	shown := make([]bool, len(all))
	for _, i := range order {
		c := all[i]
		if c.Deleted && !shown[i] {
			continue
		}
		shown[i] = true
		if c.Parent != nil {
			if p, ok := byID[*c.Parent]; ok {
				shown[p] = true
			}
		}
	}
	out := []commentJSON{}
	for i, c := range all {
		if !shown[i] {
			continue
		}
		if c.Deleted {
			c.Body, c.Author, c.Likes, c.Liked, c.Mine = "", nil, 0, false, false
		}
		out = append(out, c)
	}
	return out
}

func (s *Server) postComment(w http.ResponseWriter, r *http.Request, u *user) {
	if u.Muted {
		writeErr(w, http.StatusForbidden, mutedMsg)
		return
	}
	var in struct {
		Page   string `json:"page"`
		Parent *int64 `json:"parent"`
		Body   string `json:"body"`
	}
	if !decode(w, r, maxBody, &in) {
		return
	}
	if !validPage(in.Page) {
		writeErr(w, http.StatusBadRequest, "invalid page")
		return
	}
	body := cleanBody(in.Body)
	if n := utf8.RuneCountInString(body); n < 1 || n > maxCommentRunes {
		writeErr(w, http.StatusBadRequest, "comment must be 1-5000 characters")
		return
	}
	// The page must be one the site publishes with a comment thread, and its title is the
	// published one: a client-sent "title" (older frontends send one) is ignored.
	title, ok, loaded := s.commentPage(r.Context(), in.Page)
	if !ok {
		if !loaded {
			writeErr(w, http.StatusServiceUnavailable, "comments are temporarily unavailable, try again in a minute")
		} else {
			writeErr(w, http.StatusNotFound, "this page doesn't take comments")
		}
		return
	}
	now := s.now()
	if !s.commentLimit.allow(strconv.FormatInt(u.ID, 10), now) {
		writeErr(w, http.StatusTooManyRequests, "too many comments, slow down")
		return
	}
	var id int64
	err := s.tx(r.Context(), func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO pages (path, title) VALUES (?, ?)
			ON CONFLICT (path) DO UPDATE SET title = excluded.title`, in.Page, title); err != nil {
			return err
		}
		var parentAuthor int64
		if in.Parent != nil {
			var page string
			var deleted bool
			err := tx.QueryRow(`SELECT page, user_id, deleted_at IS NOT NULL FROM comments WHERE id = ?`, *in.Parent).
				Scan(&page, &parentAuthor, &deleted)
			switch {
			case errors.Is(err, sql.ErrNoRows) || err == nil && page != in.Page:
				return errStatus{http.StatusBadRequest, "parent comment not found on this page"}
			case err != nil:
				return err
			case deleted:
				return errStatus{http.StatusConflict, "parent comment was deleted"}
			}
		}
		if err := tx.QueryRow(`INSERT INTO comments (page, parent_id, user_id, body, created_at) VALUES (?, ?, ?, ?, ?) RETURNING id`,
			in.Page, in.Parent, u.ID, body, now.Unix()).Scan(&id); err != nil {
			return err
		}
		if parentAuthor != 0 && parentAuthor != u.ID {
			if _, err := tx.Exec(`INSERT OR IGNORE INTO notifications (user_id, kind, actor_id, comment_id, created_at)
				VALUES (?, 'reply', ?, ?, ?)`, parentAuthor, u.ID, id, now.Unix()); err != nil {
				return err
			}
		}
		// Admins hear about every comment, except their own and ones that already notified
		// them as a reply.
		_, err := tx.Exec(`INSERT OR IGNORE INTO notifications (user_id, kind, actor_id, comment_id, created_at)
			SELECT id, 'comment', ?, ?, ? FROM users WHERE role = 'admin' AND id != ? AND id != ?`,
			u.ID, id, now.Unix(), u.ID, parentAuthor)
		return err
	})
	if err != nil {
		writeTxErr(w, "post comment", err)
		return
	}
	author, err := s.publicUser(r.Context(), u.ID)
	if err != nil {
		internalErr(w, "load comment author", err)
		return
	}
	writeJSON(w, http.StatusCreated, commentJSON{ID: id, Parent: in.Parent, Author: author,
		Body: body, Created: rfc3339(now.Unix()), Mine: true})
}

func (s *Server) deleteComment(w http.ResponseWriter, r *http.Request, u *user) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	err := s.tx(r.Context(), func(tx *sql.Tx) error {
		var author int64
		var deleted bool
		err := tx.QueryRow(`SELECT user_id, deleted_at IS NOT NULL FROM comments WHERE id = ?`, id).Scan(&author, &deleted)
		if errors.Is(err, sql.ErrNoRows) {
			return errStatus{http.StatusNotFound, "not found"}
		}
		if err != nil {
			return err
		}
		if author != u.ID && !u.moderator() {
			return errStatus{http.StatusForbidden, "not your comment"}
		}
		if deleted {
			return nil
		}
		if author != u.ID {
			slog.Info("moderation", "actor", u.ID, "action", "delete_comment", "comment", id, "target", author)
		}
		// Soft delete: the row stays so replies keep their parent. Its likes and every
		// notification about it go.
		if _, err := tx.Exec(`UPDATE comments SET deleted_at = ?, body = '' WHERE id = ?`, s.now().Unix(), id); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM notifications WHERE comment_id = ?`, id); err != nil {
			return err
		}
		_, err = tx.Exec(`DELETE FROM comment_likes WHERE comment_id = ?`, id)
		return err
	})
	if err != nil {
		writeTxErr(w, "delete comment", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) likeComment(w http.ResponseWriter, r *http.Request, u *user) {
	s.setLike(w, r, u, true)
}

func (s *Server) unlikeComment(w http.ResponseWriter, r *http.Request, u *user) {
	s.setLike(w, r, u, false)
}

// setLike likes or unlikes idempotently; only a new like notifies, and unliking withdraws it.
func (s *Server) setLike(w http.ResponseWriter, r *http.Request, u *user, like bool) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var likes int
	err := s.tx(r.Context(), func(tx *sql.Tx) error {
		var author int64
		var deleted bool
		err := tx.QueryRow(`SELECT user_id, deleted_at IS NOT NULL FROM comments WHERE id = ?`, id).Scan(&author, &deleted)
		if errors.Is(err, sql.ErrNoRows) {
			return errStatus{http.StatusNotFound, "not found"}
		}
		if err != nil {
			return err
		}
		now := s.now().Unix()
		if like {
			if deleted {
				return errStatus{http.StatusConflict, "comment was deleted"}
			}
			res, err := tx.Exec(`INSERT OR IGNORE INTO comment_likes (comment_id, user_id, created_at) VALUES (?, ?, ?)`, id, u.ID, now)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 1 && author != u.ID {
				if _, err := tx.Exec(`INSERT OR IGNORE INTO notifications (user_id, kind, actor_id, comment_id, created_at)
					VALUES (?, 'like', ?, ?, ?)`, author, u.ID, id, now); err != nil {
					return err
				}
			}
		} else {
			if _, err := tx.Exec(`DELETE FROM comment_likes WHERE comment_id = ? AND user_id = ?`, id, u.ID); err != nil {
				return err
			}
			if _, err := tx.Exec(`DELETE FROM notifications WHERE kind = 'like' AND actor_id = ? AND comment_id = ?`, u.ID, id); err != nil {
				return err
			}
		}
		return tx.QueryRow(`SELECT COUNT(*) FROM comment_likes WHERE comment_id = ?`, id).Scan(&likes)
	})
	if err != nil {
		writeTxErr(w, "like comment", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"likes": likes, "liked": like})
}
