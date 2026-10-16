package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"fazon.nexus/backend/internal/db"
)

const (
	maxWeekSubcats = 30   // per category in one week's form
	maxAllSubcats  = 1000 // per category, archived included: bounds the register of names
)

type trackCategory struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

var trackCategories = []trackCategory{
	{"active", "Active immersion"},
	{"passive", "Passive immersion"},
	{"phonetic", "Phonetic training"},
	{"output", "Output"},
	{"soroban", "Soroban"},
	{"anki", "Anki"},
}

// catIndex is the category's position in trackCategories, or -1.
func catIndex(c string) int {
	return slices.IndexFunc(trackCategories, func(x trackCategory) bool { return x.ID == c })
}

type subcatJSON struct {
	ID       int64  `json:"id"`
	Category string `json:"category"`
	Name     string `json:"name"`
	Archived bool   `json:"archived"`
	Position int    `json:"position"`
}

// userSubcats lists a user's sub-categories in display order (category, position, id).
func (s *Server) userSubcats(ctx context.Context, userID int64) ([]subcatJSON, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, category, name, archived, position FROM track_subcats WHERE user_id = ? ORDER BY position, id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []subcatJSON{}
	for rows.Next() {
		var c subcatJSON
		if err := rows.Scan(&c.ID, &c.Category, &c.Name, &c.Archived, &c.Position); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	slices.SortStableFunc(out, func(a, b subcatJSON) int { return catIndex(a.Category) - catIndex(b.Category) })
	return out, rows.Err()
}

func (s *Server) trackConfig(w http.ResponseWriter, r *http.Request, u *user) {
	subcats, err := s.userSubcats(r.Context(), u.ID)
	if err != nil {
		internalErr(w, "list subcats", err)
		return
	}
	var public bool
	if err := s.db.QueryRowContext(r.Context(), `SELECT track_public FROM users WHERE id = ?`, u.ID).Scan(&public); err != nil {
		internalErr(w, "load track settings", err)
		return
	}
	p, err := s.publication(r.Context(), u.Admin)
	if err != nil {
		internalErr(w, "load publication", err)
		return
	}
	out := map[string]any{"categories": trackCategories, "subcats": subcats, "public": public,
		"current_week": p.CurrentWeek, "current_week_end": p.CurrentWeekEnd, "published_through": p.PublishedThrough,
		"ended_early": p.EndedEarly}
	if p.EndWeek != nil {
		out["end_week"] = p.EndWeek // admins only
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) trackSettings(w http.ResponseWriter, r *http.Request, u *user) {
	var in struct {
		Public *bool `json:"public"`
	}
	if !decode(w, r, maxBody, &in) {
		return
	}
	if in.Public == nil {
		writeErr(w, http.StatusBadRequest, "public must be true or false")
		return
	}
	if !s.trackLimit.allow(strconv.FormatInt(u.ID, 10), s.now()) {
		writeErr(w, http.StatusTooManyRequests, "too many changes, slow down")
		return
	}
	if _, err := s.db.ExecContext(r.Context(), `UPDATE users SET track_public = ? WHERE id = ?`, *in.Public, u.ID); err != nil {
		internalErr(w, "save track settings", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"public": *in.Public})
}

// subcatConflict reports a clash with an existing non-archived name (case-insensitive), ignoring
// the subcat self, or, for a new one (self 0), the cap on a category's register of names. How
// many a week shows is limited separately (maxWeekSubcats, addWeekSubcat).
func subcatConflict(tx *sql.Tx, userID int64, category, name string, self int64) error {
	var all, dup int
	err := tx.QueryRow(`SELECT COUNT(*), COUNT(*) FILTER (WHERE archived = 0 AND name = ? COLLATE NOCASE AND id != ?)
		FROM track_subcats WHERE user_id = ? AND category = ?`, name, self, userID, category).Scan(&all, &dup)
	switch {
	case err != nil:
		return err
	case dup > 0:
		return errStatus{http.StatusConflict, "you already have a sub-category with that name"}
	case self == 0 && all >= maxAllSubcats:
		return errStatus{http.StatusConflict, "too many sub-categories in this category"}
	}
	return nil
}

func (s *Server) createSubcat(w http.ResponseWriter, r *http.Request, u *user) {
	var in struct {
		Category string `json:"category"`
		Name     string `json:"name"`
	}
	if !decode(w, r, maxBody, &in) {
		return
	}
	name, err := validSubcat(in.Category, in.Name)
	if err != nil {
		writeTxErr(w, "create subcat", err)
		return
	}
	if !s.trackLimit.allow(strconv.FormatInt(u.ID, 10), s.now()) {
		writeErr(w, http.StatusTooManyRequests, "too many changes, slow down")
		return
	}
	c, err := s.insertSubcat(r.Context(), u.ID, in.Category, name)
	if err != nil {
		writeTxErr(w, "create subcat", err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

// validSubcat checks a new sub-category's category and name, returning the normalized name or
// a 400 errStatus.
func validSubcat(category, name string) (string, error) {
	if catIndex(category) < 0 {
		return "", errStatus{http.StatusBadRequest, "unknown category"}
	}
	name, err := db.NormalizeName(name)
	if err != nil {
		return "", errStatus{http.StatusBadRequest, err.Error()}
	}
	return name, nil
}

// insertSubcat creates a (validated) sub-category at the end of its category; errStatus 409 on
// a name clash or the per-category limits.
func (s *Server) insertSubcat(ctx context.Context, userID int64, category, name string) (subcatJSON, error) {
	var c subcatJSON
	err := s.tx(ctx, func(tx *sql.Tx) (err error) {
		c, err = insertSubcatTx(tx, userID, category, name, s.now().Unix())
		return err
	})
	return c, err
}

// insertSubcatTx is insertSubcat inside a transaction (the Kyōtsu trainer creates its
// sub-category in the transaction that saves the session).
func insertSubcatTx(tx *sql.Tx, userID int64, category, name string, now int64) (subcatJSON, error) {
	c := subcatJSON{Category: category, Name: name}
	if err := subcatConflict(tx, userID, category, name, 0); err != nil {
		return c, err
	}
	err := tx.QueryRow(`INSERT INTO track_subcats (user_id, category, name, position, created_at)
		VALUES (?1, ?2, ?3, (SELECT IFNULL(MAX(position) + 1, 0) FROM track_subcats WHERE user_id = ?1 AND category = ?2), ?4)
		RETURNING id, position`, userID, category, name, now).Scan(&c.ID, &c.Position)
	return c, err
}

func (s *Server) updateSubcat(w http.ResponseWriter, r *http.Request, u *user) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		Name     *string `json:"name"`
		Archived *bool   `json:"archived"`
		Position *int    `json:"position"`
	}
	if !decode(w, r, maxBody, &in) {
		return
	}
	if in.Name != nil {
		name, err := db.NormalizeName(*in.Name)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		in.Name = &name
	}
	if in.Position != nil && (*in.Position < 0 || *in.Position > 10000) {
		writeErr(w, http.StatusBadRequest, "position must be 0-10000")
		return
	}
	if !s.trackLimit.allow(strconv.FormatInt(u.ID, 10), s.now()) {
		writeErr(w, http.StatusTooManyRequests, "too many changes, slow down")
		return
	}
	var c subcatJSON
	err := s.tx(r.Context(), func(tx *sql.Tx) error {
		err := tx.QueryRow(`SELECT id, category, name, archived, position FROM track_subcats WHERE id = ? AND user_id = ?`, id, u.ID).
			Scan(&c.ID, &c.Category, &c.Name, &c.Archived, &c.Position)
		if errors.Is(err, sql.ErrNoRows) {
			return errStatus{http.StatusNotFound, "not found"}
		}
		if err != nil {
			return err
		}
		if in.Name != nil {
			c.Name = *in.Name
		}
		if in.Archived != nil {
			c.Archived = *in.Archived
		}
		if in.Position != nil {
			c.Position = *in.Position
		}
		if !c.Archived {
			if err := subcatConflict(tx, u.ID, c.Category, c.Name, c.ID); err != nil {
				return err
			}
		}
		_, err = tx.Exec(`UPDATE track_subcats SET name = ?, archived = ?, position = ? WHERE id = ?`, c.Name, c.Archived, c.Position, c.ID)
		return err
	})
	if err != nil {
		writeTxErr(w, "update subcat", err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) deleteSubcat(w http.ResponseWriter, r *http.Request, u *user) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	err := s.tx(r.Context(), func(tx *sql.Tx) error {
		var used bool
		err := tx.QueryRow(`SELECT EXISTS (SELECT 1 FROM track_hours WHERE subcat_id = s.id) FROM track_subcats s
			WHERE s.id = ? AND s.user_id = ?`, id, u.ID).Scan(&used)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return errStatus{http.StatusNotFound, "not found"}
		case err != nil:
			return err
		case used:
			return errStatus{http.StatusConflict, "this sub-category has logged hours; archive it instead"}
		}
		_, err = tx.Exec(`DELETE FROM track_subcats WHERE id = ?`, id)
		return err
	})
	if err != nil {
		writeTxErr(w, "delete subcat", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- a week's sub-categories ----

// weekSubcatIDs lists the sub-categories added to a week (track_week_subcats). The week's form
// shows these plus every sub-category the week has hours for.
func weekSubcatIDs(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, userID int64, key string) ([]int64, error) {
	rows, err := q.QueryContext(ctx, `SELECT subcat_id FROM track_week_subcats WHERE user_id = ? AND week = ? ORDER BY subcat_id`, userID, key)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// subcatUnusedTx reports whether a sub-category is in no week: no hours and not added to any.
func subcatUnusedTx(tx *sql.Tx, id int64) (bool, error) {
	var used bool
	err := tx.QueryRow(`SELECT EXISTS (SELECT 1 FROM track_hours WHERE subcat_id = ?1)
		OR EXISTS (SELECT 1 FROM track_week_subcats WHERE subcat_id = ?1)`, id).Scan(&used)
	return !used, err
}

// POST /v1/track/weeks/{key}/subcats {category, name, replace?} adds a sub-category to one week:
// the user's sub-category with that name (an archived one is restored), or a new one. Names are
// shared between weeks, so "Anime" is the same sub-category in every week that has it.
//
// With replace (a sub-category id in the week) it is a rename in this week only: a sub-category
// no other week uses is renamed in place; otherwise this week switches from it to the named one
// (its hours move over when the form is saved; the answer's id differs from replace).
func (s *Server) addWeekSubcat(w http.ResponseWriter, r *http.Request, u *user) {
	k, ok := s.pathWeek(w, r)
	if !ok {
		return
	}
	var in struct {
		Category string `json:"category"`
		Name     string `json:"name"`
		Replace  int64  `json:"replace"`
	}
	if !decode(w, r, maxBody, &in) {
		return
	}
	name, err := validSubcat(in.Category, in.Name)
	if err != nil {
		writeTxErr(w, "add week subcat", err)
		return
	}
	if !s.trackLimit.allow(strconv.FormatInt(u.ID, 10), s.now()) {
		writeErr(w, http.StatusTooManyRequests, "too many changes, slow down")
		return
	}
	key := k.String()
	var c subcatJSON
	err = s.tx(r.Context(), func(tx *sql.Tx) error {
		// What the week shows in this category now, the sub-category being renamed aside.
		shown := map[int64]bool{}
		rows, err := tx.Query(`SELECT subcat_id FROM track_week_subcats ws JOIN track_subcats s ON s.id = ws.subcat_id
				WHERE ws.user_id = ?1 AND ws.week = ?2 AND s.category = ?3
			UNION SELECT subcat_id FROM track_hours WHERE user_id = ?1 AND week = ?2 AND category = ?3 AND subcat_id IS NOT NULL`,
			u.ID, key, in.Category)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			shown[id] = true
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if in.Replace != 0 {
			var old subcatJSON
			err := tx.QueryRow(`SELECT id, category, name, archived, position FROM track_subcats WHERE id = ? AND user_id = ?`,
				in.Replace, u.ID).Scan(&old.ID, &old.Category, &old.Name, &old.Archived, &old.Position)
			if errors.Is(err, sql.ErrNoRows) || err == nil && (old.Category != in.Category || !shown[old.ID]) {
				return errStatus{http.StatusNotFound, "that sub-category isn't in this week"}
			}
			if err != nil {
				return err
			}
			if old.Name == name {
				c = old
				return nil
			}
			delete(shown, old.ID)
			// Only this week has it, and the new name is free (or differs only in case): rename it.
			// A change of case alone renames it everywhere: names are the same in any case, so
			// there is no other sub-category to switch to.
			var elsewhere, taken bool
			if err := tx.QueryRow(`SELECT EXISTS (SELECT 1 FROM track_hours WHERE subcat_id = ?1 AND week != ?2)
					OR EXISTS (SELECT 1 FROM track_week_subcats WHERE subcat_id = ?1 AND week != ?2),
				EXISTS (SELECT 1 FROM track_subcats WHERE user_id = ?3 AND category = ?4 AND archived = 0 AND id != ?1
					AND name = ?5 COLLATE NOCASE)`, old.ID, key, u.ID, in.Category, name).Scan(&elsewhere, &taken); err != nil {
				return err
			}
			if !taken && (!elsewhere || strings.EqualFold(old.Name, name)) {
				if _, err := tx.Exec(`UPDATE track_subcats SET name = ?, archived = 0 WHERE id = ?`, name, old.ID); err != nil {
					return err
				}
				if _, err := tx.Exec(`INSERT OR IGNORE INTO track_week_subcats (user_id, week, subcat_id) VALUES (?, ?, ?)`,
					u.ID, key, old.ID); err != nil {
					return err
				}
				old.Name, old.Archived, c = name, false, old
				return nil
			}
		}
		if c, _, err = subcatByNameTx(tx, u.ID, in.Category, []string{name}, s.now().Unix()); err != nil {
			return err
		}
		switch {
		case shown[c.ID]:
			return errStatus{http.StatusConflict, "“" + c.Name + "” is already in this week"}
		case len(shown) >= maxWeekSubcats:
			return errStatus{http.StatusConflict, fmt.Sprintf("at most %d sub-categories per category in a week", maxWeekSubcats)}
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO track_week_subcats (user_id, week, subcat_id) VALUES (?, ?, ?)`,
			u.ID, key, c.ID); err != nil {
			return err
		}
		if in.Replace != 0 && in.Replace != c.ID {
			if _, err := tx.Exec(`DELETE FROM track_week_subcats WHERE user_id = ? AND week = ? AND subcat_id = ?`, u.ID, key, in.Replace); err != nil {
				return err
			}
			// In no week now (no hours to move here either): delete it, as removeWeekSubcat would.
			unused, err := subcatUnusedTx(tx, in.Replace)
			if err != nil || !unused {
				return err
			}
			_, err = tx.Exec(`DELETE FROM track_subcats WHERE id = ?`, in.Replace)
			return err
		}
		return nil
	})
	if err != nil {
		writeTxErr(w, "add week subcat", err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// DELETE /v1/track/weeks/{key}/subcats/{id} takes a sub-category out of one week's form. Hours
// still show it until they are cleared and saved. A sub-category that is then in no week at all
// is deleted (deleted: true); other weeks keep theirs.
func (s *Server) removeWeekSubcat(w http.ResponseWriter, r *http.Request, u *user) {
	k, ok := s.pathWeek(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if !s.trackLimit.allow(strconv.FormatInt(u.ID, 10), s.now()) {
		writeErr(w, http.StatusTooManyRequests, "too many changes, slow down")
		return
	}
	var deleted bool
	err := s.tx(r.Context(), func(tx *sql.Tx) error {
		var owned bool
		if err := tx.QueryRow(`SELECT EXISTS (SELECT 1 FROM track_subcats WHERE id = ? AND user_id = ?)`, id, u.ID).Scan(&owned); err != nil {
			return err
		}
		if !owned {
			return errStatus{http.StatusNotFound, "not found"}
		}
		if _, err := tx.Exec(`DELETE FROM track_week_subcats WHERE user_id = ? AND week = ? AND subcat_id = ?`, u.ID, k.String(), id); err != nil {
			return err
		}
		unused, err := subcatUnusedTx(tx, id)
		if err != nil || !unused {
			return err
		}
		deleted = true
		_, err = tx.Exec(`DELETE FROM track_subcats WHERE id = ?`, id)
		return err
	})
	if err != nil {
		writeTxErr(w, "remove week subcat", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true, "deleted": deleted})
}
