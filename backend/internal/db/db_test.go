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
	if version != 3 || fk != 1 || busy != 5000 || journal != "wal" {
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
	if version != 3 || users != 1 {
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
	if version != 3 || public != 2 || comments != 1 || likes != 1 || avatars != 1 || kyotsu != 1 {
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
	if err != nil || bob.Email != "bob@example.com" || bob.Admin {
		t.Fatalf("add: %+v %v", bob, err)
	}
	// EnsureAdmin promotes but never renames.
	d.Exec(`UPDATE users SET name = 'Renamed', is_admin = 0 WHERE id = 1`)
	EnsureAdmin(ctx, d, "admin@example.com", "Faizan")
	u, err := UserByEmail(ctx, d, "ADMIN@example.com")
	if err != nil || u.Name != "Renamed" || !u.Admin {
		t.Fatalf("admin %+v %v", u, err)
	}
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
	d.Exec(`INSERT INTO sessions VALUES (x'01', 1, 0, ?, 0), (x'02', 1, 0, ?, 0)`, now.Add(-time.Second).Unix(), now.Add(time.Hour).Unix())
	tok, sess, err := Purge(ctx, d, now)
	if err != nil || tok != 1 || sess != 1 {
		t.Fatalf("purged %d tokens %d sessions, err %v", tok, sess, err)
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
