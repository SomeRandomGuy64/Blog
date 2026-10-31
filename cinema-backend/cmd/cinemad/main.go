// Command cinemad serves listings.json. It scrapes every non-relay cinema on start and every
// interval, keeps each cinema's last good data on disk, and accepts uploads for the
// relay-only cinemas (blocked from datacentre IPs) from cinema-relay.
//
// Config: flags, or the env vars named in each flag's help (flags win).
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata"

	"fazon.nexus/cinema/internal/feedback"
	"fazon.nexus/cinema/internal/region"
	"fazon.nexus/cinema/internal/registry"
	"fazon.nexus/cinema/internal/scrape"
	"fazon.nexus/cinema/internal/store"
)

type config struct {
	listen   string
	dataDir  string
	token    string
	origins  []string
	interval time.Duration // hot server cinemas
	timeout  time.Duration
	workers  int
	maxBody  int64
	days     int

	coldEvery  time.Duration // cinemas in regions nobody is looking at
	relayEvery time.Duration // hot relay cinemas
	hotFor     time.Duration // a viewed region stays hot this long
	coldBatch  int           // cold cinemas per scrape round / relay run
	tick       time.Duration // how often the server looks for due cinemas
	regions    []string      // served when a request names none; always hot
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envDur(key string, def time.Duration) time.Duration {
	if d, err := time.ParseDuration(os.Getenv(key)); err == nil {
		return d
	}
	return def
}

func envInt(key string, def int) int {
	if n, err := strconv.Atoi(os.Getenv(key)); err == nil {
		return n
	}
	return def
}

func parseConfig() config {
	var c config
	var origins, tokenFile string
	flag.StringVar(&c.listen, "listen", env("CINEMAD_LISTEN", "127.0.0.1:8090"), "listen address ($CINEMAD_LISTEN)")
	flag.StringVar(&c.dataDir, "data", env("CINEMAD_DATA", "/var/lib/cinemad"), "data directory ($CINEMAD_DATA)")
	flag.StringVar(&tokenFile, "token-file", env("CINEMAD_INGEST_TOKEN_FILE", ""), "file holding the ingest bearer token ($CINEMAD_INGEST_TOKEN_FILE; or set $CINEMAD_INGEST_TOKEN)")
	flag.StringVar(&origins, "origins", env("CINEMAD_ORIGINS", "https://fazon.nexus,http://localhost:1313,http://127.0.0.1:*"), "comma-separated CORS origins; host:* = any port ($CINEMAD_ORIGINS)")
	flag.DurationVar(&c.interval, "interval", envDur("CINEMAD_INTERVAL", 2*time.Hour), "refresh interval for hot server-scraped cinemas ($CINEMAD_INTERVAL)")
	flag.DurationVar(&c.coldEvery, "cold-interval", envDur("CINEMAD_COLD_INTERVAL", 24*time.Hour), "refresh interval for cinemas in cold regions ($CINEMAD_COLD_INTERVAL)")
	flag.DurationVar(&c.relayEvery, "relay-interval", envDur("CINEMAD_RELAY_INTERVAL", 3*time.Hour), "refresh interval for hot relay cinemas ($CINEMAD_RELAY_INTERVAL)")
	flag.DurationVar(&c.hotFor, "hot-for", envDur("CINEMAD_HOT_FOR", 48*time.Hour), "a requested region stays hot this long ($CINEMAD_HOT_FOR)")
	flag.IntVar(&c.coldBatch, "cold-batch", envInt("CINEMAD_COLD_BATCH", 40), "max cold cinemas per scrape round or relay run ($CINEMAD_COLD_BATCH)")
	flag.DurationVar(&c.tick, "tick", envDur("CINEMAD_TICK", 10*time.Minute), "how often to look for due cinemas ($CINEMAD_TICK)")
	flag.DurationVar(&c.timeout, "cinema-timeout", envDur("CINEMAD_CINEMA_TIMEOUT", 5*time.Minute), "per-cinema scrape timeout ($CINEMAD_CINEMA_TIMEOUT)")
	flag.IntVar(&c.workers, "workers", envInt("CINEMAD_WORKERS", 8), "parallel cinemas ($CINEMAD_WORKERS)")
	flag.Int64Var(&c.maxBody, "max-body", int64(envInt("CINEMAD_MAX_BODY", 16<<20)), "max ingest body bytes, after decompression ($CINEMAD_MAX_BODY)")
	flag.IntVar(&c.days, "days", envInt("CINEMAD_DAYS", scrape.ServerDays), "days scraped and served, from today; relay cinemas stay at their shorter window ($CINEMAD_DAYS)")
	flag.Func("regions", "comma-separated default regions, always hot ($CINEMAD_REGIONS; default "+strings.Join(region.Default, ",")+")", func(v string) error {
		r, err := parseRegions(v)
		c.regions = r
		return err
	})
	if v := os.Getenv("CINEMAD_REGIONS"); v != "" {
		var err error
		if c.regions, err = parseRegions(v); err != nil {
			slog.Error("CINEMAD_REGIONS", "err", err)
			os.Exit(1)
		}
	}
	flag.Parse()
	if len(c.regions) == 0 {
		c.regions, _ = parseRegions(strings.Join(region.Default, ","))
	}
	c.token = os.Getenv("CINEMAD_INGEST_TOKEN")
	if tokenFile != "" {
		b, err := os.ReadFile(tokenFile)
		if err != nil {
			slog.Error("read token file", "err", err)
			os.Exit(1)
		}
		c.token = strings.TrimSpace(string(b))
	}
	for _, o := range strings.Split(origins, ",") {
		if o = strings.TrimSpace(o); o != "" {
			c.origins = append(c.origins, strings.TrimSuffix(o, "/"))
		}
	}
	return c
}

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))
	cfg := parseConfig()
	if cfg.token == "" {
		slog.Warn("no ingest token configured: POST /v1/ingest is disabled")
	}
	st, err := store.Open(cfg.dataDir)
	if err != nil {
		slog.Error("open store", "err", err)
		os.Exit(1)
	}
	if err := st.Prune(func(n string) bool { _, ok := registry.ByName(n); return ok }); err != nil {
		slog.Warn("prune store", "err", err)
	}

	// A subdirectory: the store reads every *.json directly in the data dir as a cinema record.
	fb, err := feedback.Open(filepath.Join(cfg.dataDir, "feedback"))
	if err != nil {
		slog.Error("open feedback", "err", err)
		os.Exit(1)
	}

	dm, err := openDemand(filepath.Join(cfg.dataDir, "state", "demand.json"), cfg.regions, cfg.hotFor)
	if err != nil {
		slog.Error("open demand", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := newServer(cfg, st, fb, dm)
	httpSrv := &http.Server{Addr: cfg.listen, Handler: srv.routes(), ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 2 * time.Minute, WriteTimeout: time.Minute, IdleTimeout: 2 * time.Minute}
	scraperDone := make(chan struct{})
	go func() {
		defer close(scraperDone)
		scrapeLoop(ctx, cfg, st, dm)
	}()
	go func() {
		slog.Info("listening", "addr", cfg.listen, "data", cfg.dataDir, "origins", strings.Join(cfg.origins, " "))
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
	select {
	case <-scraperDone:
	case <-sctx.Done():
		slog.Warn("scraper did not stop in time")
	}
}

// scrapeLoop scrapes the due server-side cinemas every cfg.tick (or as soon as a cold region
// is requested) until ctx ends.
func scrapeLoop(ctx context.Context, cfg config, st *store.Store, dm *demand) {
	relay := false
	cinemas := registry.Select("", &relay)
	t := tiers{hot: cfg.interval, cold: cfg.coldEvery, retry: cfg.interval, slack: cfg.tick}
	for {
		if d := due(cinemas, st, dm, t, cfg.coldBatch, time.Now()); len(d) > 0 {
			scrapeOnce(ctx, cfg, st, d)
		}
		if err := dm.save(); err != nil {
			slog.Warn("save demand", "err", err)
		}
		select {
		case <-ctx.Done():
			dm.save()
			return
		case <-dm.kick:
		case <-time.After(cfg.tick):
		}
	}
}

func scrapeOnce(ctx context.Context, cfg config, st *store.Store, cinemas []scrape.Cinema) {
	from := scrape.Day(time.Now().In(scrape.London))
	to := from.AddDate(0, 0, cfg.days-1)
	t0 := time.Now()
	var ok, failed, shows int
	scrape.Run(ctx, cinemas, from, to, scrape.Options{Workers: cfg.workers, Timeout: cfg.timeout,
		Progress: func(r scrape.Result, n int, took time.Duration) {
			if ctx.Err() != nil {
				return // shutting down: don't record cancellations as failures
			}
			if _, err := st.Apply(r, "server"); err != nil {
				slog.Error("store", "cinema", r.Name, "err", err)
			}
			if r.Error != "" {
				failed++
				slog.Warn("scrape failed", "cinema", r.Name, "err", string(r.Error), "took", took.Round(time.Millisecond))
				return
			}
			ok++
			shows += n
		}})
	slog.Info("scrape done", "ok", ok, "failed", failed, "shows", shows, "took", time.Since(t0).Round(time.Second))
}
