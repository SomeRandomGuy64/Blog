package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"slices"
	"strconv"

	"fazon.nexus/backend/internal/db"
)

const (
	maxActiveSubcats = 30  // per category, non-archived
	maxAllSubcats    = 100 // per category, archived included: bounds what archiving can pile up
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

// subcatConflict reports a clash with the per-category limits or an existing non-archived name
// (case-insensitive), ignoring the subcat self.
func subcatConflict(tx *sql.Tx, userID int64, category, name string, self int64, unarchiving bool) error {
	var active, all, dup int
	err := tx.QueryRow(`SELECT COUNT(*) FILTER (WHERE archived = 0), COUNT(*),
			COUNT(*) FILTER (WHERE archived = 0 AND name = ? COLLATE NOCASE AND id != ?)
		FROM track_subcats WHERE user_id = ? AND category = ?`, name, self, userID, category).Scan(&active, &all, &dup)
	switch {
	case err != nil:
		return err
	case dup > 0:
		return errStatus{http.StatusConflict, "you already have a sub-category with that name"}
	case self == 0 && all >= maxAllSubcats:
		return errStatus{http.StatusConflict, "too many sub-categories in this category (delete some archived ones)"}
	case (self == 0 || unarchiving) && active >= maxActiveSubcats:
		return errStatus{http.StatusConflict, "at most 30 active sub-categories per category"}
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
	if err := subcatConflict(tx, userID, category, name, 0, false); err != nil {
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
		unarchiving := in.Archived != nil && !*in.Archived && c.Archived
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
			if err := subcatConflict(tx, u.ID, c.Category, c.Name, c.ID, unarchiving); err != nil {
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
