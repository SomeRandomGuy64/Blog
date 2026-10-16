package api

import (
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"math"
	"regexp"
	"slices"
	"strings"
)

// Time the site's tools measure (the Kyōtsu trainer, the video watch timer) goes straight into
// the user's Track: a category, a sub-category the tool owns by name, the current week.

// trackAdded is what a session added to its owner's Track: hours in a week (for the Kyōtsu
// trainer the week it ended in; for watch sessions the week it last added to).
type trackAdded struct {
	Week   string  `json:"week"`
	Hours  float64 `json:"hours"`
	Subcat *string `json:"subcat"` // the sub-category's name at the time; null = unassigned
	Note   string  `json:"note,omitempty"`
}

// clientIDRe is a tool's id for one run, which makes its saves idempotent per user.
var clientIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

// practiceCents turns active seconds into hundredths of an hour like the site's stopwatch does:
// rounded to whole minutes first.
func practiceCents(sec int64) int64 {
	min := (sec + 30) / 60
	return int64(math.Round(float64(min) * 100 / 60))
}

// catLabel is a category's display name ("Phonetic training").
func catLabel(cat string) string {
	if i := catIndex(cat); i >= 0 {
		return trackCategories[i].Label
	}
	return cat
}

// trackCredit is what creditTrackTx did: the hundredths it added, the sub-category's name (nil
// = unassigned, or nothing added) and a sentence about anything unusual ("" = nothing to say).
type trackCredit struct {
	added  int64
	subcat any
	note   string
}

// creditTrackTx adds cents to category cat of the user's week key, in the sub-category named
// names[0] (or one of the other names; see trackSubcatTx). It never fails the caller over
// Track's rules: a sub-category that can't be created means the unassigned bucket, and the
// 168 h caps shrink the addition (possibly to nothing).
func creditTrackTx(tx *sql.Tx, userID int64, cat string, names []string, key string, cents, now int64) (trackCredit, error) {
	hours, err := weekHoursTx(tx, userID, key)
	if err != nil {
		return trackCredit{}, err
	}
	var sub int64
	var name, note string
	if catCents(hours, cat) < maxWeekCents { // no sub-category made for time that can't be added
		if sub, name, note, err = trackSubcatTx(tx, userID, cat, names, now); err != nil {
			return trackCredit{}, err
		}
	}
	before := maps.Clone(hours)
	added := addCapped(hours, hourSlot{cat, sub}, cents)
	switch {
	case added == 0:
		note = "Nothing was added: " + catLabel(cat) + " already has 168 h this week."
	case added < cents:
		note = joinNote(note, fmt.Sprintf("Only %s h of %s h was added: %s can't go over 168 h in a week.",
			fmtCents(added), fmtCents(cents), catLabel(cat)))
	}
	if err := writeWeekHoursTx(tx, userID, key, now, before, hours); err != nil {
		return trackCredit{}, err
	}
	c := trackCredit{added: added, note: note}
	if added > 0 && sub != 0 {
		c.subcat = name
	}
	return c, nil
}

func joinNote(a, b string) string {
	if a == "" {
		return b
	}
	return a + " " + b
}

// trackSubcatTx finds the user's sub-category of cat for a tool's time (see subcatByNameTx).
// When the rules don't allow one, the time goes to the unassigned bucket (sub 0) and note says why.
func trackSubcatTx(tx *sql.Tx, userID int64, cat string, names []string, now int64) (sub int64, name, note string, err error) {
	label := catLabel(cat)
	c, how, err := subcatByNameTx(tx, userID, cat, names, now)
	if err != nil {
		var es errStatus
		if !errors.As(err, &es) {
			return 0, "", "", err
		}
		return 0, "", "Your " + label + " sub-categories are at their limit (" + strings.TrimSuffix(es.msg, ".") +
			"), so it went to Other / unassigned.", nil
	}
	switch how {
	case "restored":
		note = "Restored your archived “" + c.Name + "” sub-category under " + label + " for it."
	case "created":
		note = "Created a “" + c.Name + "” sub-category under " + label + " for it."
	}
	return c.ID, c.Name, note, nil
}

// subcatByNameTx finds the user's sub-category of cat named one of names: an active one (any
// case; the exact names[0] first, then display order), else an archived one restored, else a new
// names[0] — by the same rules as the site's sub-category edits. how is "", "restored" or
// "created"; a rule that doesn't allow it (the per-category cap) is an errStatus.
func subcatByNameTx(tx *sql.Tx, userID int64, cat string, names []string, now int64) (c subcatJSON, how string, err error) {
	var active, archived []subcatJSON
	rows, err := tx.Query(`SELECT id, name, archived, position FROM track_subcats WHERE user_id = ? AND category = ?
		ORDER BY position, id`, userID, cat)
	if err != nil {
		return c, "", err
	}
	for rows.Next() {
		x := subcatJSON{Category: cat}
		if err := rows.Scan(&x.ID, &x.Name, &x.Archived, &x.Position); err != nil {
			rows.Close()
			return c, "", err
		}
		if slices.ContainsFunc(names, func(n string) bool { return strings.EqualFold(x.Name, n) }) {
			if x.Archived {
				archived = append(archived, x)
			} else {
				active = append(active, x)
			}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return c, "", err
	}
	pick := func(cs []subcatJSON) subcatJSON { // the exact name if there is one, else the first
		for _, x := range cs {
			if x.Name == names[0] {
				return x
			}
		}
		return cs[0]
	}
	switch {
	case len(active) > 0:
		return pick(active), "", nil
	case len(archived) > 0:
		// The name can't clash: no active sub-category matches it.
		c = pick(archived)
		if _, err := tx.Exec(`UPDATE track_subcats SET archived = 0 WHERE id = ?`, c.ID); err != nil {
			return c, "", err
		}
		c.Archived = false
		return c, "restored", nil
	default:
		c, err = insertSubcatTx(tx, userID, cat, names[0], now)
		return c, "created", err
	}
}
