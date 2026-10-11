package api

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"fazon.nexus/backend/internal/db"
)

// The admin page's API (/v1/admin/users…): session cookie and the usual Origin/CSRF rules,
// for moderators and admins only (403 for everyone else). What each role may do:
//
//	                                     moderator           admin
//	list users                           yes, no emails      yes, with emails (search by email too)
//	mute / unmute                        regular users only  anyone but themselves
//	change roles                         no                  anyone but themselves
//	delete accounts                      no                  anyone but themselves
//	delete any comment (comments.go)     yes                 yes
//
// The last admin can never be demoted, muted or deleted (nor delete their own account, see
// account.go). Every action is logged at Info with the actor, the action and the target.

const (
	adminPageSize = 50
	adminMaxPage  = 200
)

// adminRights is what the viewer may do to one user.
type adminRights struct {
	Role   bool `json:"role"`
	Mute   bool `json:"mute"` // mute and unmute
	Delete bool `json:"delete"`
}

// rightsOver works out what actor may do to the user targetID with role targetRole, given the
// number of admins.
func rightsOver(actor *user, targetID int64, targetRole string, admins int) adminRights {
	if actor.ID == targetID {
		return adminRights{}
	}
	switch actor.Role {
	case db.RoleAdmin:
		ok := targetRole != db.RoleAdmin || admins > 1 // never the last admin
		return adminRights{Role: ok, Mute: ok, Delete: ok}
	case db.RoleModerator:
		return adminRights{Mute: targetRole == db.RoleUser}
	}
	return adminRights{}
}

type mutedByJSON struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type adminUserJSON struct {
	User        *publicUser  `json:"user"`
	Email       *string      `json:"email,omitempty"` // admins only
	Role        string       `json:"role"`
	Joined      string       `json:"joined"`
	LastActive  *string      `json:"last_active"` // newest session use (null without a live session)
	Comments    int          `json:"comments"`
	Muted       bool         `json:"muted"`
	MutedAt     *string      `json:"muted_at"`
	MutedBy     *mutedByJSON `json:"muted_by"` // null if not muted or that account is gone
	TrackPublic bool         `json:"track_public"`
	Can         adminRights  `json:"can"`
}

// countAdmins counts admins; q is *sql.DB or *sql.Tx.
func countAdmins(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}) (int, error) {
	var n int
	err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role = 'admin' AND id > 0`).Scan(&n)
	return n, err
}

// adminUsers loads the users matching where (a condition on u, with args) for viewer,
// newest first, plus the total number of matches.
func (s *Server) adminUsers(ctx context.Context, viewer *user, where string, args []any, limit, offset int) ([]adminUserJSON, int, error) {
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users u WHERE u.id > 0 AND `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	admins, err := countAdmins(ctx, s.db)
	if err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT u.id, u.email, u.role, u.created_at, u.muted_at, mb.id, mb.name, u.track_public,
			(SELECT MAX(last_seen) FROM sessions WHERE user_id = u.id),
			(SELECT COUNT(*) FROM comments WHERE user_id = u.id AND deleted_at IS NULL)
		FROM users u LEFT JOIN users mb ON mb.id = u.muted_by
		WHERE u.id > 0 AND `+where+`
		ORDER BY u.id DESC LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []adminUserJSON{}
	var ids []int64
	for rows.Next() {
		var a adminUserJSON
		var id, joined int64
		var email string
		var mutedAt, mbID, lastSeen sql.NullInt64
		var mbName sql.NullString
		if err := rows.Scan(&id, &email, &a.Role, &joined, &mutedAt, &mbID, &mbName, &a.TrackPublic, &lastSeen, &a.Comments); err != nil {
			return nil, 0, err
		}
		a.User = &publicUser{ID: id}
		if viewer.Admin {
			a.Email = &email
		}
		a.Joined = rfc3339(joined)
		if lastSeen.Valid {
			t := rfc3339(lastSeen.Int64)
			a.LastActive = &t
		}
		if mutedAt.Valid {
			t := rfc3339(mutedAt.Int64)
			a.Muted, a.MutedAt = true, &t
			if mbID.Valid {
				a.MutedBy = &mutedByJSON{mbID.Int64, mbName.String}
			}
		}
		a.Can = rightsOver(viewer, id, a.Role, admins)
		ids = append(ids, id)
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	pub, err := s.publicUsers(ctx, ids)
	if err != nil {
		return nil, 0, err
	}
	for i := range out {
		if p := pub[out[i].User.ID]; p != nil {
			out[i].User = p
		}
	}
	return out, total, nil
}

// staff wraps an admin-page handler: moderators and admins only.
func (s *Server) staff(h func(http.ResponseWriter, *http.Request, *user)) http.HandlerFunc {
	return s.authed(func(w http.ResponseWriter, r *http.Request, u *user) {
		if !u.moderator() {
			writeErr(w, http.StatusForbidden, "not allowed")
			return
		}
		h(w, r, u)
	})
}

// listAdminUsers: GET /v1/admin/users?q=&limit=&offset= → {users, total, limit, offset, viewer,
// admins}. q matches names (and, for admins, emails), case-insensitively.
func (s *Server) listAdminUsers(w http.ResponseWriter, r *http.Request, u *user) {
	qs := r.URL.Query()
	limit, offset := adminPageSize, 0
	for _, p := range []struct {
		name string
		dst  *int
		min  int
	}{{"limit", &limit, 1}, {"offset", &offset, 0}} {
		if v := qs.Get(p.name); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < p.min {
				writeErr(w, http.StatusBadRequest, "invalid "+p.name)
				return
			}
			*p.dst = n
		}
	}
	limit = min(limit, adminMaxPage)
	where, args := "1", []any{}
	if q := strings.TrimSpace(qs.Get("q")); q != "" {
		if len(q) > 200 {
			writeErr(w, http.StatusBadRequest, "search too long")
			return
		}
		like := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q) + "%"
		where, args = `(u.name LIKE ? ESCAPE '\')`, []any{like}
		if u.Admin {
			where, args = `(u.name LIKE ?1 ESCAPE '\' OR u.email LIKE ?1 ESCAPE '\')`, []any{like}
		}
	}
	users, total, err := s.adminUsers(r.Context(), u, where, args, limit, offset)
	if err != nil {
		internalErr(w, "admin: list users", err)
		return
	}
	admins, err := countAdmins(r.Context(), s.db)
	if err != nil {
		internalErr(w, "admin: count admins", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users, "total": total, "limit": limit, "offset": offset,
		"viewer": map[string]any{"id": u.ID, "role": u.Role}, "admins": admins})
}

// writeAdminUser answers {"user": <the row for id, as viewer sees it>}.
func (s *Server) writeAdminUser(w http.ResponseWriter, r *http.Request, viewer *user, id int64) {
	users, _, err := s.adminUsers(r.Context(), viewer, "u.id = ?", []any{id}, 1, 0)
	if err != nil {
		internalErr(w, "admin: load user", err)
		return
	}
	if len(users) == 0 {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": users[0]})
}

// adminTarget loads the target's role and the admin count inside tx and checks one right:
// errStatus 404 if there is no such user, 403/409 if actor may not do it.
func adminTarget(ctx context.Context, tx *sql.Tx, actor *user, id int64, right func(adminRights) bool, what string) (string, error) {
	var role string
	err := tx.QueryRowContext(ctx, `SELECT role FROM users WHERE id = ? AND id > 0`, id).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errStatus{http.StatusNotFound, "not found"}
	}
	if err != nil {
		return "", err
	}
	admins, err := countAdmins(ctx, tx)
	if err != nil {
		return "", err
	}
	if right(rightsOver(actor, id, role, admins)) {
		return role, nil
	}
	switch {
	case actor.ID == id:
		return "", errStatus{http.StatusForbidden, "you can't " + what + " yourself"}
	case actor.Role == db.RoleAdmin: // only the last-admin rule stops an admin
		return "", errStatus{http.StatusConflict, "that's the only admin: make someone else an admin first"}
	case what == "mute" || what == "unmute":
		return "", errStatus{http.StatusForbidden, "moderators can only " + what + " regular users"}
	}
	return "", errStatus{http.StatusForbidden, "only admins can do that"}
}

// setRole: POST /v1/admin/users/{id}/role {"role":"user"|"moderator"|"admin"} (admins only).
func (s *Server) adminSetRole(w http.ResponseWriter, r *http.Request, u *user) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		Role string `json:"role"`
	}
	if !decode(w, r, maxBody, &in) {
		return
	}
	if !db.ValidRole(in.Role) {
		writeErr(w, http.StatusBadRequest, `role must be "user", "moderator" or "admin"`)
		return
	}
	if !u.Admin {
		writeErr(w, http.StatusForbidden, "only admins can change roles")
		return
	}
	var old string
	err := s.tx(r.Context(), func(tx *sql.Tx) error {
		var err error
		if old, err = adminTarget(r.Context(), tx, u, id, func(c adminRights) bool { return c.Role }, "change the role of"); err != nil {
			return err
		}
		_, err = tx.Exec(`UPDATE users SET role = ?, is_admin = ? WHERE id = ?`, in.Role, in.Role == db.RoleAdmin, id)
		return err
	})
	if err != nil {
		writeTxErr(w, "admin: set role", err)
		return
	}
	slog.Info("admin action", "actor", u.ID, "action", "role", "target", id, "from", old, "to", in.Role)
	s.writeAdminUser(w, r, u, id)
}

func (s *Server) adminMute(w http.ResponseWriter, r *http.Request, u *user) { s.setMute(w, r, u, true) }

func (s *Server) adminUnmute(w http.ResponseWriter, r *http.Request, u *user) {
	s.setMute(w, r, u, false)
}

// setMute: POST /v1/admin/users/{id}/mute or …/unmute (moderators: regular users only).
// Idempotent; re-muting keeps the original time and moderator.
func (s *Server) setMute(w http.ResponseWriter, r *http.Request, u *user, mute bool) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	what := map[bool]string{true: "mute", false: "unmute"}[mute]
	err := s.tx(r.Context(), func(tx *sql.Tx) error {
		if _, err := adminTarget(r.Context(), tx, u, id, func(c adminRights) bool { return c.Mute }, what); err != nil {
			return err
		}
		var err error
		if mute {
			_, err = tx.Exec(`UPDATE users SET muted_at = ?, muted_by = ? WHERE id = ? AND muted_at IS NULL`, s.now().Unix(), u.ID, id)
		} else {
			_, err = tx.Exec(`UPDATE users SET muted_at = NULL, muted_by = NULL WHERE id = ?`, id)
		}
		return err
	})
	if err != nil {
		writeTxErr(w, "admin: "+what, err)
		return
	}
	slog.Info("admin action", "actor", u.ID, "action", what, "target", id)
	s.writeAdminUser(w, r, u, id)
}

// adminDeleteUser: DELETE /v1/admin/users/{id} (admins only) deletes the account and all its
// data exactly like DELETE /v1/me (db.DeleteUser).
func (s *Server) adminDeleteUser(w http.ResponseWriter, r *http.Request, u *user) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if !u.Admin {
		writeErr(w, http.StatusForbidden, "only admins can delete accounts")
		return
	}
	err := s.tx(r.Context(), func(tx *sql.Tx) error {
		if _, err := adminTarget(r.Context(), tx, u, id, func(c adminRights) bool { return c.Delete }, "delete"); err != nil {
			return err
		}
		return db.DeleteUser(r.Context(), tx, id, s.now())
	})
	if err != nil {
		writeTxErr(w, "admin: delete user", err)
		return
	}
	slog.Info("admin action", "actor", u.ID, "action", "delete_account", "target", id)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
