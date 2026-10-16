package db

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"fazon.nexus/backend/internal/week"
)

// weekOfSQL is 0013's backfill expression for a sub-category's week.
const weekOfSQL = `date(? - 10800, 'unixepoch', '-7 days', 'weekday 6')`

// The backfill's SQL week matches week.Of: around the Sunday 03:00 UTC boundary, on the label
// Saturday itself, mid-week, and hourly across a few weeks (including a month and year turn).
func TestWeekOfSQL(t *testing.T) {
	d := open(t, t.TempDir()+"/x.db")
	check := func(tm time.Time) {
		t.Helper()
		var got string
		if err := d.QueryRow(`SELECT `+weekOfSQL, tm.Unix()).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if want := week.Of(tm).String(); got != want {
			t.Fatalf("%s: SQL week %s, week.Of %s", tm.Format(time.RFC3339), got, want)
		}
	}
	for _, c := range []struct {
		at   time.Time
		want string
	}{
		{time.Date(2026, 10, 17, 23, 0, 0, 0, time.UTC), "2026-10-10"},  // Saturday evening: still the old week
		{time.Date(2026, 10, 18, 2, 59, 59, 0, time.UTC), "2026-10-10"}, // Sunday 02:59:59
		{time.Date(2026, 10, 18, 3, 0, 0, 0, time.UTC), "2026-10-17"},   // Sunday 03:00: the new week
		{time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC), "2026-10-10"},  // Wednesday
		{time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC), "2026-10-10"},   // the label Saturday's midnight
	} {
		var got string
		if err := d.QueryRow(`SELECT `+weekOfSQL, c.at.Unix()).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != c.want || week.Of(c.at).String() != c.want {
			t.Fatalf("%s: SQL %s, week.Of %s, want %s", c.at.Format(time.RFC3339), got, week.Of(c.at), c.want)
		}
	}
	for tm := time.Date(2025, 12, 20, 0, 0, 0, 0, time.UTC); tm.Before(time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC)); tm = tm.Add(time.Hour) {
		check(tm)
		check(tm.Add(-time.Second))
	}
}

// 0013 adds track_week_subcats and puts every active sub-category in the week it was created in;
// archived ones join no week. Nothing else changes.
func TestMigrateFromV12(t *testing.T) {
	ts := func(y int, mo time.Month, d, h, mi, s int) int64 {
		return time.Date(y, mo, d, h, mi, s, 0, time.UTC).Unix()
	}
	path := oldDB(t, 12,
		`INSERT INTO users (id, email, name, role, is_admin, created_at) VALUES (1, 'admin@example.com', 'Admin', 'admin', 1, 0),
			(2, 'a@example.com', 'A', 'user', 0, 1)`,
		fmt.Sprintf(`INSERT INTO track_subcats (id, user_id, category, name, archived, position, created_at) VALUES
			(3, 2, 'active', 'Sat night', 0, 0, %d), (4, 2, 'active', 'Sun 02:59', 0, 1, %d), (5, 2, 'passive', 'Sun 03:00', 0, 0, %d),
			(6, 2, 'anki', 'Midweek', 0, 0, %d), (7, 2, 'active', 'Archived', 1, 2, %d), (8, 1, 'output', 'Admin', 0, 0, %d)`,
			ts(2026, 10, 17, 23, 0, 0), ts(2026, 10, 18, 2, 59, 59), ts(2026, 10, 18, 3, 0, 0), ts(2026, 10, 14, 12, 0, 0),
			ts(2026, 10, 14, 12, 0, 0), ts(2026, 1, 1, 0, 0, 0)),
		`INSERT INTO track_weeks (user_id, week, updated_at) VALUES (2, '2026-10-10', 5)`,
		`INSERT INTO track_hours (user_id, week, category, subcat_id, centihours) VALUES (2, '2026-10-10', 'active', 7, 100)`,
	)
	d := open(t, path)
	if got := dumpRows(t, d, `SELECT user_id, week, subcat_id FROM track_week_subcats ORDER BY subcat_id`); got !=
		"2 2026-10-10 3\n2 2026-10-10 4\n2 2026-10-17 5\n2 2026-10-10 6\n1 2025-12-27 8\n" {
		t.Fatalf("backfill:\n%s", got)
	}
	if got := dumpRows(t, d, `SELECT COUNT(*) FROM track_subcats`); got != "6\n" {
		t.Fatalf("subcats: %s", got)
	}
	// A sub-category is in a week at most once; deleting it or its user takes its memberships.
	if _, err := d.Exec(`INSERT INTO track_week_subcats (user_id, week, subcat_id) VALUES (2, '2026-10-10', 3)`); err == nil {
		t.Fatal("duplicate membership accepted")
	}
	if _, err := d.Exec(`DELETE FROM track_subcats WHERE id = 3`); err != nil {
		t.Fatal(err)
	}
	if got := dumpRows(t, d, `SELECT subcat_id FROM track_week_subcats WHERE subcat_id = 3`); got != "" {
		t.Fatalf("membership survived its sub-category: %s", got)
	}
	for _, q := range []string{`PRAGMA foreign_key_check`, `PRAGMA integrity_check`} {
		if got := dumpRows(t, d, q); got != "" && got != "ok\n" {
			t.Fatalf("%s: %s", q, got)
		}
	}
}

func TestFoldSubcats(t *testing.T) {
	ctx := context.Background()
	d := open(t, t.TempDir()+"/x.db")
	alice, err := AddUser(ctx, d, "alice@example.com", "Alice", false)
	if err != nil {
		t.Fatal(err)
	}
	bob, err := AddUser(ctx, d, "bob@example.com", "Bob", false)
	if err != nil {
		t.Fatal(err)
	}
	before := time.Unix(1000, 0)
	for _, q := range []string{
		fmt.Sprintf(`INSERT INTO track_subcats (id, user_id, category, name, archived, position, created_at) VALUES
			(10, %[1]d, 'active', 'Anime', 0, 0, 500), (11, %[1]d, 'passive', 'Podcasts', 0, 0, 600),
			(12, %[1]d, 'active', 'Old', 1, 1, 700), (13, %[1]d, 'active', 'New', 0, 2, 1000),
			(14, %[1]d, 'active', 'Newer', 0, 3, 2000), (20, %[2]d, 'active', 'Bob''s', 0, 0, 500)`, alice.ID, bob.ID),
		fmt.Sprintf(`INSERT INTO track_weeks (user_id, week, updated_at) VALUES (%[1]d, '2026-09-26', 100), (%[1]d, '2026-10-03', 100),
			(%[1]d, '2026-10-10', 100), (%[2]d, '2026-09-26', 100)`, alice.ID, bob.ID),
		fmt.Sprintf(`INSERT INTO track_hours (user_id, week, category, subcat_id, centihours) VALUES
			(%[1]d, '2026-09-26', 'active', 10, 300), (%[1]d, '2026-09-26', 'active', NULL, 100), (%[1]d, '2026-09-26', 'active', 13, 50),
			(%[1]d, '2026-09-26', 'passive', 11, 200),
			(%[1]d, '2026-10-03', 'active', 10, 150), (%[1]d, '2026-10-03', 'active', 12, 25),
			(%[1]d, '2026-10-10', 'active', 14, 75),
			(%[2]d, '2026-09-26', 'active', 20, 500)`, alice.ID, bob.ID),
		fmt.Sprintf(`INSERT INTO track_week_subcats (user_id, week, subcat_id) VALUES (%[1]d, '2026-09-26', 10), (%[1]d, '2026-10-03', 10),
			(%[1]d, '2026-09-26', 13), (%[1]d, '2026-10-17', 11), (%[2]d, '2026-09-26', 20)`, alice.ID, bob.ID),
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	all := []string{
		`SELECT * FROM track_subcats ORDER BY id`, `SELECT * FROM track_hours ORDER BY user_id, week, category, IFNULL(subcat_id, 0)`,
		`SELECT * FROM track_week_subcats ORDER BY user_id, week, subcat_id`, `SELECT * FROM track_weeks ORDER BY user_id, week`,
	}
	snapshot := func() string {
		var b strings.Builder
		for _, q := range all {
			b.WriteString(dumpRows(t, d, q))
		}
		return b.String()
	}
	totals := `SELECT user_id, week, category, SUM(centihours) FROM track_hours GROUP BY user_id, week, category ORDER BY user_id, week, category`
	wantTotals := dumpRows(t, d, totals)
	describe := func(fs []FoldedSubcat) string {
		var out []string
		for _, f := range fs {
			out = append(out, fmt.Sprintf("%d %s %s %s %d %d", f.ID, f.Category, f.Name, f.Created.Format(time.RFC3339), f.Weeks, f.Centihours))
		}
		return strings.Join(out, "\n")
	}
	want := "10 active Anime 1970-01-01T00:08:20Z 2 450\n12 active Old 1970-01-01T00:11:40Z 1 25\n11 passive Podcasts 1970-01-01T00:10:00Z 1 200"

	// Dry run: reports, changes nothing.
	start := snapshot()
	fs, err := FoldSubcats(ctx, d, alice.ID, before, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := describe(fs); got != want {
		t.Fatalf("dry run report:\n%s\nwant:\n%s", got, want)
	}
	if snapshot() != start {
		t.Fatal("dry run changed the database")
	}

	fs, err = FoldSubcats(ctx, d, alice.ID, before, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := describe(fs); got != want {
		t.Fatalf("apply report:\n%s\nwant:\n%s", got, want)
	}
	if got := dumpRows(t, d, totals); got != wantTotals {
		t.Fatalf("category totals changed:\nbefore:\n%safter:\n%s", wantTotals, got)
	}
	// Hours went to the unassigned bucket (summed into the one already there); New/Newer (created at
	// or after before) and Bob are untouched.
	if got := dumpRows(t, d, fmt.Sprintf(`SELECT week, category, subcat_id, centihours FROM track_hours WHERE user_id = %d
		ORDER BY week, category, IFNULL(subcat_id, 0)`, alice.ID)); got !=
		"2026-09-26 active <nil> 400\n2026-09-26 active 13 50\n2026-09-26 passive <nil> 200\n2026-10-03 active <nil> 175\n2026-10-10 active 14 75\n" {
		t.Fatalf("alice's hours:\n%s", got)
	}
	if got := dumpRows(t, d, `SELECT id FROM track_subcats ORDER BY id`); got != "13\n14\n20\n" {
		t.Fatalf("subcats left:\n%s", got)
	}
	if got := dumpRows(t, d, `SELECT user_id, week, subcat_id FROM track_week_subcats ORDER BY user_id, week, subcat_id`); got !=
		fmt.Sprintf("%d 2026-09-26 13\n%d 2026-09-26 20\n", alice.ID, bob.ID) {
		t.Fatalf("memberships left:\n%s", got)
	}
	if got := dumpRows(t, d, `SELECT user_id, subcat_id, centihours FROM track_hours WHERE user_id != `+fmt.Sprint(alice.ID)); got !=
		fmt.Sprintf("%d 20 500\n", bob.ID) {
		t.Fatalf("bob's hours:\n%s", got)
	}
	// Weeks whose hours moved get a new updated_at; the others keep theirs.
	if got := dumpRows(t, d, `SELECT user_id, week, updated_at > 100 FROM track_weeks ORDER BY user_id, week`); got !=
		fmt.Sprintf("%[1]d 2026-09-26 1\n%[1]d 2026-10-03 1\n%[1]d 2026-10-10 0\n%[2]d 2026-09-26 0\n", alice.ID, bob.ID) {
		t.Fatalf("updated_at:\n%s", got)
	}
	for _, q := range []string{`PRAGMA foreign_key_check`, `PRAGMA integrity_check`} {
		if got := dumpRows(t, d, q); got != "" && got != "ok\n" {
			t.Fatalf("%s: %s", q, got)
		}
	}

	// Again: nothing left to fold.
	end := snapshot()
	if fs, err = FoldSubcats(ctx, d, alice.ID, before, true); err != nil || len(fs) != 0 {
		t.Fatalf("second fold: %v %v", fs, err)
	}
	if snapshot() != end {
		t.Fatal("an empty fold changed the database")
	}
}
