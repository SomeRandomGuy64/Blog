// Command fazond is the fazon.nexus API (accounts, comments, notifications, Kyōtsu sessions)
// and its admin CLI:
//
//	fazond [serve]                                  run the API (default)
//	fazond user add -email X -name Y [-admin]       add an account (there is no signup)
//	fazond user list
//	fazond login-link -email X [-next /path/]       print a fresh magic link, no email sent
//
// Config: flags, or the FAZOND_* env vars named in each flag's help (flags win).
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"fazon.nexus/backend/internal/api"
	"fazon.nexus/backend/internal/db"
)

const (
	backupKeep       = 14
	maintenanceEvery = time.Hour
)

const usage = `usage:
  fazond [serve] [flags]
  fazond user add -email X -name Y [-admin] [flags]
  fazond user list [flags]
  fazond login-link -email X [-next /path/] [flags]
run "fazond serve -h" for the shared config flags`

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))
	args := os.Args[1:]
	cmd := "serve"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}
	if cmd == "user" && len(args) > 0 {
		cmd, args = "user "+args[0], args[1:]
	}
	var err error
	switch cmd {
	case "serve":
		err = serve(args)
	case "user add":
		err = userAdd(args)
	case "user list":
		err = userList(args)
	case "login-link":
		err = loginLink(args)
	case "help":
		fmt.Println(usage)
	default:
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		slog.Error(cmd, "err", err)
		os.Exit(1)
	}
}

// open parses the shared flags (plus any registered by extra), opens + migrates the database
// and seeds the admin account.
func open(name string, args []string, extra func(*flag.FlagSet)) (*config, *sql.DB, error) {
	fs := flag.NewFlagSet("fazond "+name, flag.ExitOnError)
	cfg, finish := configFlags(fs)
	if extra != nil {
		extra(fs)
	}
	fs.Parse(args)
	if fs.NArg() > 0 {
		return nil, nil, fmt.Errorf("unexpected arguments: %q", fs.Args())
	}
	if err := finish(); err != nil {
		return nil, nil, err
	}
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(cfg.dataDir, "fazon.db"))
	if err != nil {
		return nil, nil, err
	}
	if err := db.EnsureAdmin(ctx, d, cfg.adminEmail, cfg.adminName); err != nil {
		d.Close()
		return nil, nil, fmt.Errorf("seed admin: %w", err)
	}
	return cfg, d, nil
}

func serve(args []string) error {
	cfg, d, err := open("serve", args, nil)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := api.New(d, api.Config{Site: cfg.site, Origins: cfg.origins, CookieSecure: cfg.cookieSecure,
		Mailer: cfg.mailer(), MailTimeout: time.Minute})
	httpSrv := &http.Server{Addr: cfg.listen, Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 2 * time.Minute}
	maintDone := make(chan struct{})
	go func() {
		defer close(maintDone)
		maintenance(ctx, d, srv, filepath.Join(cfg.dataDir, "backups"))
	}()
	go func() {
		slog.Info("listening", "addr", cfg.listen, "data", cfg.dataDir, "site", cfg.site, "mail", cfg.mailMode,
			"origins", strings.Join(cfg.origins, " "), "cookie_secure", cfg.cookieSecure)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("listen", "err", err)
			stop()
		}
	}()
	<-ctx.Done()
	slog.Info("shutting down")
	sctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(sctx); err != nil {
		slog.Warn("http shutdown", "err", err)
	}
	mailDone := make(chan struct{})
	go func() { srv.Wait(); close(mailDone) }()
	select {
	case <-mailDone:
	case <-sctx.Done():
		slog.Warn("pending emails did not finish in time")
	}
	<-maintDone
	return nil
}

// maintenance purges expired tokens/sessions and rate-limit state, and writes the day's
// backup if it is missing: at start and then hourly, so a backup lands within an hour of
// midnight UTC (or of startup).
func maintenance(ctx context.Context, d *sql.DB, srv *api.Server, backupDir string) {
	for {
		now := time.Now()
		if tok, sess, err := db.Purge(ctx, d, now); err != nil {
			slog.Error("purge", "err", err)
		} else if tok+sess > 0 {
			slog.Info("purged expired", "login_tokens", tok, "sessions", sess)
		}
		srv.Sweep()
		if path, err := db.Backup(ctx, d, backupDir, now, backupKeep); err != nil {
			slog.Error("backup", "err", err)
		} else if path != "" {
			slog.Info("backup written", "path", path)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(maintenanceEvery):
		}
	}
}

func userAdd(args []string) error {
	var email, name string
	var admin bool
	_, d, err := open("user add", args, func(fs *flag.FlagSet) {
		fs.StringVar(&email, "email", "", "email address (required)")
		fs.StringVar(&name, "name", "", "display name shown on comments (required)")
		fs.BoolVar(&admin, "admin", false, "make the account an admin")
	})
	if err != nil {
		return err
	}
	defer d.Close()
	u, err := db.AddUser(context.Background(), d, email, name, admin)
	if err != nil {
		return err
	}
	fmt.Printf("added user %d: %s <%s> admin=%v\n", u.ID, u.Name, u.Email, u.Admin)
	return nil
}

func userList(args []string) error {
	_, d, err := open("user list", args, nil)
	if err != nil {
		return err
	}
	defer d.Close()
	users, err := db.ListUsers(context.Background(), d)
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tEMAIL\tNAME\tADMIN\tCREATED")
	for _, u := range users {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%v\t%s\n", u.ID, u.Email, u.Name, u.Admin, u.Created.Format(time.RFC3339))
	}
	return tw.Flush()
}

func loginLink(args []string) error {
	var email, next string
	cfg, d, err := open("login-link", args, func(fs *flag.FlagSet) {
		fs.StringVar(&email, "email", "", "account email (required)")
		fs.StringVar(&next, "next", "/", "path to land on after signing in")
	})
	if err != nil {
		return err
	}
	defer d.Close()
	ctx := context.Background()
	u, err := db.UserByEmail(ctx, d, email)
	if err != nil {
		return fmt.Errorf("%s: %w", email, err)
	}
	next = api.SafeNext(next)
	tok, err := db.CreateLoginToken(ctx, d, u.ID, next, time.Now(), api.LoginTokenTTL)
	if err != nil {
		return err
	}
	fmt.Println(api.MagicLink(cfg.site, tok, next))
	return nil
}
