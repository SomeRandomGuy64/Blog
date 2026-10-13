package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
)

// latestVersion is the number of the newest migration.
const latestVersion = 12

func open(t *testing.T, path string) *sql.DB {
	t.Helper()
	d, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestOpenMigratePragmas(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.db")
	open(t, path)
	d := open(t, path) // reopening is a no-op migration
	var version, fk, busy int
	var journal string
	d.QueryRow("PRAGMA user_version").Scan(&version)
	d.QueryRow("PRAGMA foreign_keys").Scan(&fk)
	d.QueryRow("PRAGMA busy_timeout").Scan(&busy)
	d.QueryRow("PRAGMA journal_mode").Scan(&journal)
	if version != latestVersion || fk != 1 || busy != 5000 || journal != "wal" {
		t.Fatalf("user_version=%d foreign_keys=%d busy_timeout=%d journal=%s", version, fk, busy, journal)
	}
	if _, err := d.Exec(`INSERT INTO sessions (hash, user_id, created_at, expires_at, last_seen) VALUES (x'00', 999, 0, 0, 0)`); err == nil {
		t.Fatal("foreign key not enforced")
	}
}

// oldDB builds a database the way an older binary left it: migrations 1..version applied by
// hand, then the given statements (existing data) run.
func oldDB(t *testing.T, version int, data ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "old.db")
	raw, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	names, _ := fs.Glob(migrations, "migrations/*.sql")
	sort.Strings(names)
	for _, name := range names[:version] {
		body, _ := migrations.ReadFile(name)
		if _, err := raw.Exec(string(body)); err != nil {
			t.Fatal(name, err)
		}
	}
	for _, q := range append([]string{fmt.Sprintf("PRAGMA user_version = %d", version)}, data...) {
		if _, err := raw.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	return path
}

func TestMigrateFromV1(t *testing.T) {
	d := open(t, oldDB(t, 1, `INSERT INTO users (email, name, is_admin, created_at) VALUES ('a@example.com', 'A', 1, 0)`))
	var version, users int
	d.QueryRow("PRAGMA user_version").Scan(&version)
	d.QueryRow("SELECT COUNT(*) FROM users").Scan(&users)
	if version != latestVersion || users != 1 {
		t.Fatalf("user_version=%d users=%d", version, users)
	}
	if _, err := d.Exec(`INSERT INTO avatars (user_id, data, updated_at) VALUES (1, x'ff', 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO avatars (user_id, data, updated_at) VALUES (99, x'ff', 1)`); err == nil {
		t.Fatal("avatars.user_id foreign key not enforced")
	}
}

// Production is at version 2 with real data; 0003 must apply on top without touching it.
func TestMigrateFromV2(t *testing.T) {
	d := open(t, oldDB(t, 2,
		`INSERT INTO users (email, name, is_admin, created_at) VALUES ('a@example.com', 'A', 1, 0), ('b@example.com', 'B', 0, 0)`,
		`INSERT INTO pages (path, title) VALUES ('/articles/x/', 'X')`,
		`INSERT INTO comments (page, user_id, body, created_at) VALUES ('/articles/x/', 2, 'hi', 0)`,
		`INSERT INTO comment_likes (comment_id, user_id, created_at) VALUES (1, 1, 0)`,
		`INSERT INTO avatars (user_id, data, updated_at) VALUES (2, x'ffd8', 5)`,
		`INSERT INTO kyotsu_sessions (user_id, started_at, ended_at, layout, total, correct, answers_json) VALUES (2, 0, 1, 'kotu', 10, 9, '[]')`))
	var version, public, comments, likes, avatars, kyotsu int
	d.QueryRow("PRAGMA user_version").Scan(&version)
	d.QueryRow("SELECT SUM(track_public) FROM users").Scan(&public)
	d.QueryRow("SELECT COUNT(*) FROM comments").Scan(&comments)
	d.QueryRow("SELECT COUNT(*) FROM comment_likes").Scan(&likes)
	d.QueryRow("SELECT COUNT(*) FROM avatars").Scan(&avatars)
	d.QueryRow("SELECT COUNT(*) FROM kyotsu_sessions").Scan(&kyotsu)
	if version != latestVersion || public != 2 || comments != 1 || likes != 1 || avatars != 1 || kyotsu != 1 {
		t.Fatalf("version=%d public=%d comments=%d likes=%d avatars=%d kyotsu=%d", version, public, comments, likes, avatars, kyotsu)
	}
	for _, q := range []string{
		`INSERT INTO track_subcats (user_id, category, name, created_at) VALUES (2, 'active', 'YouTube', 0)`,
		`INSERT INTO track_weeks (user_id, week, updated_at) VALUES (2, '2026-10-10', 0)`,
		`INSERT INTO track_hours (user_id, week, category, subcat_id, centihours) VALUES (2, '2026-10-10', 'active', 1, 450)`,
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	for _, bad := range []string{
		`INSERT INTO track_subcats (user_id, category, name, created_at) VALUES (2, 'active', 'youtube', 0)`, // dup, case-insensitive
		`INSERT INTO track_subcats (user_id, category, name, created_at) VALUES (2, 'gaming', 'X', 0)`,
		`INSERT INTO track_hours (user_id, week, category, centihours) VALUES (2, '2026-10-03', 'active', 10)`, // no week row
		`INSERT INTO track_hours (user_id, week, category, subcat_id, centihours) VALUES (2, '2026-10-10', 'active', 1, 5)`,
		`DELETE FROM track_subcats WHERE id = 1`, // still has hours
	} {
		if _, err := d.Exec(bad); err == nil {
			t.Fatalf("accepted: %s", bad)
		}
	}
	// An archived name no longer blocks a new one; deleting the week takes its hours.
	d.Exec(`UPDATE track_subcats SET archived = 1 WHERE id = 1`)
	if _, err := d.Exec(`INSERT INTO track_subcats (user_id, category, name, created_at) VALUES (2, 'active', 'youtube', 0)`); err != nil {
		t.Fatal(err)
	}
	d.Exec(`DELETE FROM track_weeks WHERE user_id = 2`)
	var hours int
	d.QueryRow("SELECT COUNT(*) FROM track_hours").Scan(&hours)
	if hours != 0 {
		t.Fatal("hours survived their week")
	}
}

// dumpRows renders every row of q, one line per row, for before/after comparisons.
func dumpRows(t *testing.T, d *sql.DB, q string) string {
	t.Helper()
	rows, err := d.Query(q)
	if err != nil {
		t.Fatal(q, err)
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	var b strings.Builder
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintln(&b, vals...)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// Production is at version 3 with real Track data; 0004 rebuilds track_subcats and track_hours
// to allow 'anki' and must keep every row, id, index and foreign key.
func TestMigrateFromV3(t *testing.T) {
	path := oldDB(t, 3,
		`INSERT INTO users (email, name, is_admin, created_at) VALUES ('a@example.com', 'A', 1, 0), ('b@example.com', 'B', 0, 0), ('c@example.com', 'C', 0, 0)`,
		// ids with gaps, an archived duplicate name, several categories
		`INSERT INTO track_subcats (id, user_id, category, name, archived, position, created_at) VALUES
			(3, 2, 'active', 'YouTube', 0, 0, 10), (4, 2, 'active', 'youtube', 1, 1, 11), (7, 2, 'passive', 'Podcasts', 0, 0, 12),
			(9, 2, 'soroban', 'Drills', 0, 2, 13), (12, 1, 'phonetic', 'Pitch', 0, 0, 14), (13, 3, 'output', 'Diary', 0, 0, 15)`,
		`INSERT INTO track_weeks (user_id, week, kotu, summary, summary_private, updated_at) VALUES
			(2, '2026-10-03', '[90,null,null,null,null,null,95.5]', 'first', 0, 100), (2, '2026-10-10', '[null,null,null,null,null,null,null]', 'second', 1, 200),
			(1, '2026-10-10', '[null,null,null,null,null,null,null]', '', 0, 300), (3, '2026-10-10', '[null,null,null,null,null,null,null]', 'c', 0, 400)`,
		`INSERT INTO track_hours (user_id, week, category, subcat_id, centihours) VALUES
			(2, '2026-10-03', 'active', 3, 450), (2, '2026-10-03', 'active', 4, 100), (2, '2026-10-03', 'active', NULL, 25),
			(2, '2026-10-10', 'passive', 7, 1000), (2, '2026-10-10', 'soroban', 9, 16800), (1, '2026-10-10', 'phonetic', 12, 1),
			(1, '2026-10-10', 'output', NULL, 300), (3, '2026-10-10', 'output', 13, 200)`,
		`DELETE FROM track_hours WHERE centihours = 100`, // leaves a rowid gap
	)
	queries := []string{
		`SELECT id, user_id, category, name, archived, position, created_at FROM track_subcats ORDER BY id`,
		`SELECT rowid, user_id, week, category, subcat_id, centihours FROM track_hours ORDER BY rowid`,
		`SELECT * FROM track_weeks ORDER BY user_id, week`,
		`SELECT id, email, track_public, bio FROM users ORDER BY id`,
	}
	before := make([]string, len(queries))
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for i, q := range queries {
		before[i] = dumpRows(t, raw, q)
	}
	raw.Close()
	if !strings.Contains(before[0], "YouTube") || strings.Count(before[1], "\n") != 7 {
		t.Fatalf("fixture not as expected:\n%s\n%s", before[0], before[1])
	}

	d := open(t, path)
	var version int
	d.QueryRow("PRAGMA user_version").Scan(&version)
	if version != latestVersion {
		t.Fatalf("user_version=%d", version)
	}
	for i, q := range queries {
		if after := dumpRows(t, d, q); after != before[i] {
			t.Fatalf("%s changed:\nbefore:\n%safter:\n%s", q, before[i], after)
		}
	}
	if got := dumpRows(t, d, `PRAGMA foreign_key_check`); got != "" {
		t.Fatalf("foreign_key_check: %s", got)
	}
	if got := dumpRows(t, d, `PRAGMA integrity_check`); got != "ok\n" {
		t.Fatalf("integrity_check: %s", got)
	}
	if got := dumpRows(t, d, `SELECT name FROM sqlite_master WHERE tbl_name IN ('track_subcats', 'track_hours') AND name NOT LIKE 'sqlite_%' ORDER BY name`); got !=
		"track_hours\ntrack_hours_key\ntrack_hours_subcat\ntrack_subcats\ntrack_subcats_name\ntrack_subcats_user\n" {
		t.Fatalf("schema objects:\n%s", got)
	}
	if got := dumpRows(t, d, `SELECT name FROM sqlite_master WHERE name LIKE '%_new'`); got != "" {
		t.Fatalf("leftover tables: %s", got)
	}
	if got := dumpRows(t, d, `SELECT "table", "from", "to", on_delete FROM pragma_foreign_key_list('track_hours') ORDER BY id, seq`); !strings.Contains(got, "track_subcats subcat_id id NO ACTION") ||
		!strings.Contains(got, "track_weeks user_id user_id CASCADE") || !strings.Contains(got, "track_weeks week week CASCADE") {
		t.Fatalf("track_hours foreign keys:\n%s", got)
	}

	// 'anki' is now a category, for sub-categories and hours alike; other strings still aren't.
	for _, q := range []string{
		`INSERT INTO track_subcats (user_id, category, name, created_at) VALUES (2, 'anki', 'Core 2k', 0)`,
		`INSERT INTO track_hours (user_id, week, category, subcat_id, centihours) VALUES (2, '2026-10-10', 'anki', last_insert_rowid(), 150)`,
		`INSERT INTO track_hours (user_id, week, category, centihours) VALUES (2, '2026-10-10', 'anki', 50)`,
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	for _, bad := range []string{
		`INSERT INTO track_subcats (user_id, category, name, created_at) VALUES (2, 'gaming', 'X', 0)`,
		`INSERT INTO track_hours (user_id, week, category, centihours) VALUES (2, '2026-10-10', 'gaming', 10)`,
		`INSERT INTO track_subcats (user_id, category, name, created_at) VALUES (2, 'active', 'YOUTUBE', 0)`,                 // dup, case-insensitive
		`INSERT INTO track_hours (user_id, week, category, subcat_id, centihours) VALUES (2, '2026-10-10', 'passive', 7, 5)`, // dup key
		`INSERT INTO track_hours (user_id, week, category, centihours) VALUES (2, '2026-09-26', 'anki', 10)`,                 // no week row
		`INSERT INTO track_hours (user_id, week, category, subcat_id, centihours) VALUES (2, '2026-10-10', 'anki', 999, 10)`, // no subcat
		`INSERT INTO track_hours (user_id, week, category, centihours) VALUES (1, '2026-10-10', 'anki', 16801)`,
		`DELETE FROM track_subcats WHERE id = 3`, // still has hours
		`INSERT INTO track_subcats (user_id, category, name, created_at) VALUES (99, 'anki', 'X', 0)`,
	} {
		if _, err := d.Exec(bad); err == nil {
			t.Fatalf("accepted: %s", bad)
		}
	}
	// Cascades still work: deleting a week takes its hours, deleting a user takes the rest.
	d.Exec(`DELETE FROM track_weeks WHERE user_id = 2 AND week = '2026-10-10'`)
	var hours, subcats int
	d.QueryRow(`SELECT COUNT(*) FROM track_hours WHERE user_id = 2`).Scan(&hours)
	if hours != 2 {
		t.Fatalf("user 2 has %d hour rows after deleting a week", hours)
	}
	if _, err := d.Exec(`DELETE FROM users WHERE id = 2`); err != nil {
		t.Fatal(err)
	}
	d.QueryRow(`SELECT (SELECT COUNT(*) FROM track_hours WHERE user_id = 2) + (SELECT COUNT(*) FROM track_subcats WHERE user_id = 2)`).Scan(&subcats)
	if subcats != 0 {
		t.Fatal("user 2's track data survived deleting the user")
	}
}

func TestUsers(t *testing.T) {
	ctx := context.Background()
	d := open(t, filepath.Join(t.TempDir(), "x.db"))
	for i := 0; i < 2; i++ {
		if err := EnsureAdmin(ctx, d, "Admin@Example.com", "Faizan"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := AddUser(ctx, d, "admin@example.COM", "Dup", false); !errors.Is(err, ErrExists) {
		t.Fatalf("duplicate add: %v", err)
	}
	if _, err := AddUser(ctx, d, "bob@example.com", "  ", false); err == nil {
		t.Fatal("blank name accepted")
	}
	if _, err := AddUser(ctx, d, "bob@example.com", strings.Repeat("名", 41), false); err == nil {
		t.Fatal("41-rune name accepted")
	}
	if _, err := AddUser(ctx, d, "bob@example.com", "Bo\x00b", false); err == nil {
		t.Fatal("control character accepted")
	}
	if _, err := AddUser(ctx, d, "Bob <bob@example.com>", "Bob", false); err == nil {
		t.Fatal("display-name address accepted")
	}
	bob, err := AddUser(ctx, d, " bob@example.com ", "Bob", false)
	if err != nil || bob.Email != "bob@example.com" || bob.Admin || bob.Role != RoleUser {
		t.Fatalf("add: %+v %v", bob, err)
	}
	if carol, err := AddUser(ctx, d, "carol@example.com", "Carol", true); err != nil || carol.Role != RoleAdmin || !carol.Admin {
		t.Fatalf("add admin: %+v %v", carol, err)
	}
	// EnsureAdmin promotes (role and the legacy is_admin) but never renames.
	d.Exec(`UPDATE users SET name = 'Renamed', role = 'moderator', is_admin = 0 WHERE id = 1`)
	EnsureAdmin(ctx, d, "admin@example.com", "Faizan")
	u, err := UserByEmail(ctx, d, "ADMIN@example.com")
	if err != nil || u.Name != "Renamed" || !u.Admin || u.Role != RoleAdmin {
		t.Fatalf("admin %+v %v", u, err)
	}
	if got := dumpRows(t, d, `SELECT id, role, is_admin FROM users ORDER BY id`); got != "1 admin 1\n2 user 0\n3 admin 1\n" {
		t.Fatalf("roles:\n%s", got)
	}
	// The deleted-account placeholder (GhostID) is never found or listed.
	d.Exec(`INSERT INTO users (id, email, name, created_at) VALUES (0, '', '[deleted]', 0)`)
	if _, err := UserByEmail(ctx, d, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ghost found by email: %v", err)
	}
	d.Exec(`DELETE FROM users WHERE id = 3`)
	if _, err := UserByEmail(ctx, d, "nobody@example.com"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing user: %v", err)
	}
	users, _ := ListUsers(ctx, d)
	if len(users) != 2 {
		t.Fatalf("users %+v", users)
	}
}

func TestPurge(t *testing.T) {
	ctx := context.Background()
	d := open(t, filepath.Join(t.TempDir(), "x.db"))
	EnsureAdmin(ctx, d, "a@example.com", "A")
	now := time.Now()
	CreateLoginToken(ctx, d, 1, "/", now.Add(-time.Hour), 15*time.Minute)
	CreateLoginToken(ctx, d, 1, "/", now, 15*time.Minute)
	CreateSignupToken(ctx, d, "new@example.com", "New", "/", now.Add(-2*time.Hour))
	CreateSignupToken(ctx, d, "new@example.com", "New", "/", now)
	d.Exec(`INSERT INTO sessions VALUES (x'01', 1, 0, ?, 0), (x'02', 1, 0, ?, 0)`, now.Add(-time.Second).Unix(), now.Add(time.Hour).Unix())
	tok, sess, err := Purge(ctx, d, now)
	if err != nil || tok != 2 || sess != 1 {
		t.Fatalf("purged %d tokens %d sessions, err %v", tok, sess, err)
	}
	var left int
	d.QueryRow(`SELECT (SELECT COUNT(*) FROM login_tokens) + (SELECT COUNT(*) FROM signup_tokens)`).Scan(&left)
	if left != 2 {
		t.Fatalf("%d live tokens left, want 2", left)
	}
}

func TestBackup(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	d := open(t, filepath.Join(dir, "fazon.db"))
	EnsureAdmin(ctx, d, "a@example.com", "A")
	bdir := filepath.Join(dir, "backups")
	os.MkdirAll(bdir, 0o750)
	touch := func(name string) { os.WriteFile(filepath.Join(bdir, name), []byte("x"), 0o600) }
	for i := 1; i <= 20; i++ { // older compressed backups to rotate out
		touch(time.Date(2026, 9, i, 0, 0, 0, 0, time.UTC).Format("fazon-2006-01-02.db.zst"))
	}
	for _, name := range []string{
		"fazon-2026-10-10.db", "fazon-2026-10-11.db", // legacy uncompressed, incl. today's
		"fazon-2026-10-12.db",                                                                 // legacy dated after the newest .zst: kept
		".fazon-2026-10-09.db.tmp", ".fazon-2026-10-09.db.zst.tmp", "fazon-2026-10-01.db.tmp", // crash leftovers
		"notes.txt",
	} {
		touch(name)
	}
	now := time.Date(2026, 10, 11, 23, 30, 0, 0, time.UTC)
	path, err := Backup(ctx, d, bdir, now, 14)
	if err != nil || filepath.Base(path) != "fazon-2026-10-11.db.zst" {
		t.Fatalf("backup %q %v", path, err)
	}
	if path, err := Backup(ctx, d, bdir, now, 14); err != nil || path != "" {
		t.Fatalf("second backup same day: %q %v", path, err)
	}
	var left []string
	entries, _ := os.ReadDir(bdir)
	for _, e := range entries {
		left = append(left, e.Name())
	}
	want := []string{"fazon-2026-09-08.db.zst"}
	for i := 9; i <= 20; i++ {
		want = append(want, fmt.Sprintf("fazon-2026-09-%02d.db.zst", i))
	}
	want = append(want, "fazon-2026-10-11.db.zst", "fazon-2026-10-12.db", "notes.txt")
	if strings.Join(left, " ") != strings.Join(want, " ") {
		t.Fatalf("left %v\nwant %v", left, want)
	}

	// The backup is a valid zstd stream of a working database.
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := zstd.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	restored := filepath.Join(t.TempDir(), "restored.db")
	out, _ := os.Create(restored)
	if _, err := io.Copy(out, zr); err != nil {
		t.Fatal(err)
	}
	out.Close()
	b := open(t, restored)
	var n int
	if err := b.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("backup users %d %v", n, err)
	}
}

// Production is at version 4 with real Track data; 0005 only adds tables (ratings, goals,
// settings), so every existing row and schema object must come through untouched.
func TestMigrateFromV4(t *testing.T) {
	path := oldDB(t, 4,
		`INSERT INTO users (email, name, is_admin, created_at) VALUES ('a@example.com', 'A', 1, 0), ('b@example.com', 'B', 0, 0)`,
		`INSERT INTO track_subcats (id, user_id, category, name, created_at) VALUES (5, 2, 'anki', 'Core', 1), (6, 2, 'active', 'YouTube', 2)`,
		`INSERT INTO track_weeks (user_id, week, kotu, summary, summary_private, updated_at) VALUES
			(2, '2026-10-03', '[90,null,null,null,null,null,95.5]', 'first', 0, 100), (2, '2026-10-10', '[null,null,null,null,null,null,null]', 's', 1, 200)`,
		`INSERT INTO track_hours (user_id, week, category, subcat_id, centihours) VALUES
			(2, '2026-10-03', 'anki', 5, 150), (2, '2026-10-03', 'active', 6, 450), (2, '2026-10-10', 'passive', NULL, 1000)`,
	)
	queries := []string{
		`SELECT id, user_id, category, name, archived, position, created_at FROM track_subcats ORDER BY id`,
		`SELECT rowid, user_id, week, category, subcat_id, centihours FROM track_hours ORDER BY rowid`,
		`SELECT * FROM track_weeks ORDER BY user_id, week`,
		`SELECT id, email, track_public, bio FROM users ORDER BY id`,
		`SELECT type, name, tbl_name, sql FROM sqlite_master WHERE name != 'users' AND tbl_name != 'notifications' ORDER BY name`, // users gains columns in 0007; 0010 rebuilds notifications (TestMigrateFromV9)
	}
	before := make([]string, len(queries))
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for i, q := range queries {
		before[i] = dumpRows(t, raw, q)
	}
	raw.Close()

	d := open(t, path)
	var version int
	d.QueryRow("PRAGMA user_version").Scan(&version)
	if version != latestVersion {
		t.Fatalf("user_version=%d", version)
	}
	for i, q := range queries[:4] {
		if after := dumpRows(t, d, q); after != before[i] {
			t.Fatalf("%s changed:\nbefore:\n%safter:\n%s", q, before[i], after)
		}
	}
	// The old schema objects are all still there, unchanged; only new tables were added.
	after := dumpRows(t, d, queries[4])
	for _, line := range strings.SplitAfter(before[4], "\n") {
		if !strings.Contains(after, line) {
			t.Fatalf("schema object changed or lost: %s", line)
		}
	}
	for _, tbl := range []string{"track_ratings", "track_goals", "settings"} {
		if !strings.Contains(after, "table "+tbl+" "+tbl) {
			t.Fatalf("no %s table:\n%s", tbl, after)
		}
	}
	for _, q := range []string{`PRAGMA foreign_key_check`, `PRAGMA integrity_check`} {
		if got := dumpRows(t, d, q); got != "" && got != "ok\n" {
			t.Fatalf("%s: %s", q, got)
		}
	}

	for _, q := range []string{
		`INSERT INTO track_ratings (user_id, week, category, rating) VALUES (2, '2026-10-03', 'active', 1), (2, '2026-10-03', 'anki', 4), (2, '2026-10-10', 'passive', 3)`,
		`INSERT INTO track_goals (user_id, week, category, centihours, created_at) VALUES (2, '2026-10-10', 'active', 3500, 0), (2, '2026-10-17', 'anki', 16800, 0)`,
		`INSERT INTO settings (key, value) VALUES ('track_ended_week', '2026-10-10')`,
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	for _, bad := range []string{
		`INSERT INTO track_ratings (user_id, week, category, rating) VALUES (2, '2026-10-03', 'passive', 0)`,
		`INSERT INTO track_ratings (user_id, week, category, rating) VALUES (2, '2026-10-03', 'passive', 5)`,
		`INSERT INTO track_ratings (user_id, week, category, rating) VALUES (2, '2026-10-03', 'gaming', 2)`,
		`INSERT INTO track_ratings (user_id, week, category, rating) VALUES (2, '2026-10-03', 'active', 2)`,                    // dup
		`INSERT INTO track_ratings (user_id, week, category, rating) VALUES (2, '2026-09-26', 'active', 2)`,                    // no week row
		`INSERT INTO track_goals (user_id, week, category, centihours, created_at) VALUES (2, '2026-10-10', 'active', 100, 0)`, // dup
		`INSERT INTO track_goals (user_id, week, category, centihours, created_at) VALUES (2, '2026-10-10', 'output', 0, 0)`,
		`INSERT INTO track_goals (user_id, week, category, centihours, created_at) VALUES (2, '2026-10-10', 'output', 16801, 0)`,
		`INSERT INTO track_goals (user_id, week, category, centihours, created_at) VALUES (2, '2026-10-10', 'gaming', 10, 0)`,
		`INSERT INTO track_goals (user_id, week, category, centihours, created_at) VALUES (99, '2026-10-10', 'output', 10, 0)`, // no user
		`INSERT INTO settings (key, value) VALUES ('track_ended_week', 'x')`,                                                   // dup key
	} {
		if _, err := d.Exec(bad); err == nil {
			t.Fatalf("accepted: %s", bad)
		}
	}
	// Deleting a week takes its ratings but not its goals (write-once); deleting the user takes all.
	d.Exec(`DELETE FROM track_weeks WHERE user_id = 2 AND week = '2026-10-10'`)
	var ratings, goals int
	d.QueryRow(`SELECT (SELECT COUNT(*) FROM track_ratings WHERE user_id = 2), (SELECT COUNT(*) FROM track_goals WHERE user_id = 2)`).Scan(&ratings, &goals)
	if ratings != 2 || goals != 2 {
		t.Fatalf("after deleting a week: %d ratings, %d goals", ratings, goals)
	}
	if _, err := d.Exec(`DELETE FROM users WHERE id = 2`); err != nil {
		t.Fatal(err)
	}
	d.QueryRow(`SELECT (SELECT COUNT(*) FROM track_ratings) + (SELECT COUNT(*) FROM track_goals)`).Scan(&ratings)
	if ratings != 0 {
		t.Fatal("ratings or goals survived deleting the user")
	}
}

// Production is at version 5 with real data; 0006 only adds the api_keys table, so every
// existing row and schema object must come through untouched.
func TestMigrateFromV5(t *testing.T) {
	path := oldDB(t, 5,
		`INSERT INTO users (email, name, is_admin, created_at) VALUES ('a@example.com', 'A', 1, 0), ('b@example.com', 'B', 0, 0)`,
		`INSERT INTO sessions (hash, user_id, created_at, expires_at, last_seen) VALUES (x'01', 2, 0, 9999999999, 0)`,
		`INSERT INTO track_subcats (id, user_id, category, name, created_at) VALUES (5, 2, 'anki', 'Core', 1), (6, 2, 'active', 'YouTube', 2)`,
		`INSERT INTO track_weeks (user_id, week, kotu, summary, summary_private, updated_at) VALUES
			(2, '2026-10-03', '[90,null,null,null,null,null,95.5]', 'first', 0, 100), (2, '2026-10-10', '[null,null,null,null,null,null,null]', 's', 1, 200)`,
		`INSERT INTO track_hours (user_id, week, category, subcat_id, centihours) VALUES
			(2, '2026-10-03', 'anki', 5, 150), (2, '2026-10-03', 'active', 6, 450), (2, '2026-10-10', 'passive', NULL, 1000)`,
		`INSERT INTO track_ratings (user_id, week, category, rating) VALUES (2, '2026-10-03', 'active', 3)`,
		`INSERT INTO track_goals (user_id, week, category, centihours, created_at) VALUES (2, '2026-10-10', 'active', 3500, 0)`,
		`INSERT INTO settings (key, value) VALUES ('track_ended_week', '2026-10-10')`,
	)
	queries := []string{
		`SELECT id, email, name, is_admin, track_public, bio FROM users ORDER BY id`,
		`SELECT * FROM sessions`,
		`SELECT * FROM track_subcats ORDER BY id`,
		`SELECT rowid, * FROM track_hours ORDER BY rowid`,
		`SELECT * FROM track_weeks ORDER BY user_id, week`,
		`SELECT * FROM track_ratings`,
		`SELECT * FROM track_goals`,
		`SELECT * FROM settings WHERE key != 'users_max_id'`, /* added by 0008 */
		`SELECT type, name, tbl_name, sql FROM sqlite_master WHERE name != 'users' AND tbl_name != 'notifications' ORDER BY name`, // users gains columns in 0007; 0010 rebuilds notifications (TestMigrateFromV9)
	}
	last := len(queries) - 1
	before := make([]string, len(queries))
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for i, q := range queries {
		before[i] = dumpRows(t, raw, q)
	}
	raw.Close()

	d := open(t, path)
	var version int
	d.QueryRow("PRAGMA user_version").Scan(&version)
	if version != latestVersion {
		t.Fatalf("user_version=%d", version)
	}
	for i, q := range queries[:last] {
		if after := dumpRows(t, d, q); after != before[i] {
			t.Fatalf("%s changed:\nbefore:\n%safter:\n%s", q, before[i], after)
		}
	}
	after := dumpRows(t, d, queries[last])
	for _, line := range strings.SplitAfter(before[last], "\n") {
		if !strings.Contains(after, line) {
			t.Fatalf("schema object changed or lost: %s", line)
		}
	}
	if !strings.Contains(after, "table api_keys api_keys") || !strings.Contains(after, "index api_keys_user api_keys") {
		t.Fatalf("no api_keys table/index:\n%s", after)
	}
	for _, q := range []string{`PRAGMA foreign_key_check`, `PRAGMA integrity_check`} {
		if got := dumpRows(t, d, q); got != "" && got != "ok\n" {
			t.Fatalf("%s: %s", q, got)
		}
	}

	if _, err := d.Exec(`INSERT INTO api_keys (user_id, name, prefix, hash, scope, created_at) VALUES
		(2, 'k1', 'fz_aaaa', x'aa', 'read', 0), (2, 'k2', 'fz_bbbb', x'bb', 'write', 0), (1, 'k3', 'fz_cccc', x'cc', 'write', 0)`); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		`INSERT INTO api_keys (user_id, name, prefix, hash, scope, created_at) VALUES (2, 'x', 'fz_x', x'aa', 'read', 0)`,  // dup hash
		`INSERT INTO api_keys (user_id, name, prefix, hash, scope, created_at) VALUES (2, 'x', 'fz_x', x'dd', 'admin', 0)`, // scope
		`INSERT INTO api_keys (user_id, name, prefix, hash, scope, created_at) VALUES (99, 'x', 'fz_x', x'ee', 'read', 0)`, // no user
		`INSERT INTO api_keys (user_id, name, prefix, scope, created_at) VALUES (2, 'x', 'fz_x', 'read', 0)`,               // no hash
	} {
		if _, err := d.Exec(bad); err == nil {
			t.Fatalf("accepted: %s", bad)
		}
	}
	// Deleting the user takes their keys (and only theirs).
	if _, err := d.Exec(`DELETE FROM users WHERE id = 2`); err != nil {
		t.Fatal(err)
	}
	var mine, others int
	d.QueryRow(`SELECT COUNT(*) FILTER (WHERE user_id = 2), COUNT(*) FILTER (WHERE user_id = 1) FROM api_keys`).Scan(&mine, &others)
	if mine != 0 || others != 1 {
		t.Fatalf("after deleting the user: %d of their keys, %d others", mine, others)
	}
}

// Production is at version 6 with real data; 0007 adds role/muted_at/muted_by to users and the
// signup_tokens table. It must keep every row, id, index and foreign key, and turn admins into
// role 'admin' and everyone else into 'user'.
func TestMigrateFromV6(t *testing.T) {
	path := oldDB(t, 6,
		`INSERT INTO users (email, name, is_admin, created_at, bio) VALUES ('a@example.com', 'A', 1, 0, 'hi'), ('b@example.com', 'B', 0, 5, ''),
			('c@example.com', 'C', 0, 6, '')`,
		`INSERT INTO sessions (hash, user_id, created_at, expires_at, last_seen) VALUES (x'01', 2, 0, 9999999999, 0)`,
		`INSERT INTO login_tokens (hash, user_id, next, created_at, expires_at) VALUES (x'02', 3, '/', 0, 9999999999)`,
		`INSERT INTO pages (path, title) VALUES ('/articles/x/', 'X')`,
		`INSERT INTO comments (page, user_id, body, created_at) VALUES ('/articles/x/', 2, 'hi', 0)`,
		`INSERT INTO comments (page, parent_id, user_id, body, created_at) VALUES ('/articles/x/', 1, 3, 'yo', 1)`,
		`INSERT INTO comment_likes (comment_id, user_id, created_at) VALUES (1, 3, 0)`,
		`INSERT INTO notifications (user_id, kind, actor_id, comment_id, created_at) VALUES (2, 'reply', 3, 2, 1)`,
		`INSERT INTO avatars (user_id, data, updated_at) VALUES (2, x'ffd8', 5)`,
		`INSERT INTO kyotsu_sessions (user_id, started_at, ended_at, layout, total, correct, answers_json) VALUES (2, 0, 1, 'kotu', 10, 9, '[]')`,
		`INSERT INTO track_subcats (id, user_id, category, name, created_at) VALUES (5, 2, 'anki', 'Core', 1)`,
		`INSERT INTO track_weeks (user_id, week, summary, updated_at) VALUES (2, '2026-10-03', 'first', 100)`,
		`INSERT INTO track_hours (user_id, week, category, subcat_id, centihours) VALUES (2, '2026-10-03', 'anki', 5, 150)`,
		`INSERT INTO track_ratings (user_id, week, category, rating) VALUES (2, '2026-10-03', 'anki', 3)`,
		`INSERT INTO track_goals (user_id, week, category, centihours, created_at) VALUES (2, '2026-10-03', 'anki', 100, 0)`,
		`INSERT INTO api_keys (user_id, name, prefix, hash, scope, created_at) VALUES (2, 'k', 'fz_aaaa', x'aa', 'read', 0)`,
		`INSERT INTO settings (key, value) VALUES ('track_ended_week', '2026-10-10')`,
	)
	queries := []string{
		`SELECT id, email, name, is_admin, track_public, bio, created_at FROM users ORDER BY id`,
		`SELECT * FROM sessions`, `SELECT * FROM login_tokens`, `SELECT * FROM pages`,
		`SELECT * FROM comments ORDER BY id`, `SELECT * FROM comment_likes`, `SELECT id, user_id, kind, actor_id, comment_id, created_at, read_at FROM notifications`, /* 0010 adds page */
		`SELECT * FROM avatars`, `SELECT id, user_id, started_at, ended_at, layout, advanced, total, correct, answers_json FROM kyotsu_sessions` /* 0009 adds columns */, `SELECT * FROM track_subcats`,
		`SELECT rowid, * FROM track_hours`, `SELECT * FROM track_weeks`, `SELECT * FROM track_ratings`,
		`SELECT * FROM track_goals`, `SELECT * FROM api_keys`, `SELECT * FROM settings WHERE key != 'users_max_id'`, /* added by 0008 */
		`SELECT type, name, tbl_name, sql FROM sqlite_master WHERE name != 'users' AND tbl_name != 'notifications' ORDER BY name`, /* see TestMigrateFromV9 */
	}
	last := len(queries) - 1
	before := make([]string, len(queries))
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for i, q := range queries {
		before[i] = dumpRows(t, raw, q)
	}
	raw.Close()

	d := open(t, path)
	var version int
	d.QueryRow("PRAGMA user_version").Scan(&version)
	if version != latestVersion {
		t.Fatalf("user_version=%d", version)
	}
	for i, q := range queries[:last] {
		if after := dumpRows(t, d, q); after != before[i] {
			t.Fatalf("%s changed:\nbefore:\n%safter:\n%s", q, before[i], after)
		}
	}
	after := dumpRows(t, d, queries[last])
	for _, line := range strings.SplitAfter(before[last], "\n") {
		if !strings.Contains(after, line) {
			t.Fatalf("schema object changed or lost: %s", line)
		}
	}
	for _, want := range []string{"table signup_tokens signup_tokens", "index signup_tokens_email", "index signup_tokens_expires"} {
		if !strings.Contains(after, want) {
			t.Fatalf("no %s:\n%s", want, after)
		}
	}
	if got := dumpRows(t, d, `SELECT id, role, muted_at, muted_by FROM users ORDER BY id`); got != "1 admin <nil> <nil>\n2 user <nil> <nil>\n3 user <nil> <nil>\n" {
		t.Fatalf("roles after migration:\n%s", got)
	}
	for _, q := range []string{`PRAGMA foreign_key_check`, `PRAGMA integrity_check`} {
		if got := dumpRows(t, d, q); got != "" && got != "ok\n" {
			t.Fatalf("%s: %s", q, got)
		}
	}
	for _, bad := range []string{
		`UPDATE users SET role = 'owner' WHERE id = 2`,
		`UPDATE users SET role = NULL WHERE id = 2`,
		`UPDATE users SET muted_by = 99 WHERE id = 2`, // no such user
	} {
		if _, err := d.Exec(bad); err == nil {
			t.Fatalf("accepted: %s", bad)
		}
	}
	// A mute survives the deletion of the account that gave it (muted_by is cleared).
	d.Exec(`UPDATE users SET muted_at = 1, muted_by = 3 WHERE id = 2`)
	d.Exec(`DELETE FROM comment_likes WHERE user_id = 3`)
	d.Exec(`DELETE FROM notifications`)
	d.Exec(`DELETE FROM login_tokens WHERE user_id = 3`)
	d.Exec(`DELETE FROM comments WHERE user_id = 3`)
	if _, err := d.Exec(`DELETE FROM users WHERE id = 3`); err != nil {
		t.Fatal(err)
	}
	if got := dumpRows(t, d, `SELECT muted_at, muted_by FROM users WHERE id = 2`); got != "1 <nil>\n" {
		t.Fatalf("mute after the muting account was deleted: %s", got)
	}
}

// A deleted account's id is never handed to a new account (migration 0008), even when it was
// the highest id and even across a reopen.
func TestUserIDsNeverReused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fazon.db")
	d := open(t, path)
	ctx := context.Background()
	a, err := AddUser(ctx, d, "a@example.com", "A", false)
	if err != nil {
		t.Fatal(err)
	}
	b, err := AddUser(ctx, d, "b@example.com", "B", false)
	if err != nil {
		t.Fatal(err)
	}
	del := func(id int64) {
		tx, err := d.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if err := DeleteUser(ctx, tx, id, time.Now()); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	del(b.ID) // the highest id: plain SQLite would hand it out again
	c, err := AddUser(ctx, d, "c@example.com", "C", false)
	if err != nil {
		t.Fatal(err)
	}
	if c.ID <= b.ID {
		t.Fatalf("new account got id %d, deleted account had %d", c.ID, b.ID)
	}
	del(c.ID)
	del(a.ID)
	d.Close()
	d = open(t, path)
	if err := EnsureAdmin(ctx, d, "admin@example.com", "Admin"); err != nil {
		t.Fatal(err)
	}
	var adminID int64
	if err := d.QueryRow(`SELECT id FROM users WHERE email = 'admin@example.com'`).Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	if adminID <= c.ID {
		t.Fatalf("admin got id %d after ids up to %d were deleted", adminID, c.ID)
	}
	// re-running EnsureAdmin for an existing admin keeps its id
	if err := EnsureAdmin(ctx, d, "admin@example.com", "Admin"); err != nil {
		t.Fatal(err)
	}
	var again int64
	d.QueryRow(`SELECT id FROM users WHERE email = 'admin@example.com'`).Scan(&again)
	if again != adminID {
		t.Fatalf("EnsureAdmin changed the id %d → %d", adminID, again)
	}
}

// Production is at version 8; 0009 only adds kyotsu_sessions columns (practice time, what it
// added to Track, the trainer's client id) with defaults that mean "nothing", and a partial
// unique index on (user_id, client_id). Existing sessions must come through unchanged.
func TestMigrateFromV8(t *testing.T) {
	path := oldDB(t, 8,
		`INSERT INTO users (id, email, name, role, created_at) VALUES (2, 'a@example.com', 'A', 'user', 0)`,
		`INSERT INTO kyotsu_sessions (user_id, started_at, ended_at, layout, advanced, total, correct, answers_json) VALUES (2, 10, 70, 'kotu', 1, 3, 2, '[{"ok":true}]')`,
		`INSERT INTO kyotsu_sessions (user_id, started_at, ended_at, layout, total, correct, answers_json) VALUES (2, 80, 90, 'classic', 1, 0, '[]')`,
	)
	d := open(t, path)
	var version int
	d.QueryRow("PRAGMA user_version").Scan(&version)
	if version != latestVersion {
		t.Fatalf("user_version=%d", version)
	}
	got := dumpRows(t, d, `SELECT id, user_id, started_at, ended_at, layout, advanced, total, correct, answers_json,
		active_seconds, track_centihours, track_subcat, track_note, client_id FROM kyotsu_sessions ORDER BY id`)
	want := "1 2 10 70 kotu 1 3 2 [{\"ok\":true}] 0 0 <nil>  <nil>\n2 2 80 90 classic 0 1 0 [] 0 0 <nil>  <nil>\n"
	if got != want {
		t.Fatalf("sessions after migration:\n%s\nwant:\n%s", got, want)
	}
	// client_id is unique per user; NULL (older trainers) any number of times.
	ins := `INSERT INTO kyotsu_sessions (user_id, started_at, ended_at, layout, total, correct, answers_json, client_id) VALUES (2, 0, 1, 'kotu', 1, 1, '[]', ?)`
	for _, id := range []any{nil, nil, "abcdefgh"} {
		if _, err := d.Exec(ins, id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.Exec(ins, "abcdefgh"); err == nil {
		t.Fatal("duplicate client_id accepted")
	}
	for _, q := range []string{`PRAGMA foreign_key_check`, `PRAGMA integrity_check`} {
		if got := dumpRows(t, d, q); got != "" && got != "ok\n" {
			t.Fatalf("%s: %s", q, got)
		}
	}
}

// Production is at version 9; 0010 adds page_likes (article likes) and rebuilds notifications so
// an article like can notify ('page_like': a page, no comment). Every notification must come
// through with its id, read state and timestamps, the old indexes must be back under the same
// names and definitions, and nothing else in the schema may change.
func TestMigrateFromV9(t *testing.T) {
	path := oldDB(t, 9,
		`INSERT INTO users (id, email, name, role, is_admin, created_at) VALUES (1, 'admin@example.com', 'Admin', 'admin', 1, 0),
			(2, 'a@example.com', 'A', 'user', 0, 1), (3, 'b@example.com', 'B', 'moderator', 0, 2)`,
		`INSERT INTO pages (path, title) VALUES ('/articles/x/', 'X'), ('/articles/y/', 'Y')`,
		`INSERT INTO comments (id, page, user_id, body, created_at) VALUES (1, '/articles/x/', 2, 'hi', 10), (5, '/articles/y/', 1, 'admin here', 11)`,
		`INSERT INTO comments (id, page, parent_id, user_id, body, created_at) VALUES (7, '/articles/x/', 1, 3, 'yo', 12)`,
		`INSERT INTO comment_likes (comment_id, user_id, created_at) VALUES (1, 3, 13), (5, 2, 14)`,
		// ids with a gap; read and unread; all three kinds
		`INSERT INTO notifications (id, user_id, kind, actor_id, comment_id, created_at, read_at) VALUES
			(3, 1, 'comment', 2, 1, 10, 20), (4, 2, 'reply', 3, 7, 12, NULL), (9, 2, 'like', 3, 1, 13, NULL), (10, 1, 'like', 2, 5, 14, 15)`,
	)
	queries := []string{
		`SELECT id, user_id, kind, actor_id, comment_id, created_at, read_at FROM notifications ORDER BY id`,
		// (users and kyotsu_sessions: as they were before 0011 added a column to each)
		`SELECT id, email, name, is_admin, created_at, track_public, bio, role, muted_at, muted_by FROM users ORDER BY id`,
		`SELECT * FROM pages`, `SELECT * FROM comments ORDER BY id`, `SELECT * FROM comment_likes`,
		`SELECT type, name, tbl_name, sql FROM sqlite_master WHERE tbl_name != 'notifications' AND name NOT IN ('users', 'kyotsu_sessions') ORDER BY name`,
		`SELECT type, name, sql FROM sqlite_master WHERE tbl_name = 'notifications' AND type = 'index' ORDER BY name`,
	}
	before := make([]string, len(queries))
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for i, q := range queries {
		before[i] = dumpRows(t, raw, q)
	}
	raw.Close()
	if strings.Count(before[0], "\n") != 4 {
		t.Fatalf("fixture not as expected:\n%s", before[0])
	}

	d := open(t, path)
	var version int
	d.QueryRow("PRAGMA user_version").Scan(&version)
	if version != latestVersion {
		t.Fatalf("user_version=%d", version)
	}
	for i, q := range queries[:5] {
		if after := dumpRows(t, d, q); after != before[i] {
			t.Fatalf("%s changed:\nbefore:\n%safter:\n%s", q, before[i], after)
		}
	}
	// Every other schema object is untouched, and the old notification indexes are back as they were.
	for _, i := range []int{5, 6} {
		after := dumpRows(t, d, queries[i])
		for _, line := range strings.SplitAfter(before[i], "\n") {
			if !strings.Contains(after, line) {
				t.Fatalf("schema object changed or lost: %s\nafter:\n%s", line, after)
			}
		}
	}
	if got := dumpRows(t, d, `SELECT name FROM sqlite_master WHERE name LIKE '%_new'`); got != "" {
		t.Fatalf("leftover tables: %s", got)
	}
	if got := dumpRows(t, d, `SELECT name FROM sqlite_master WHERE tbl_name IN ('page_likes', 'notifications') AND name NOT LIKE 'sqlite_%' ORDER BY name`); got !=
		"notifications\nnotifications_comment\nnotifications_once\nnotifications_page_once\nnotifications_unread\nnotifications_user\npage_likes\npage_likes_page\n" {
		t.Fatalf("schema objects:\n%s", got)
	}
	if got := dumpRows(t, d, `SELECT page FROM notifications WHERE page IS NOT NULL`); got != "" {
		t.Fatalf("old notifications got a page: %s", got)
	}
	if got := dumpRows(t, d, `SELECT "table", "from", "to", on_delete FROM pragma_foreign_key_list('notifications') ORDER BY "from"`); got !=
		"users actor_id id CASCADE\ncomments comment_id id CASCADE\npages page path NO ACTION\nusers user_id id CASCADE\n" {
		t.Fatalf("notifications foreign keys:\n%s", got)
	}

	// The new shapes: an article like, liked once per account; its notification has a page and no
	// comment, once per (author, actor, page); comment notifications are as strict as before.
	for _, q := range []string{
		`INSERT INTO page_likes (user_id, page, created_at) VALUES (2, '/articles/x/', 30), (3, '/articles/x/', 31), (2, '/articles/y/', 32)`,
		`INSERT INTO notifications (user_id, kind, actor_id, page, created_at) VALUES (1, 'page_like', 2, '/articles/x/', 30)`,
		`INSERT INTO notifications (user_id, kind, actor_id, page, created_at) VALUES (1, 'page_like', 3, '/articles/x/', 31)`,
		`INSERT INTO notifications (user_id, kind, actor_id, comment_id, created_at) VALUES (1, 'reply', 3, 7, 32)`,
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	for _, bad := range []string{
		`INSERT INTO page_likes (user_id, page, created_at) VALUES (2, '/articles/x/', 40)`,                                    // twice
		`INSERT INTO page_likes (user_id, page, created_at) VALUES (2, '/articles/nope/', 40)`,                                 // no such page
		`INSERT INTO page_likes (user_id, page, created_at) VALUES (99, '/articles/x/', 40)`,                                   // no such user
		`INSERT INTO notifications (user_id, kind, actor_id, page, created_at) VALUES (1, 'page_like', 2, '/articles/x/', 40)`, // twice
		`INSERT INTO notifications (user_id, kind, actor_id, created_at) VALUES (1, 'page_like', 2, 40)`,                       // no page
		`INSERT INTO notifications (user_id, kind, actor_id, comment_id, page, created_at) VALUES (1, 'page_like', 3, 1, '/articles/y/', 40)`,
		`INSERT INTO notifications (user_id, kind, actor_id, created_at) VALUES (2, 'like', 3, 40)`, // no comment
		`INSERT INTO notifications (user_id, kind, actor_id, comment_id, page, created_at) VALUES (2, 'like', 1, 1, '/articles/x/', 40)`,
		`INSERT INTO notifications (user_id, kind, actor_id, comment_id, created_at) VALUES (2, 'like', 3, 1, 40)`, // like (9) again
		`INSERT INTO notifications (user_id, kind, actor_id, comment_id, created_at) VALUES (2, 'poke', 3, 1, 40)`,
	} {
		if _, err := d.Exec(bad); err == nil {
			t.Fatalf("accepted: %s", bad)
		}
	}
	if got := dumpRows(t, d, `SELECT MAX(id) FROM notifications`); got != "13\n" {
		t.Fatalf("new notification ids: %s", got)
	}
	// Deleting a comment still takes its notifications along; deleting an account its likes.
	if _, err := d.Exec(`DELETE FROM comments WHERE id = 7`); err != nil {
		t.Fatal(err)
	}
	if got := dumpRows(t, d, `SELECT COUNT(*) FROM notifications WHERE comment_id = 7`); got != "0\n" {
		t.Fatalf("notifications of a deleted comment: %s", got)
	}
	d.Exec(`DELETE FROM comment_likes WHERE user_id = 3`)
	d.Exec(`DELETE FROM notifications WHERE user_id = 3 OR actor_id = 3`)
	if _, err := d.Exec(`DELETE FROM users WHERE id = 3`); err != nil {
		t.Fatal(err)
	}
	if got := dumpRows(t, d, `SELECT user_id, page FROM page_likes ORDER BY user_id, page`); got != "2 /articles/x/\n2 /articles/y/\n" {
		t.Fatalf("page likes after deleting an account:\n%s", got)
	}
	for _, q := range []string{`PRAGMA foreign_key_check`, `PRAGMA integrity_check`} {
		if got := dumpRows(t, d, q); got != "" && got != "ok\n" {
			t.Fatalf("%s: %s", q, got)
		}
	}
}

// 0011 (Tokyo mode) only adds users.hyojungo (0) and kyotsu_sessions.mode ('keihan', the only
// mode before it); every row and every other schema object stays as it was.
func TestMigrateFromV10(t *testing.T) {
	path := oldDB(t, 10,
		`INSERT INTO users (id, email, name, role, is_admin, created_at) VALUES (1, 'admin@example.com', 'Admin', 'admin', 1, 0),
			(2, 'a@example.com', 'A', 'user', 0, 1)`,
		`INSERT INTO kyotsu_sessions (id, user_id, started_at, ended_at, layout, advanced, total, correct, answers_json, active_seconds, client_id)
			VALUES (4, 2, 10, 20, 'kotu', 1, 3, 2, '[]', 9, 'abc12345'), (6, 1, 30, 40, 'classic', 0, 1, 1, '[]', 0, NULL)`,
	)
	queries := []string{
		`SELECT id, email, name, is_admin, created_at, track_public, bio, role, muted_at, muted_by FROM users ORDER BY id`,
		`SELECT id, user_id, started_at, ended_at, layout, advanced, total, correct, answers_json, active_seconds,
			track_centihours, track_subcat, track_note, client_id FROM kyotsu_sessions ORDER BY id`,
		// (watch_sessions: new in 0012)
		`SELECT type, name, tbl_name, sql FROM sqlite_master WHERE name NOT IN ('users', 'kyotsu_sessions')
			AND tbl_name != 'watch_sessions' ORDER BY name`,
	}
	before := make([]string, len(queries))
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for i, q := range queries {
		before[i] = dumpRows(t, raw, q)
	}
	raw.Close()

	d := open(t, path)
	for i, q := range queries {
		if after := dumpRows(t, d, q); after != before[i] {
			t.Fatalf("%s changed:\nbefore:\n%safter:\n%s", q, before[i], after)
		}
	}
	if got := dumpRows(t, d, `SELECT id, hyojungo FROM users ORDER BY id`); got != "1 0\n2 0\n" {
		t.Fatalf("hyojungo:\n%s", got)
	}
	if got := dumpRows(t, d, `SELECT id, mode FROM kyotsu_sessions ORDER BY id`); got != "4 keihan\n6 keihan\n" {
		t.Fatalf("mode:\n%s", got)
	}
	if _, err := d.Exec(`UPDATE kyotsu_sessions SET mode = 'osaka' WHERE id = 4`); err == nil {
		t.Fatal("mode accepted a value outside keihan/tokyo")
	}
	if _, err := d.Exec(`UPDATE kyotsu_sessions SET mode = 'tokyo' WHERE id = 4`); err != nil {
		t.Fatal(err)
	}
}

// 0012 only adds watch_sessions (video watch time): every row and every other schema object
// stays as it was. client_id is unique per user, and the table goes with its user.
func TestMigrateFromV11(t *testing.T) {
	path := oldDB(t, 11,
		`INSERT INTO users (id, email, name, role, is_admin, created_at) VALUES (1, 'admin@example.com', 'Admin', 'admin', 1, 0),
			(2, 'a@example.com', 'A', 'user', 0, 1)`,
		`INSERT INTO kyotsu_sessions (id, user_id, started_at, ended_at, layout, total, correct, answers_json, active_seconds, client_id, mode)
			VALUES (4, 2, 10, 20, 'kotu', 3, 2, '[]', 9, 'abc12345', 'tokyo')`,
	)
	queries := []string{
		`SELECT type, name, tbl_name, sql FROM sqlite_master WHERE tbl_name != 'watch_sessions' ORDER BY name`,
		`SELECT * FROM users ORDER BY id`, `SELECT * FROM kyotsu_sessions ORDER BY id`,
	}
	before := make([]string, len(queries))
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for i, q := range queries {
		before[i] = dumpRows(t, raw, q)
	}
	raw.Close()

	d := open(t, path)
	for i, q := range queries {
		if after := dumpRows(t, d, q); after != before[i] {
			t.Fatalf("%s changed:\nbefore:\n%safter:\n%s", q, before[i], after)
		}
	}
	if got := dumpRows(t, d, `SELECT name FROM sqlite_master WHERE tbl_name = 'watch_sessions' AND name NOT LIKE 'sqlite_%' ORDER BY name`); got !=
		"watch_sessions\nwatch_sessions_client\n" {
		t.Fatalf("schema objects:\n%s", got)
	}
	ins := `INSERT INTO watch_sessions (user_id, client_id, started_at, updated_at) VALUES (?, ?, 0, 0)`
	for _, row := range [][]any{{2, "abcdefgh"}, {1, "abcdefgh"}, {2, "ijklmnop"}} {
		if _, err := d.Exec(ins, row...); err != nil {
			t.Fatal(err)
		}
	}
	for _, bad := range [][]any{{2, "abcdefgh"}, {2, nil}, {99, "qrstuvwx"}} {
		if _, err := d.Exec(ins, bad...); err == nil {
			t.Fatalf("accepted %v", bad)
		}
	}
	if got := dumpRows(t, d, `SELECT user_id, client_id, video_id, active_seconds, track_centihours, track_week, track_subcat, track_note
		FROM watch_sessions WHERE id = 1`); got != "2 abcdefgh <nil> 0 0 <nil> <nil> \n" {
		t.Fatalf("defaults: %q", got)
	}
	if _, err := d.Exec(`DELETE FROM kyotsu_sessions WHERE user_id = 2`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`DELETE FROM users WHERE id = 2`); err != nil {
		t.Fatal(err)
	}
	if got := dumpRows(t, d, `SELECT user_id FROM watch_sessions`); got != "1\n" {
		t.Fatalf("after deleting user 2: %s", got)
	}
}
