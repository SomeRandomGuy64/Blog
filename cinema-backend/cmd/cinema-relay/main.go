// Command cinema-relay scrapes the cinemas that block datacentre IPs from a residential
// connection and uploads the raw results to cinemad.
//
//	cinema-relay [--server https://cinema-api.fazon.nexus] [--all | --only odeon,vue] [--days 7] [--dry-run]
//
// The ingest token comes from $CINEMA_RELAY_TOKEN or ~/.config/cinema-relay/token.
package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata"

	"fazon.nexus/cinema/internal/registry"
	"fazon.nexus/cinema/internal/scrape"
)

func main() {
	server := flag.String("server", "https://cinema-api.fazon.nexus", "cinemad base URL")
	all := flag.Bool("all", false, "scrape every cinema, not just the relay-flagged ones")
	only := flag.String("only", "", "comma-separated substrings of cinema name/chain (any cinema)")
	days := flag.Int("days", scrape.DefaultDays, "days to scrape, from today")
	dry := flag.Bool("dry-run", false, "scrape and print a summary; don't upload")
	workers := flag.Int("workers", 8, "parallel cinemas")
	timeout := flag.Duration("cinema-timeout", 5*time.Minute, "per-cinema timeout")
	flag.Parse()

	var cinemas []scrape.Cinema
	switch {
	case *only != "":
		cinemas = registry.Select(*only, nil)
	case *all:
		cinemas = registry.Cinemas
	default:
		relay := true
		cinemas = registry.Select("", &relay)
	}
	if len(cinemas) == 0 {
		fatal(errors.New("no cinemas selected"))
	}
	token := ""
	if !*dry {
		var err error
		if token, err = loadToken(); err != nil {
			fatal(err)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	from := scrape.Day(time.Now().In(scrape.London))
	to := from.AddDate(0, 0, *days-1)
	results := scrape.Run(ctx, cinemas, from, to, scrape.Options{Workers: *workers, Timeout: *timeout,
		Progress: func(r scrape.Result, n int, took time.Duration) {
			status := "ok "
			if r.Error != "" {
				status = "ERR"
			}
			fmt.Fprintf(os.Stderr, "%s %-36s %5d shows %5.1fs %s\n", status, r.Name, n, took.Seconds(), r.Error)
		}})
	if ctx.Err() != nil {
		fatal(ctx.Err())
	}
	failed := 0
	for _, r := range results {
		if r.Error != "" {
			failed++
		}
	}
	fmt.Fprintf(os.Stderr, "%d/%d cinemas ok\n", len(results)-failed, len(results))
	if *dry {
		return
	}
	anyCinema := false
	for _, c := range cinemas {
		anyCinema = anyCinema || !c.Relay
	}
	if err := upload(ctx, *server, token, results, anyCinema); err != nil {
		fatal(err)
	}
	if failed == len(results) {
		fatal(errors.New("every cinema failed"))
	}
}

func loadToken() (string, error) {
	if t := strings.TrimSpace(os.Getenv("CINEMA_RELAY_TOKEN")); t != "" {
		return t, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(filepath.Join(dir, "cinema-relay", "token"))
	if err != nil {
		return "", fmt.Errorf("no token: set $CINEMA_RELAY_TOKEN or write ~/.config/cinema-relay/token (%w)", err)
	}
	if t := strings.TrimSpace(string(b)); t != "" {
		return t, nil
	}
	return "", errors.New("token file is empty")
}

// upload POSTs the results (gzipped), retrying transient failures.
func upload(ctx context.Context, server, token string, results []scrape.Result, anyCinema bool) error {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if err := json.NewEncoder(zw).Encode(map[string]any{"results": results}); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	u := strings.TrimSuffix(server, "/") + "/v1/ingest"
	if anyCinema {
		u += "?any=1"
	}
	client := &http.Client{Timeout: 2 * time.Minute}
	var last error
	for attempt := range 4 {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt*attempt) * 10 * time.Second):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(buf.Bytes()))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Content-Encoding", "gzip")
		res, err := client.Do(req)
		if err != nil {
			last = err
			continue
		}
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		res.Body.Close()
		switch {
		case res.StatusCode/100 == 2:
			fmt.Fprintf(os.Stderr, "uploaded %d cinemas (%d KB gzipped) to %s\n", len(results), buf.Len()/1024, u)
			return nil
		case res.StatusCode >= 500 || res.StatusCode == http.StatusTooManyRequests:
			last = fmt.Errorf("upload: HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(msg)))
		default: // 4xx: retrying won't help
			return fmt.Errorf("upload: HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(msg)))
		}
	}
	return last
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "cinema-relay:", err)
	os.Exit(1)
}
