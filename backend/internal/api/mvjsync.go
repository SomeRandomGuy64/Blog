package api

// MvJ sync: mirrors one account's Track into the MvJ dojo immersion tracker
// (mattvsjapan.com/immersion-tracking). That site is a Supabase app with no public API, so this
// talks to its PostgREST tables the way its own frontend does, signed in as the owner's MvJ
// account (row-level security keeps it to that account's rows). fazond never sees the password:
// the owner signs in on their own machine (deploy/mvj_login.py) and hands over only the session's
// refresh token (fazond mvj-token), kept in settings and rotated on every refresh. Tables:
//
//	weeks        one row per week: id, start_date (= our week key, the label Saturday), …
//	weekly_logs  one row per (user_id, week_id): categories {cat: {hours, rating}}, notes,
//	             total_hours; saved with an upsert that replaces the row, as the site does
//
// Both sites use the same calendar, categories and rating emojis. Track is the source of truth:
// a week is pushed whole whenever what we'd send for it changes, so edits made on the MvJ site
// to a mirrored week are overwritten by the next change here. Weeks never logged here are never
// pushed. Two things are kept from the MvJ row instead: the notes when our summary is private,
// and the rating of a category not rated here. The MvJ tracker has no Kotu / Kyōtsu scores, so
// the notes we push are the week's day-by-day results (mvjResults) with the summary underneath.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"fazon.nexus/backend/internal/db"
	"fazon.nexus/backend/internal/week"
)

// MvJConfig turns the sync on (see Server.RunMvJSync).
type MvJConfig struct {
	Supabase   string   // the site's Supabase project URL, e.g. https://xxxx.supabase.co
	AnonKey    string   // the site's public anon key (the apikey header its frontend sends)
	LocalEmail string   // the fazon.nexus account whose Track is mirrored
	From       week.Key // earliest week mirrored
	HTTP       *http.Client
}

const (
	mvjEvery       = time.Minute
	mvjMaxBackoff  = time.Hour
	mvjDefaultRate = "🔥" // what the MvJ form sends for a category nobody rated
	// SettingMvJRefresh is the settings key holding the MvJ session's current refresh token.
	SettingMvJRefresh = "mvj_refresh_token"
	mvjRelogin        = "sign in again from your machine: python3 backend/deploy/mvj_login.py root@<server>"
)

// mvjRatings maps track_ratings values to the MvJ site's rating strings.
var mvjRatings = map[int]string{1: "😞", 2: "😐", 3: "👌", 4: "🔥"}

var errMvJNoWeek = errors.New("week not created on the MvJ site yet")

type mvjSync struct {
	s   *Server
	cfg MvJConfig
	hc  *http.Client

	access, userID string
	expires        time.Time

	weekIDs map[string]string   // week key → MvJ weeks.id
	sent    map[string][32]byte // week key → hash of what was last pushed (or found empty at start)
}

// RunMvJSync pushes changed weeks every minute until ctx ends, backing off (up to an hour) while
// the MvJ site or its sign-in fails. State is in memory: after a restart every logged week from
// cfg.From on is pushed once again, which the upsert makes harmless.
func (s *Server) RunMvJSync(ctx context.Context, cfg MvJConfig) {
	m := newMvJSync(s, cfg)
	delay := mvjEvery
	for {
		if err := m.pass(ctx); err != nil {
			delay = min(delay*2, mvjMaxBackoff)
			slog.Warn("mvj sync", "err", err, "retry_in", delay)
		} else {
			delay = mvjEvery
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

func newMvJSync(s *Server, cfg MvJConfig) *mvjSync {
	hc := cfg.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	cfg.Supabase = strings.TrimSuffix(cfg.Supabase, "/")
	return &mvjSync{s: s, cfg: cfg, hc: hc, weekIDs: map[string]string{}, sent: map[string][32]byte{}}
}

// mvjLocal is what one week contributes to its MvJ row; its hash decides whether to push.
type mvjLocal struct {
	Cents          []int64        `json:"cents"` // per trackCategories
	Ratings        map[string]int `json:"ratings"`
	Results        string         `json:"results"` // mvjResults, prepended to the notes
	Summary        string         `json:"summary"`
	SummaryPrivate bool           `json:"summary_private"`
}

func mvjLocalOf(d *weekData) mvjLocal {
	l := mvjLocal{Cents: make([]int64, len(trackCategories)), Ratings: map[string]int{}}
	if d == nil {
		return l
	}
	for i, c := range trackCategories {
		l.Cents[i] = d.total(c.ID)
	}
	l.Ratings, l.Summary, l.SummaryPrivate = d.ratingsOut(), d.summary, d.summaryPrivate
	l.Results = mvjResults(d)
	return l
}

// mvjResults is the week's effective Kotu and Kyōtsu scores as the notes' leading block, one
// section per test with any score, blank days left out ("" if neither has one):
//
//	Kotu Results:
//
//	Sunday 11 Oct: 96.2%
//	Monday 12 Oct: 90.9%
//
//	Kyōtsu Results:
//
//	Sunday 11 Oct: 84.7%
func mvjResults(d *weekData) string {
	labels := d.key.DayLabels()
	var sections []string
	for _, t := range []struct {
		name string
		sc   scores
	}{{"Kotu", d.kotuEffective()}, {"Kyōtsu", d.kyotsuEffective()}} {
		if t.sc.empty() {
			continue
		}
		var b strings.Builder
		b.WriteString(t.name + " Results:\n")
		for i, v := range t.sc {
			if v == nil {
				continue
			}
			day, _ := time.Parse("2006-01-02", labels[i])
			fmt.Fprintf(&b, "\n%s: %.1f%%", day.Format("Monday 2 Jan"), *v)
		}
		sections = append(sections, b.String())
	}
	return strings.Join(sections, "\n\n")
}

// mvjNotes puts the results block above the summary, a blank line between.
func mvjNotes(results, summary string) string {
	if results == "" || summary == "" {
		return results + summary
	}
	return results + "\n\n" + summary
}

func (l mvjLocal) hash() [32]byte {
	b, _ := json.Marshal(l) // map keys are sorted, so this is stable
	return sha256.Sum256(b)
}

// pass pushes every week from cfg.From to now whose content changed since it was last pushed.
func (m *mvjSync) pass(ctx context.Context) error {
	u, err := db.UserByEmail(ctx, m.s.db, m.cfg.LocalEmail)
	if err != nil {
		return fmt.Errorf("local account %s: %w", m.cfg.LocalEmail, err)
	}
	cur := week.Of(m.s.now())
	if cur.Before(m.cfg.From) {
		return nil
	}
	loaded, err := m.s.loadWeeks(ctx, []int64{u.ID}, m.cfg.From, cur)
	if err != nil {
		return err
	}
	byKey := map[string]*weekData{}
	for _, d := range loaded[u.ID] {
		byKey[d.key.String()] = d
	}
	for _, k := range week.Range(m.cfg.From, cur) {
		key := k.String()
		d := byKey[key]
		l := mvjLocalOf(d)
		h := l.hash()
		prev, seen := m.sent[key]
		if !seen && d == nil {
			m.sent[key] = h // never logged here: leave the MvJ row alone until it is
			continue
		}
		if seen && prev == h {
			continue
		}
		switch err := m.push(ctx, key, l); {
		case errors.Is(err, errMvJNoWeek):
			slog.Debug("mvj sync: week not on the MvJ site yet", "week", key)
		case err != nil:
			return fmt.Errorf("week %s: %w", key, err)
		default:
			m.sent[key] = h
			slog.Info("mvj sync: pushed", "week", key)
		}
	}
	return nil
}

type mvjCategory struct {
	Hours  float64 `json:"hours"`
	Rating string  `json:"rating"`
}

type mvjLog struct {
	Categories map[string]mvjCategory `json:"categories"`
	Notes      string                 `json:"notes"`
}

func (m *mvjSync) push(ctx context.Context, key string, l mvjLocal) error {
	weekID, err := m.weekID(ctx, key)
	if err != nil {
		return err
	}
	if _, err := m.token(ctx); err != nil {
		return err
	}
	var existing []mvjLog
	q := url.Values{"select": {"categories,notes"}, "user_id": {"eq." + m.userID}, "week_id": {"eq." + weekID}}
	if err := m.rest(ctx, http.MethodGet, "weekly_logs?"+q.Encode(), nil, "", &existing); err != nil {
		return err
	}
	var old mvjLog
	if len(existing) > 0 {
		old = existing[0]
	}
	cats := map[string]mvjCategory{}
	var total int64
	for i, c := range trackCategories {
		rating := mvjRatings[l.Ratings[c.ID]]
		if rating == "" {
			rating = old.Categories[c.ID].Rating
		}
		if rating == "" {
			rating = mvjDefaultRate
		}
		cats[c.ID] = mvjCategory{Hours: float64(l.Cents[i]) / 100, Rating: rating}
		total += l.Cents[i]
	}
	notes := mvjNotes(l.Results, l.Summary)
	if l.SummaryPrivate {
		notes = old.Notes
	}
	body := map[string]any{"user_id": m.userID, "week_id": weekID, "categories": cats, "notes": notes,
		"total_hours": float64(total) / 100}
	return m.rest(ctx, http.MethodPost, "weekly_logs?on_conflict=user_id,week_id", body,
		"resolution=merge-duplicates,return=minimal", nil)
}

// weekID looks up (and caches) the MvJ id of the week labelled key; errMvJNoWeek if it has none
// yet (the site creates its weeks itself, around the week's start).
func (m *mvjSync) weekID(ctx context.Context, key string) (string, error) {
	if id := m.weekIDs[key]; id != "" {
		return id, nil
	}
	var rows []struct {
		ID string `json:"id"`
	}
	if err := m.rest(ctx, http.MethodGet, "weeks?select=id&start_date=eq."+key, nil, "", &rows); err != nil {
		return "", err
	}
	if len(rows) == 0 || rows[0].ID == "" {
		return "", errMvJNoWeek
	}
	m.weekIDs[key] = rows[0].ID
	return rows[0].ID, nil
}

// rest calls the PostgREST API at /rest/v1/path, decoding a JSON response into out (if non-nil).
// A 401 signs in again and retries once.
func (m *mvjSync) rest(ctx context.Context, method, path string, body any, prefer string, out any) error {
	for attempt := 0; ; attempt++ {
		tok, err := m.token(ctx)
		if err != nil {
			return err
		}
		hdr := http.Header{"Authorization": {"Bearer " + tok}, "Accept": {"application/json"}}
		if prefer != "" {
			hdr.Set("Prefer", prefer)
		}
		status, raw, err := m.call(ctx, method, m.cfg.Supabase+"/rest/v1/"+path, hdr, body)
		if err != nil {
			return err
		}
		if status == http.StatusUnauthorized && attempt == 0 {
			m.access = ""
			continue
		}
		if status >= 300 {
			return fmt.Errorf("%s /rest/v1/%s: %d %s", method, strings.SplitN(path, "?", 2)[0], status, snippet(raw))
		}
		if out != nil {
			return json.Unmarshal(raw, out)
		}
		return nil
	}
}

// token returns a live access token: the current one, else one from refreshing the stored
// session. Refresh tokens are single-use (Supabase rotates them), so the new one is stored
// straight away: the token in settings is always the session's latest. One stored by
// fazond mvj-token is picked up at the next refresh.
func (m *mvjSync) token(ctx context.Context) (string, error) {
	if m.access != "" && time.Now().Before(m.expires.Add(-2*time.Minute)) {
		return m.access, nil
	}
	var refresh string
	err := m.s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, SettingMvJRefresh).Scan(&refresh)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && refresh == "") {
		return "", errors.New("no MvJ session: " + mvjRelogin)
	}
	if err != nil {
		return "", err
	}
	status, raw, err := m.call(ctx, http.MethodPost, m.cfg.Supabase+"/auth/v1/token?grant_type=refresh_token",
		http.Header{}, map[string]string{"refresh_token": refresh})
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("MvJ session refresh: %d %s; %s", status, snippet(raw), mvjRelogin)
	}
	var r struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		User         struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return "", fmt.Errorf("MvJ session refresh: %w", err)
	}
	if r.AccessToken == "" || r.RefreshToken == "" || r.User.ID == "" {
		return "", errors.New("MvJ session refresh: no tokens or user in the response")
	}
	// Replace only the token just used, so one stored meanwhile by mvj-token wins.
	if _, err := m.s.db.ExecContext(ctx, `UPDATE settings SET value = ? WHERE key = ? AND value = ?`,
		r.RefreshToken, SettingMvJRefresh, refresh); err != nil {
		return "", fmt.Errorf("store the rotated MvJ refresh token: %w", err)
	}
	m.access, m.userID = r.AccessToken, r.User.ID
	m.expires = time.Now().Add(time.Duration(r.ExpiresIn) * time.Second)
	return m.access, nil
}

// call sends one request with the apikey header (and a JSON body if non-nil) and reads the reply.
func (m *mvjSync) call(ctx context.Context, method, u string, hdr http.Header, body any) (int, []byte, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return 0, nil, err
	}
	req.Header = hdr
	req.Header.Set("apikey", m.cfg.AnonKey)
	req.Header.Set("User-Agent", "fazond (fazon.nexus Track → MvJ sync)")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := m.hc.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, raw, err
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
