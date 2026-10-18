// Command cinema-relay scrapes the cinemas that block datacentre IPs from a residential
// connection and uploads the raw results to cinemad. By default it asks the server which relay
// cinemas are due (the hot regions' ones every few hours, a batch of the rest of the country
// each run), so it is meant to run often (every 30 min) and is usually quick.
//
//	cinema-relay [--server https://cinema-api.fazon.nexus] [--limit 40 | --all | --only odeon,vue | --every-relay] [--days 7] [--dry-run]
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
	everyRelay := flag.Bool("every-relay", false, "scrape every relay cinema, not just the due ones")
	limit := flag.Int("limit", -1, "max cold-region cinemas this run (default: the server's batch size)")
	only := flag.String("only", "", "comma-separated substrings of cinema name/chain (any cinema)")
	days := flag.Int("days", scrape.DefaultDays, "days to scrape, from today")
	dry := flag.Bool("dry-run", false, "scrape and print a summary; don't upload")
	workers := flag.Int("workers", 8, "parallel cinemas")
	timeout := flag.Duration("cinema-timeout", 5*time.Minute, "per-cinema timeout")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	byServer := *only == "" && !*all && !*everyRelay
	token := ""
	if !*dry || byServer {
		var err error
		if token, err = loadToken(); err != nil {
			fatal(err)
		}
	}
	var cinemas []scrape.Cinema
	switch {
	case *only != "":
		cinemas = registry.Select(*only, nil)
	case *all:
		cinemas = registry.Cinemas
	case *everyRelay:
		relay := true
		cinemas = registry.Select("", &relay)
	default:
		names, err := dueList(ctx, *server, token, *limit)
		if err != nil {
			fatal(err)
		}
		for _, n := range names {
			if c, ok := registry.ByName(n); ok {
				cinemas = append(cinemas, c)
			} else {
				fmt.Fprintf(os.Stderr, "server asked for %q, which this build doesn't know (update cinema-relay)\n", n)
			}
		}
		if len(cinemas) == 0 {
			fmt.Fprintln(os.Stderr, "nothing due")
			return
		}
	}
	if len(cinemas) == 0 {
		fatal(errors.New("no cinemas selected"))
	}
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

// dueList asks cinemad which relay cinemas to scrape now.
func dueList(ctx context.Context, server, token string, limit int) ([]string, error) {
	u := strings.TrimSuffix(server, "/") + "/v1/relay/due"
	if limit >= 0 {
		u += fmt.Sprintf("?limit=%d", limit)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := (&http.Client{Timeout: time.Minute}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("due list: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return nil, fmt.Errorf("due list: HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(msg)))
	}
	var out struct {
		Cinemas   []string `json:"cinemas"`
		Hot, Cold int
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("due list: %w", err)
	}
	fmt.Fprintf(os.Stderr, "due: %d hot-region, %d cold-region cinemas\n", out.Hot, out.Cold)
	return out.Cinemas, nil
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
