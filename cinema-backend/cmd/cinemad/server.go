package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"fazon.nexus/cinema/internal/feedback"
	"fazon.nexus/cinema/internal/listings"
	"fazon.nexus/cinema/internal/registry"
	"fazon.nexus/cinema/internal/scrape"
	"fazon.nexus/cinema/internal/store"
)

// rebuildAfter bounds how long a cached document lives even if nothing changed (stale flags
// and the date window depend on the clock).
const rebuildAfter = 5 * time.Minute

type server struct {
	cfg      config
	store    *store.Store
	feedback *feedback.Store

	mu    sync.Mutex
	cache *doc
}

// doc is a rendered listings.json.
type doc struct {
	gen     uint64
	day     string
	built   time.Time
	json    []byte
	gz      []byte
	etag    string
	lastMod time.Time
}

func newServer(cfg config, st *store.Store, fb *feedback.Store) *server {
	return &server{cfg: cfg, store: st, feedback: fb}
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/listings.json", s.listings)
	mux.HandleFunc("OPTIONS /v1/listings.json", s.preflight)
	mux.HandleFunc("POST /v1/ingest", s.ingest)
	mux.HandleFunc("GET /v1/feedback", s.feedbackList)
	mux.HandleFunc("POST /v1/feedback", s.feedbackPost)
	mux.HandleFunc("OPTIONS /v1/feedback", s.feedbackPreflight)
	mux.HandleFunc("DELETE /v1/feedback/{id}", s.feedbackDelete)
	mux.HandleFunc("GET /healthz", s.health)
	return mux
}

func (s *server) cors(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Vary", "Origin")
	if o := r.Header.Get("Origin"); o != "" && originAllowed(s.cfg.origins, o) {
		w.Header().Set("Access-Control-Allow-Origin", o)
		w.Header().Set("Access-Control-Expose-Headers", "ETag, Last-Modified")
	}
}

func (s *server) preflight(w http.ResponseWriter, r *http.Request) {
	s.cors(w, r)
	w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "If-None-Match, If-Modified-Since")
	w.Header().Set("Access-Control-Max-Age", "86400")
	w.WriteHeader(http.StatusNoContent)
}

// originAllowed matches an Origin against the configured list; an entry ending in ":*"
// ("http://127.0.0.1:*") allows any port.
func originAllowed(allowed []string, origin string) bool {
	for _, a := range allowed {
		if a == origin {
			return true
		}
		if base, ok := strings.CutSuffix(a, ":*"); ok {
			if port, ok := strings.CutPrefix(origin, base+":"); ok && port != "" && strings.Trim(port, "0123456789") == "" {
				return true
			}
		}
	}
	return false
}

// current returns the cached document, rebuilding it when the data, the day or the clock moved on.
func (s *server) current() (*doc, error) {
	now := time.Now()
	from := scrape.Day(now.In(scrape.London))
	entries, gen := s.store.Entries()
	s.mu.Lock()
	defer s.mu.Unlock()
	if c := s.cache; c != nil && c.gen == gen && c.day == from.Format(scrape.DateFmt) && now.Sub(c.built) < rebuildAfter {
		return c, nil
	}
	data := listings.Build(registry.Cinemas, entries, from, from.AddDate(0, 0, s.cfg.days-1), now)
	// "generated" = the newest data, not the render time, so unchanged data keeps its ETag.
	var newest time.Time
	for _, n := range registry.Cinemas {
		if r, ok := s.store.Get(n.Name); ok && r.Attempted.After(newest) {
			newest = r.Attempted
		}
	}
	if !newest.IsZero() {
		data.Generated = newest.In(scrape.London).Format(time.RFC3339)
	}
	b, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	var gz bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&gz, gzip.BestCompression)
	zw.Write(b)
	zw.Close()
	sum := sha256.Sum256(b)
	etag := `"` + hex.EncodeToString(sum[:12]) + `"`
	lastMod := newest
	if c := s.cache; c != nil && c.etag == etag {
		lastMod = c.lastMod
	}
	if lastMod.IsZero() {
		lastMod = now
	}
	s.cache = &doc{gen: gen, day: from.Format(scrape.DateFmt), built: now, json: b, gz: gz.Bytes(), etag: etag, lastMod: lastMod}
	return s.cache, nil
}

func (s *server) listings(w http.ResponseWriter, r *http.Request) {
	s.cors(w, r)
	d, err := s.current()
	if err != nil {
		slog.Error("build listings", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Cache-Control", "public, max-age=300")
	h.Set("ETag", d.etag)
	h.Set("Last-Modified", d.lastMod.UTC().Format(http.TimeFormat))
	h.Add("Vary", "Accept-Encoding")
	if etagMatch(r.Header.Get("If-None-Match"), d.etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set("Content-Type", "application/json; charset=utf-8")
	body := d.json
	if acceptsGzip(r) {
		h.Set("Content-Encoding", "gzip")
		body = d.gz
	}
	h.Set("Content-Length", fmt.Sprint(len(body)))
	if r.Method != http.MethodHead {
		w.Write(body)
	}
}

func etagMatch(header, etag string) bool {
	for _, t := range strings.Split(header, ",") {
		t = strings.TrimPrefix(strings.TrimSpace(t), "W/")
		if t == etag || t == "*" {
			return true
		}
	}
	return false
}

func acceptsGzip(r *http.Request) bool {
	for _, p := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		name, q, _ := strings.Cut(strings.TrimSpace(p), ";")
		if strings.EqualFold(strings.TrimSpace(name), "gzip") && strings.ReplaceAll(q, " ", "") != "q=0" {
			return true
		}
	}
	return false
}

type ingestBody struct {
	Results []scrape.Result `json:"results"`
}

type ingestReply struct {
	Name  string `json:"name"`
	Shows int    `json:"shows"`
	Error string `json:"error,omitempty"`
}

func (s *server) ingest(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(w, r) {
		return
	}
	var body io.Reader = http.MaxBytesReader(w, r.Body, s.cfg.maxBody)
	if strings.EqualFold(r.Header.Get("Content-Encoding"), "gzip") {
		zr, err := gzip.NewReader(body)
		if err != nil {
			http.Error(w, "bad gzip body", http.StatusBadRequest)
			return
		}
		defer zr.Close()
		body = io.LimitReader(zr, s.cfg.maxBody+1)
	}
	raw, err := io.ReadAll(body)
	if err != nil || int64(len(raw)) > s.cfg.maxBody {
		http.Error(w, "body too large or unreadable", http.StatusRequestEntityTooLarge)
		return
	}
	var in ingestBody
	if err := json.Unmarshal(raw, &in); err != nil {
		http.Error(w, "bad JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	anyCinema := r.URL.Query().Get("any") == "1"
	// Validate everything first so a bad upload changes nothing.
	for _, res := range in.Results {
		c, ok := registry.ByName(res.Name)
		if !ok {
			http.Error(w, fmt.Sprintf("unknown cinema %q", res.Name), http.StatusBadRequest)
			return
		}
		if !c.Relay && !anyCinema {
			http.Error(w, fmt.Sprintf("%q is scraped by the server (use ?any=1 to override)", res.Name), http.StatusForbidden)
			return
		}
	}
	now := time.Now().UTC()
	var replies []ingestReply
	for _, res := range in.Results {
		if res.Fetched.IsZero() || res.Fetched.After(now.Add(5*time.Minute)) {
			res.Fetched = now
		}
		shows := res.Shows[:0:0]
		for _, sh := range res.Shows {
			sh.Normalize()
			if _, err := time.Parse(scrape.TimeFmt, sh.Start); err == nil && sh.Film != "" {
				shows = append(shows, sh)
			}
		}
		res.Shows = shows
		rep := ingestReply{Name: res.Name, Shows: len(shows), Error: string(res.Error)}
		if _, err := s.store.Apply(res, "relay"); err != nil {
			slog.Error("store", "cinema", res.Name, "err", err)
			http.Error(w, "store error", http.StatusInternalServerError)
			return
		}
		replies = append(replies, rep)
	}
	failed := 0
	for _, rp := range replies {
		if rp.Error != "" {
			failed++
		}
	}
	slog.Info("ingest", "cinemas", len(replies), "failed", failed, "remote", r.RemoteAddr)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"accepted": replies})
}

// authorized checks the ingest bearer token (also used for feedback moderation), answering the
// request itself when it fails.
func (s *server) authorized(w http.ResponseWriter, r *http.Request) bool {
	if s.cfg.token == "" {
		http.Error(w, "token not configured", http.StatusServiceUnavailable)
		return false
	}
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || subtle.ConstantTimeCompare([]byte(strings.TrimSpace(tok)), []byte(s.cfg.token)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	return true
}

func (s *server) health(w http.ResponseWriter, r *http.Request) {
	entries, _ := s.store.Entries()
	var okN, stale int
	now := time.Now()
	for _, c := range registry.Cinemas {
		e, ok := entries[c.Name]
		if ok && e.Error == "" {
			okN++
		}
		if !ok || e.Updated.IsZero() || now.Sub(e.Updated) > listings.StaleAfter {
			stale++
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(map[string]any{"status": "ok", "cinemas": len(registry.Cinemas), "ok": okN, "stale": stale})
}
