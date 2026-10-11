// Package db opens the SQLite database, applies the embedded migrations, and holds the few
// queries shared by the API and the CLI (users, login tokens, backups, purging).
package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Open opens (creating if needed) the database at path and migrates it to the latest schema.
// Every pooled connection gets the same pragmas; _txlock=immediate makes write transactions
// take the write lock up front, so two of them wait on busy_timeout instead of deadlocking.
func Open(ctx context.Context, path string) (*sql.DB, error) {
	if strings.ContainsRune(path, '?') {
		return nil, fmt.Errorf("database path %q must not contain '?'", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	dsn := path + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)" +
		"&_pragma=synchronous(NORMAL)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetConnMaxIdleTime(5 * time.Minute)
	if err := migrate(ctx, db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate %s: %w", path, err)
	}
	return db, nil
}

// migrate applies migrations/NNNN_*.sql in order; PRAGMA user_version = the last applied number.
func migrate(ctx context.Context, db *sql.DB) error {
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	var version int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	for _, name := range names {
		var n int
		if _, err := fmt.Sscanf(filepath.Base(name), "%04d_", &n); err != nil {
			return fmt.Errorf("bad migration name %s", name)
		}
		if n <= version {
			continue
		}
		body, err := migrations.ReadFile(name)
		if err != nil {
			return err
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("%s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", n)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		version = n
	}
	return nil
}

// Purge deletes expired login and sign-up tokens (tokens counts both) and sessions.
func Purge(ctx context.Context, db *sql.DB, now time.Time) (tokens, sessions int64, err error) {
	for _, q := range []string{"DELETE FROM login_tokens WHERE expires_at < ?", "DELETE FROM signup_tokens WHERE expires_at < ?"} {
		res, err := db.ExecContext(ctx, q, now.Unix())
		if err != nil {
			return tokens, 0, err
		}
		n, _ := res.RowsAffected()
		tokens += n
	}
	res, err := db.ExecContext(ctx, "DELETE FROM sessions WHERE expires_at < ?", now.Unix())
	if err != nil {
		return tokens, 0, err
	}
	sessions, _ = res.RowsAffected()
	return tokens, sessions, nil
}

// Backup writes dir/fazon-YYYY-MM-DD.db.zst (UTC date of now) unless it already exists:
// VACUUM INTO a temp file, zstd-compress that to another temp file, fsync, rename. Then it
// keeps only the newest keep compressed backups, removes temp files left by a crash, and drops
// legacy uncompressed fazon-*.db backups once a compressed one for that day or later exists.
// It reports the path it wrote, if any. Only one Backup may run at a time (the maintenance loop).
func Backup(ctx context.Context, db *sql.DB, dir string, now time.Time, keep int) (string, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	day := now.UTC().Format("2006-01-02")
	path := filepath.Join(dir, "fazon-"+day+".db.zst")
	tmps, _ := filepath.Glob(filepath.Join(dir, "*.tmp"))
	dotTmps, _ := filepath.Glob(filepath.Join(dir, ".*.tmp"))
	for _, t := range append(tmps, dotTmps...) {
		os.Remove(t)
	}
	wrote := ""
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := writeBackup(ctx, db, dir, day, path); err != nil {
			return "", err
		}
		wrote = path
	} else if err != nil {
		return "", err
	}

	compressed, err := filepath.Glob(filepath.Join(dir, "fazon-????-??-??.db.zst"))
	if err != nil {
		return wrote, err
	}
	sort.Strings(compressed) // ISO dates sort chronologically
	for len(compressed) > keep {
		if err := os.Remove(compressed[0]); err != nil {
			return wrote, err
		}
		compressed = compressed[1:]
	}
	if len(compressed) > 0 {
		newest := strings.TrimSuffix(filepath.Base(compressed[len(compressed)-1]), ".zst")
		legacy, _ := filepath.Glob(filepath.Join(dir, "fazon-????-??-??.db"))
		for _, l := range legacy {
			if filepath.Base(l) <= newest {
				if err := os.Remove(l); err != nil {
					return wrote, err
				}
			}
		}
	}
	return wrote, nil
}

func writeBackup(ctx context.Context, db *sql.DB, dir, day, path string) error {
	raw := filepath.Join(dir, ".fazon-"+day+".db.tmp")
	tmp := filepath.Join(dir, ".fazon-"+day+".db.zst.tmp")
	defer os.Remove(raw)
	defer os.Remove(tmp) // no-op after the rename
	if _, err := db.ExecContext(ctx, "VACUUM INTO ?", raw); err != nil {
		return err
	}
	in, err := os.Open(raw)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o640)
	if err != nil {
		return err
	}
	defer out.Close()
	// One goroutine and the smaller window keep this to a few MB of RAM on a small VPS.
	zw, err := zstd.NewWriter(out, zstd.WithEncoderLevel(zstd.SpeedBetterCompression),
		zstd.WithEncoderConcurrency(1), zstd.WithLowerEncoderMem(true))
	if err != nil {
		return err
	}
	if _, err := io.Copy(zw, in); err != nil {
		zw.Close()
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	if err := out.Sync(); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	if d, err := os.Open(dir); err == nil { // make the rename itself durable
		d.Sync()
		d.Close()
	}
	return nil
}
