package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"fazon.nexus/backend/internal/db"
)

// Comment pages. The site publishes /comment-pages.json (layouts/home.commentpages.json): every
// page that renders a comment thread, with its title. Only those pages take comments, and their
// titles (shown in notifications and on profiles) come from there, never from the client.

const (
	pageIndexPath     = "/comment-pages.json"
	maxPageIndexBytes = 1 << 20
	pageFetchTimeout  = 10 * time.Second
	// A comment on a page missing from the index refetches it (a new article), at most this often.
	pageMissRefresh = time.Minute
)

// PageFetcher returns the raw comment-pages index ({"pages":{"/articles/x/":"Title",…}}).
type PageFetcher func(ctx context.Context) ([]byte, error)

// HTTPPageFetcher GETs site+pageIndexPath with a timeout and a size cap. A throwaway query
// string keeps a CDN (GitHub Pages caches for 10 minutes) from serving a stale index.
func HTTPPageFetcher(site string) PageFetcher {
	client := &http.Client{Timeout: pageFetchTimeout}
	return func(ctx context.Context) ([]byte, error) {
		u := strings.TrimSuffix(site, "/") + pageIndexPath + "?t=" + strconv.FormatInt(time.Now().Unix(), 10)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Cache-Control", "no-cache")
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("GET %s: %s", u, resp.Status)
		}
		if resp.ContentLength > maxPageIndexBytes {
			return nil, fmt.Errorf("GET %s: index larger than %d bytes", u, maxPageIndexBytes)
		}
		b, err := io.ReadAll(io.LimitReader(resp.Body, maxPageIndexBytes+1))
		if err != nil {
			return nil, err
		}
		if len(b) > maxPageIndexBytes {
			return nil, fmt.Errorf("GET %s: index larger than %d bytes", u, maxPageIndexBytes)
		}
		return b, nil
	}
}

// parsePageIndex validates an index: a JSON object with a non-empty "pages" object of
// path → title strings. Paths the API would refuse anyway are dropped; titles are cleaned.
func parsePageIndex(b []byte) (map[string]string, error) {
	if len(b) > maxPageIndexBytes {
		return nil, fmt.Errorf("index larger than %d bytes", maxPageIndexBytes)
	}
	var raw struct {
		Pages map[string]string `json:"pages"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("parse index: %w", err)
	}
	out := make(map[string]string, len(raw.Pages))
	for p, t := range raw.Pages {
		if validPage(p) {
			out[p] = cleanTitle(t)
		}
	}
	// An empty index is far more likely a broken build than a site with no articles; keeping
	// the previous one beats closing every thread.
	if len(out) == 0 {
		return nil, errors.New("index lists no valid pages")
	}
	return out, nil
}

// cleanTitle makes a title safe to show inline (notifications, profiles): one line, no control
// or bidi formatting characters (see db.NormalizeName), at most maxTitleRunes.
func cleanTitle(s string) string {
	return truncateRunes(strings.TrimSpace(strings.Map(func(r rune) rune {
		if db.IsBidiControl(r) || r == '\n' || r == '\t' {
			return ' '
		}
		return r
	}, cleanBody(s))), maxTitleRunes)
}

// pageIndex is the in-memory copy of the site's comment-pages index.
type pageIndex struct {
	fetch PageFetcher

	refreshMu sync.Mutex // one fetch at a time; guards lastTry
	lastTry   time.Time

	mu    sync.RWMutex
	pages map[string]string // nil until an index has loaded
}

func (p *pageIndex) lookup(path string) (title string, ok, loaded bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	title, ok = p.pages[path]
	return title, ok, p.pages != nil
}

// RefreshPages fetches the comment-pages index now. On success it replaces the in-memory copy
// and rewrites stored page titles to match (so a title is always the published one); on failure
// the previous copy stays. fazond calls it at startup and hourly.
func (s *Server) RefreshPages(ctx context.Context) error {
	s.pages.refreshMu.Lock()
	defer s.pages.refreshMu.Unlock()
	return s.refreshPagesLocked(ctx)
}

func (s *Server) refreshPagesLocked(ctx context.Context) error {
	s.pages.lastTry = s.now()
	if s.pages.fetch == nil {
		return errors.New("no comment-pages source configured")
	}
	b, err := s.pages.fetch(ctx)
	if err != nil {
		return fmt.Errorf("fetch comment pages: %w", err)
	}
	m, err := parsePageIndex(b)
	if err != nil {
		return err
	}
	s.pages.mu.Lock()
	first, before := s.pages.pages == nil, len(s.pages.pages)
	s.pages.pages = m
	s.pages.mu.Unlock()
	fixed, err := s.syncPageTitles(ctx, m)
	if err != nil {
		return fmt.Errorf("update page titles: %w", err)
	}
	if first || before != len(m) || fixed > 0 {
		slog.Info("comment pages loaded", "pages", len(m), "titles_updated", fixed)
	}
	return nil
}

// syncPageTitles sets every stored page's title to its published one, or to "" for a page
// that is no longer in the index (its old title came from a client and can't be trusted).
func (s *Server) syncPageTitles(ctx context.Context, m map[string]string) (int, error) {
	fixed := 0
	err := s.tx(ctx, func(tx *sql.Tx) error {
		fixed = 0
		rows, err := tx.Query(`SELECT path, title FROM pages`)
		if err != nil {
			return err
		}
		type change struct{ path, title string }
		var changes []change
		for rows.Next() {
			var path, title string
			if err := rows.Scan(&path, &title); err != nil {
				rows.Close()
				return err
			}
			if want := m[path]; title != want {
				changes = append(changes, change{path, want})
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, c := range changes {
			if _, err := tx.Exec(`UPDATE pages SET title = ? WHERE path = ?`, c.title, c.path); err != nil {
				return err
			}
		}
		fixed = len(changes)
		return nil
	})
	return fixed, err
}

// commentPage looks a page up in the index. A miss refetches the index first (a newly
// published article), at most once per pageMissRefresh across all requests. loaded is false
// while no index has ever loaded.
func (s *Server) commentPage(ctx context.Context, path string) (title string, ok, loaded bool) {
	if title, ok, loaded = s.pages.lookup(path); ok {
		return title, ok, loaded
	}
	s.pages.refreshMu.Lock()
	defer s.pages.refreshMu.Unlock()
	// Another request may have refreshed while this one waited for the lock.
	if title, ok, loaded = s.pages.lookup(path); ok {
		return title, ok, loaded
	}
	if s.pages.lastTry.IsZero() || s.now().Sub(s.pages.lastTry) >= pageMissRefresh {
		if err := s.refreshPagesLocked(ctx); err != nil {
			slog.Warn("comment pages refresh", "err", err)
		}
	}
	return s.pages.lookup(path)
}
