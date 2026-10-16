package db

import (
	"context"
	"database/sql"
	"time"
)

// FoldedSubcat is one sub-category FoldSubcats folds away.
type FoldedSubcat struct {
	ID         int64
	Category   string
	Name       string
	Created    time.Time
	Weeks      int   // weeks with hours for it
	Centihours int64 // its hours in all weeks, in hundredths
}

// FoldSubcats folds away a user's Track sub-categories created before `before`: in every week,
// their hours are added to the category's unassigned bucket, and then the sub-categories are
// deleted (from every week's form too). Category totals don't change. Weeks whose hours moved get
// a new updated_at, so a form open on the old rows is refused on save instead of writing them
// back. With apply false nothing changes: it only reports what it would fold.
func FoldSubcats(ctx context.Context, d *sql.DB, userID int64, before time.Time, apply bool) ([]FoldedSubcat, error) {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT s.id, s.category, s.name, s.created_at,
			(SELECT COUNT(*) FROM track_hours h WHERE h.subcat_id = s.id),
			(SELECT IFNULL(SUM(centihours), 0) FROM track_hours h WHERE h.subcat_id = s.id)
		FROM track_subcats s WHERE s.user_id = ? AND s.created_at < ? ORDER BY s.category, s.position, s.id`,
		userID, before.Unix())
	if err != nil {
		return nil, err
	}
	var out []FoldedSubcat
	for rows.Next() {
		var f FoldedSubcat
		var created int64
		if err := rows.Scan(&f.ID, &f.Category, &f.Name, &created, &f.Weeks, &f.Centihours); err != nil {
			rows.Close()
			return nil, err
		}
		f.Created = time.Unix(created, 0).UTC()
		out = append(out, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if !apply || len(out) == 0 {
		return out, nil
	}
	const folded = `(SELECT id FROM track_subcats WHERE user_id = ?1 AND created_at < ?2)`
	for _, q := range []string{
		// Weeks touched: a new updated_at, always later than before (as bumpUpdated in the API).
		`UPDATE track_weeks SET updated_at = MAX(?3, updated_at + 1) WHERE user_id = ?1
			AND week IN (SELECT week FROM track_hours WHERE subcat_id IN ` + folded + `)`,
		// Hours → each week's unassigned bucket. Each category stays within 168 h, so the sum does too.
		`INSERT INTO track_hours (user_id, week, category, subcat_id, centihours)
			SELECT user_id, week, category, NULL, SUM(centihours) FROM track_hours
			WHERE user_id = ?1 AND subcat_id IN ` + folded + ` GROUP BY week, category
			ON CONFLICT (user_id, week, category, IFNULL(subcat_id, 0)) DO UPDATE SET centihours = centihours + excluded.centihours`,
		`DELETE FROM track_hours WHERE user_id = ?1 AND subcat_id IN ` + folded,
		`DELETE FROM track_week_subcats WHERE user_id = ?1 AND subcat_id IN ` + folded,
		`DELETE FROM track_subcats WHERE user_id = ?1 AND created_at < ?2`,
	} {
		if _, err := tx.ExecContext(ctx, q, userID, before.Unix(), time.Now().Unix()); err != nil {
			return nil, err
		}
	}
	return out, tx.Commit()
}
