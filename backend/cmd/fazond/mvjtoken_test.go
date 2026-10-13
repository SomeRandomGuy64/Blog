package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fazon.nexus/backend/internal/api"
	"fazon.nexus/backend/internal/db"
)

func TestMvJToken(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FAZOND_DATA", dir)
	t.Setenv("FAZOND_ADMIN_EMAIL", "admin@example.com")
	stored := func() (string, bool) {
		d, err := db.Open(context.Background(), filepath.Join(dir, "fazon.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer d.Close()
		var v string
		err = d.QueryRow(`SELECT value FROM settings WHERE key = ?`, api.SettingMvJRefresh).Scan(&v)
		return v, err == nil
	}
	run := func(stdin string) error {
		f, err := os.CreateTemp(t.TempDir(), "stdin")
		if err != nil {
			t.Fatal(err)
		}
		f.WriteString(stdin)
		f.Seek(0, 0)
		old := os.Stdin
		os.Stdin = f
		defer func() { os.Stdin = old; f.Close() }()
		return mvjToken(nil)
	}

	for _, bad := range []string{"", " \n", "two tokens\n", "a\nb\n"} {
		if err := run(bad); err == nil || !strings.Contains(err.Error(), "one refresh token") {
			t.Fatalf("stdin %q: err = %v, want a one-token error", bad, err)
		}
		if _, ok := stored(); ok {
			t.Fatalf("stdin %q: stored a token", bad)
		}
	}
	if err := run("tok-1\n"); err != nil {
		t.Fatal(err)
	}
	if v, ok := stored(); !ok || v != "tok-1" {
		t.Fatalf("stored = %q, %v, want tok-1", v, ok)
	}
	if err := run("tok-2"); err != nil { // replaces, trailing newline optional
		t.Fatal(err)
	}
	if v, _ := stored(); v != "tok-2" {
		t.Fatalf("stored = %q, want tok-2", v)
	}
	if err := run("two words"); err == nil {
		t.Fatal("whitespace token accepted")
	}
	if v, _ := stored(); v != "tok-2" {
		t.Fatalf("a rejected token overwrote the stored one: %q", v)
	}
}
