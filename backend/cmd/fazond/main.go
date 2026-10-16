// Command fazond is the fazon.nexus API (accounts, comments, notifications, Kyōtsu sessions)
// and its admin CLI:
//
//	fazond [serve]                                  run the API (default)
//	fazond user add -email X -name Y [-admin]       add an account (people can also sign up on the site)
//	fazond user list
//	fazond login-link -email X [-next /path/]       print a fresh magic link, no email sent
//	fazond mvj-token < token                        store the MvJ sync's session (deploy/mvj_login.py)
//	fazond track fold-subcats -email X [-apply]     fold sub-categories from before this week into their category
//
// Config: flags, or the FAZOND_* env vars named in each flag's help (flags win).
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
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
	"fazon.nexus/backend/internal/week"
)

const (
	backupKeep       = 14
	maintenanceEvery = time.Hour
)

const usage = `usage:
  fazond [serve] [flags]
  fazond user add -email X -name Y [-admin] [flags]
  fazond user list [flags]
  fazond user hyojungo -email X [-off] [flags]   give (or take) the trainer's Tokyo mode
  fazond login-link -email X [-next /path/] [flags]
  fazond mvj-token [flags] < refresh-token       store the MvJ sync's session (see deploy/mvj_login.py)
  fazond track fold-subcats -email X [-before WEEK] [-apply] [flags]
                                                 fold Track sub-categories created before a week into
                                                 their category's unassigned hours (dry run without -apply)
run "fazond serve -h" for the shared config flags`

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))
	args := os.Args[1:]
	cmd := "serve"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}
	if (cmd == "user" || cmd == "track") && len(args) > 0 {
		cmd, args = cmd+" "+args[0], args[1:]
	}
	var err error
	switch cmd {
	case "serve":
		err = serve(args)
	case "user add":
		err = userAdd(args)
	case "user list":
		err = userList(args)
	case "user hyojungo":
		err = userHyojungo(args)
	case "login-link":
		err = loginLink(args)
	case "mvj-token":
		err = mvjToken(args)
	case "track fold-subcats":
		err = trackFoldSubcats(args)
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
		Mailer: cfg.mailer(), MailTimeout: time.Minute, AuthorEmail: cfg.adminEmail,
		HyojungoDir: filepath.Join(cfg.dataDir, "hyojungo")})
	httpSrv := &http.Server{Addr: cfg.listen, Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 2 * time.Minute}
	maintDone := make(chan struct{})
	go func() {
		defer close(maintDone)
		maintenance(ctx, d, srv, filepath.Join(cfg.dataDir, "backups"))
	}()
	mvjDone := make(chan struct{})
	go func() {
		defer close(mvjDone)
		if cfg.mvj.email == "" {
			return
		}
		from, _ := week.Parse(cfg.mvj.from, time.Now()) // checked in configFlags
		slog.Info("mvj sync on", "account", cfg.mvj.email, "from", cfg.mvj.from)
		srv.RunMvJSync(ctx, api.MvJConfig{Supabase: cfg.mvj.supabase, AnonKey: cfg.mvj.anonKey,
			LocalEmail: cfg.adminEmail, From: from})
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
	<-mvjDone
	return nil
}

// maintenance reloads the site's comment-pages index (titles of commentable pages), purges
// expired tokens/sessions and rate-limit state, and writes the day's backup if it is missing:
// at start and then hourly, so a backup lands within an hour of midnight UTC (or of startup).
func maintenance(ctx context.Context, d *sql.DB, srv *api.Server, backupDir string) {
	for {
		if err := srv.RefreshPages(ctx); err != nil {
			slog.Warn("comment pages", "err", err)
		}
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
	fmt.Fprintln(tw, "ID\tEMAIL\tNAME\tROLE\tCREATED")
	for _, u := range users {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\n", u.ID, u.Email, u.Name, u.Role, u.Created.Format(time.RFC3339))
	}
	return tw.Flush()
}

// userHyojungo gives an account the trainer's Tokyo mode (Kotu's NHK pairs), or takes it away
// with -off. Admins always have it. The admin page does the same.
func userHyojungo(args []string) error {
	var email string
	var off bool
	_, d, err := open("user hyojungo", args, func(fs *flag.FlagSet) {
		fs.StringVar(&email, "email", "", "account email (required)")
		fs.BoolVar(&off, "off", false, "take Tokyo mode away instead")
	})
	if err != nil {
		return err
	}
	defer d.Close()
	u, err := db.UserByEmail(context.Background(), d, email)
	if err != nil {
		return fmt.Errorf("%s: %w", email, err)
	}
	if _, err := d.Exec(`UPDATE users SET hyojungo = ? WHERE id = ?`, !off, u.ID); err != nil {
		return err
	}
	fmt.Printf("%s <%s>: Tokyo mode %s%s\n", u.Name, u.Email, map[bool]string{true: "off", false: "on"}[off],
		map[bool]string{true: " (admins always have it)", false: ""}[u.Admin])
	return nil
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

// trackFoldSubcats folds an account's Track sub-categories created before a week (default: this
// week) into their categories: every week keeps its category totals, the hours move to the
// unassigned bucket and the sub-categories are deleted. Without -apply it only lists them.
func trackFoldSubcats(args []string) error {
	var email, before string
	var apply bool
	_, d, err := open("track fold-subcats", args, func(fs *flag.FlagSet) {
		fs.StringVar(&email, "email", "", "account email (required)")
		fs.StringVar(&before, "before", "", "fold sub-categories created before this week (a Saturday key; default: the current week)")
		fs.BoolVar(&apply, "apply", false, "make the change (otherwise a dry run)")
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
	k := week.Of(time.Now())
	if before != "" {
		if k, err = week.Parse(before, time.Now()); err != nil {
			return err
		}
	}
	folded, err := db.FoldSubcats(ctx, d, u.ID, k.Start(), apply)
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tCATEGORY\tNAME\tCREATED\tWEEKS\tHOURS")
	var total int64
	for _, f := range folded {
		total += f.Centihours
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%d\t%.2f\n", f.ID, f.Category, f.Name, f.Created.Format(time.DateOnly), f.Weeks, float64(f.Centihours)/100)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	verb := "would fold"
	if apply {
		verb = "folded"
	}
	fmt.Printf("%s <%s>: %s %d sub-categories created before the week of %s (%.2f h moved to unassigned)\n",
		u.Name, u.Email, verb, len(folded), k, float64(total)/100)
	if !apply && len(folded) > 0 {
		fmt.Println("dry run: add -apply to make the change")
	}
	return nil
}

// mvjToken stores the MvJ sync's session: the refresh token of a sign-in made on the owner's own
// machine (deploy/mvj_login.py pipes it in), so the server never holds the password. The running
// server picks it up at its next refresh; the script restarts it to make that immediate.
func mvjToken(args []string) error {
	_, d, err := open("mvj-token", args, nil)
	if err != nil {
		return err
	}
	defer d.Close()
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, 4096))
	if err != nil {
		return err
	}
	tok := strings.TrimSpace(string(raw))
	if tok == "" || strings.ContainsAny(tok, " \t\n") {
		return errors.New("expected one refresh token on stdin")
	}
	if _, err := d.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, api.SettingMvJRefresh, tok); err != nil {
		return err
	}
	fmt.Println("MvJ session stored")
	return nil
}
