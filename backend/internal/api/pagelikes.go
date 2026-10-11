package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
)

// Article likes. Any article in the site's comment-pages index (pages.go) can be liked, once per
// account; muted accounts can like too (muting only blocks commenting). Every article is
// written by the author account (Config.AuthorEmail): each like from someone else is a point of
// its XP (see publicUsers) and notifies it ('page_like'), exactly like a like on a comment. The
// author may like their own article: it counts on the article but gives no XP and no
// notification, as with comments.

type pageLikesJSON struct {
	Count int  `json:"count"`
	Liked bool `json:"liked"`
}

// likeablePage reads and checks ?page=: well formed (400) and in the comment-pages index (404,
// after a refresh-on-miss; 503 while no index has ever loaded). It returns the published title.
func (s *Server) likeablePage(w http.ResponseWriter, r *http.Request) (page, title string, ok bool) {
	page = r.URL.Query().Get("page")
	if !validPage(page) {
		writeErr(w, http.StatusBadRequest, "invalid page")
		return "", "", false
	}
	title, ok, loaded := s.commentPage(r.Context(), page)
	if !ok {
		if !loaded {
			writeErr(w, http.StatusServiceUnavailable, "likes are temporarily unavailable, try again in a minute")
		} else {
			writeErr(w, http.StatusNotFound, "this page can't be liked")
		}
		return "", "", false
	}
	return page, title, true
}

// authorID is the id of the account that writes the articles (Config.AuthorEmail), or 0 if
// none is configured or it doesn't exist.
func (s *Server) authorID(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}) (int64, error) {
	if s.cfg.AuthorEmail == "" {
		return 0, nil
	}
	var id int64
	err := q.QueryRowContext(ctx, `SELECT id FROM users WHERE email = ? AND id > 0`, s.cfg.AuthorEmail).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

// pageLikes: GET /v1/pages/likes?page=/articles/x/ → {"count":N,"liked":bool} (liked is false
// when logged out).
func (s *Server) pageLikes(w http.ResponseWriter, r *http.Request) {
	page, _, ok := s.likeablePage(w, r)
	if !ok {
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
	var out pageLikesJSON
	if err := s.db.QueryRowContext(r.Context(), `SELECT COUNT(*), COALESCE(SUM(user_id = ?), 0) > 0
		FROM page_likes WHERE page = ?`, viewerID, page).Scan(&out.Count, &out.Liked); err != nil {
		internalErr(w, "count page likes", err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) likePage(w http.ResponseWriter, r *http.Request, u *user) {
	s.setPageLike(w, r, u, true)
}

func (s *Server) unlikePage(w http.ResponseWriter, r *http.Request, u *user) {
	s.setPageLike(w, r, u, false)
}

// setPageLike likes or unlikes ?page= idempotently. Only a new like notifies the author;
// unliking withdraws that notification (so like/unlike/like never piles them up).
func (s *Server) setPageLike(w http.ResponseWriter, r *http.Request, u *user, like bool) {
	page, title, ok := s.likeablePage(w, r)
	if !ok {
		return
	}
	now := s.now()
	if !s.pageLikeLimit.allow(strconv.FormatInt(u.ID, 10), now) {
		writeErr(w, http.StatusTooManyRequests, "too many likes, slow down")
		return
	}
	var out pageLikesJSON
	err := s.tx(r.Context(), func(tx *sql.Tx) error {
		if like {
			if _, err := tx.Exec(`INSERT INTO pages (path, title) VALUES (?, ?)
				ON CONFLICT (path) DO UPDATE SET title = excluded.title`, page, title); err != nil {
				return err
			}
			res, err := tx.Exec(`INSERT OR IGNORE INTO page_likes (user_id, page, created_at) VALUES (?, ?, ?)`, u.ID, page, now.Unix())
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 1 {
				author, err := s.authorID(r.Context(), tx)
				if err != nil {
					return err
				}
				if author != 0 && author != u.ID {
					if _, err := tx.Exec(`INSERT OR IGNORE INTO notifications (user_id, kind, actor_id, page, created_at)
						VALUES (?, 'page_like', ?, ?, ?)`, author, u.ID, page, now.Unix()); err != nil {
						return err
					}
				}
			}
		} else {
			if _, err := tx.Exec(`DELETE FROM page_likes WHERE user_id = ? AND page = ?`, u.ID, page); err != nil {
				return err
			}
			if _, err := tx.Exec(`DELETE FROM notifications WHERE kind = 'page_like' AND actor_id = ? AND page = ?`, u.ID, page); err != nil {
				return err
			}
		}
		out.Liked = like
		return tx.QueryRow(`SELECT COUNT(*) FROM page_likes WHERE page = ?`, page).Scan(&out.Count)
	})
	if err != nil {
		writeTxErr(w, "like page", err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
