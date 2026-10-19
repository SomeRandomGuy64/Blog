package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"fazon.nexus/cinema/internal/feedback"
	"fazon.nexus/cinema/internal/registry"
	"fazon.nexus/cinema/internal/scrape"
	"fazon.nexus/cinema/internal/store"
)

func testServer(t *testing.T) *httptest.Server {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fb, err := feedback.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg := config{token: "secret", origins: []string{"https://fazon.nexus"}, maxBody: 1 << 20, days: 7,
		regions: []string{"north-west", "yorkshire", "east-midlands"}, coldBatch: 5, relayEvery: 3 * time.Hour, coldEvery: 24 * time.Hour}
	dm, err := openDemand(filepath.Join(t.TempDir(), "demand.json"), cfg.regions, 48*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(newServer(cfg, st, fb, dm).routes())
	t.Cleanup(ts.Close)
	return ts
}

func post(t *testing.T, url, token, body string, gz bool) *http.Response {
	t.Helper()
	var b bytes.Buffer
	if gz {
		w := gzip.NewWriter(&b)
		w.Write([]byte(body))
		w.Close()
	} else {
		b.WriteString(body)
	}
	req, _ := http.NewRequest(http.MethodPost, url, &b)
	req.Header.Set("Authorization", "Bearer "+token)
	if gz {
		req.Header.Set("Content-Encoding", "gzip")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	return res
}

func TestIngestAndListings(t *testing.T) {
	ts := testServer(t)
	start := time.Now().In(scrape.London).Format(scrape.DateFmt) + "T23:59"
	body := `{"results":[{"name":"Odeon Oldham","shows":[{"film":"Test Film","start":"` + start +
		`","screen":"Screen 1","formats":["IMAX"],"url":null,"end":null,"duration":100}],"error":null,"fetched":"` +
		time.Now().UTC().Format(time.RFC3339) + `"}]}`

	for _, c := range []struct {
		name, url, token, body string
		want                   int
	}{
		{"bad token", "/v1/ingest", "nope", body, 401},
		{"unknown cinema", "/v1/ingest", "secret", `{"results":[{"name":"Nowhere","shows":[]}]}`, 400},
		{"server cinema", "/v1/ingest", "secret", `{"results":[{"name":"Cultplex","shows":[]}]}`, 403},
		{"server cinema with any", "/v1/ingest?any=1", "secret", `{"results":[{"name":"Cultplex","shows":[]}]}`, 200},
		{"bad json", "/v1/ingest", "secret", `{`, 400},
	} {
		if res := post(t, ts.URL+c.url, c.token, c.body, false); res.StatusCode != c.want {
			t.Errorf("%s: HTTP %d, want %d", c.name, res.StatusCode, c.want)
		}
	}
	if res := post(t, ts.URL+"/v1/ingest", "secret", body, true); res.StatusCode != 200 {
		t.Fatalf("gzip ingest: HTTP %d", res.StatusCode)
	}

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/v1/listings.json", nil)
	req.Header.Set("Origin", "https://fazon.nexus")
	res, err := http.DefaultClient.Do(req) // the client transparently gunzips
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Region  []string         `json:"region"`
		Regions []map[string]any `json:"regions"`
		Shows   []map[string]any `json:"shows"`
		Cinemas []struct {
			Name  string  `json:"name"`
			Error *string `json:"error"`
			Count int     `json:"count"`
			Stale bool    `json:"stale"`
		} `json:"cinemas"`
	}
	json.NewDecoder(res.Body).Decode(&doc)
	res.Body.Close()
	if res.Header.Get("Access-Control-Allow-Origin") != "https://fazon.nexus" || res.Header.Get("Cache-Control") != "public, max-age=300" {
		t.Errorf("headers: %v", res.Header)
	}
	def := registry.InRegions([]string{"north-west", "yorkshire", "east-midlands"})
	if len(doc.Cinemas) != len(def) || len(doc.Shows) != 1 || !slices.Equal(doc.Region, []string{"north-west", "yorkshire", "east-midlands"}) || len(doc.Regions) != 12 {
		t.Fatalf("cinemas=%d (want %d) shows=%d region=%v regions=%d", len(doc.Cinemas), len(def), len(doc.Shows), doc.Region, len(doc.Regions))
	}
	for _, c := range doc.Cinemas {
		if c.Name == "Odeon Oldham" && (c.Count != 1 || c.Error != nil || c.Stale) {
			t.Errorf("Oldham: %+v", c)
		}
		if c.Name == "Odeon Rochdale" && (!c.Stale || c.Error == nil) {
			t.Errorf("Rochdale should be stale/not fetched: %+v", c)
		}
	}

	etag := res.Header.Get("ETag")
	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/v1/listings.json", nil)
	req.Header.Set("If-None-Match", etag)
	if res, _ := http.DefaultClient.Do(req); res.StatusCode != http.StatusNotModified {
		t.Errorf("If-None-Match: HTTP %d, want 304", res.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodOptions, ts.URL+"/v1/listings.json", nil)
	req.Header.Set("Origin", "https://evil.example")
	res, _ = http.DefaultClient.Do(req)
	if res.StatusCode != http.StatusNoContent || res.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("preflight from foreign origin: %d %v", res.StatusCode, res.Header)
	}
}

func TestListingsRegions(t *testing.T) {
	ts := testServer(t)
	get := func(q string) (int, []string, []string) {
		res, err := http.Get(ts.URL + "/v1/listings.json" + q)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var doc struct {
			Region  []string `json:"region"`
			Cinemas []struct {
				Name   string `json:"name"`
				Region string `json:"region"`
			} `json:"cinemas"`
		}
		json.NewDecoder(res.Body).Decode(&doc)
		var regions []string
		for _, c := range doc.Cinemas {
			if !slices.Contains(regions, c.Region) {
				regions = append(regions, c.Region)
			}
		}
		return res.StatusCode, doc.Region, regions
	}
	if code, sel, got := get("?regions=london,%20scotland,london"); code != 200 || !slices.Equal(sel, []string{"scotland", "london"}) ||
		len(got) != 2 || !slices.Contains(got, "london") || !slices.Contains(got, "scotland") {
		t.Errorf("london+scotland: %d %v %v", code, sel, got)
	}
	if code, _, _ := get("?regions=atlantis"); code != 400 {
		t.Errorf("unknown region: HTTP %d, want 400", code)
	}
	if code, sel, _ := get("?regions="); code != 200 || len(sel) != 3 {
		t.Errorf("empty regions = default: %d %v", code, sel)
	}
}

func TestDue(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dm, _ := openDemand(filepath.Join(t.TempDir(), "d.json"), []string{"north-west"}, 48*time.Hour)
	now := time.Now().UTC().Truncate(time.Second)
	cs := []scrape.Cinema{{Name: "A", Region: "north-west"}, {Name: "B", Region: "north-west"}, {Name: "C", Region: "north-west"},
		{Name: "D", Region: "london"}, {Name: "E", Region: "london"}, {Name: "F", Region: "london"}, {Name: "G", Region: "wales"}}
	apply := func(name string, at time.Time, fail bool) {
		r := scrape.Result{Name: name, Fetched: at}
		if fail {
			r.Error = "boom"
		}
		st.Apply(r, "server")
	}
	apply("B", now.Add(-30*time.Minute), false) // hot, fresh
	apply("C", now.Add(-3*time.Hour), false)    // hot, old
	apply("D", now.Add(-30*time.Hour), false)   // cold, old
	apply("E", now.Add(-26*time.Hour), false)   // cold, old but newer than D
	apply("F", now.Add(-time.Hour), false)      // cold, fresh
	apply("G", now.Add(-50*time.Hour), false)   // cold, oldest
	tr := tiers{hot: 2 * time.Hour, cold: 24 * time.Hour, retry: 2 * time.Hour, slack: 10 * time.Minute}
	names := func(cs []scrape.Cinema) (out []string) {
		for _, c := range cs {
			out = append(out, c.Name)
		}
		return out
	}
	if got := names(due(cs, st, dm, tr, 2, now)); !slices.Equal(got, []string{"A", "C", "G", "D"}) {
		t.Errorf("due = %v, want hot A C then the 2 oldest cold G D", got)
	}
	// A failed attempt waits for the retry interval, even though its data is old.
	apply("C", now.Add(-5*time.Minute), true)
	if got := names(due(cs, st, dm, tr, 0, now)); !slices.Equal(got, []string{"A"}) {
		t.Errorf("after C failed: %v", got)
	}
	// Viewing London makes it hot: E (26 h old) and D are due with no cold budget.
	dm.touch([]string{"london"}, now)
	select {
	case <-dm.kick:
	default:
		t.Error("touching a cold region didn't kick the scraper")
	}
	if got := names(due(cs, st, dm, tr, 0, now)); !slices.Equal(got, []string{"A", "D", "E"}) {
		t.Errorf("london hot: %v", got)
	}
	dm.touch([]string{"london"}, now.Add(time.Second))
	select {
	case <-dm.kick:
		t.Error("touching a hot region kicked again")
	default:
	}
	if err := dm.save(); err != nil {
		t.Fatal(err)
	}
	re, _ := openDemand(dm.path, nil, 48*time.Hour)
	if !re.hot("london", now) || re.hot("wales", now) {
		t.Error("demand not persisted")
	}
}

func TestRelayDue(t *testing.T) {
	ts := testServer(t)
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/v1/relay/due?limit=3", nil)
	if res, _ := http.DefaultClient.Do(req); res.StatusCode != 401 {
		t.Errorf("no token: %d", res.StatusCode)
	}
	req.Header.Set("Authorization", "Bearer secret")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Cinemas   []string `json:"cinemas"`
		Hot, Cold int
	}
	json.NewDecoder(res.Body).Decode(&out)
	res.Body.Close()
	relay := true
	hot := 0
	for _, c := range registry.Select("", &relay) {
		if slices.Contains([]string{"north-west", "yorkshire", "east-midlands"}, c.Region) {
			hot++
		}
	}
	if out.Hot != hot || out.Cold != 3 || len(out.Cinemas) != hot+3 {
		t.Errorf("due: hot %d cold %d (%d names), want hot %d cold 3", out.Hot, out.Cold, len(out.Cinemas), hot)
	}
	for _, n := range out.Cinemas {
		if c, _ := registry.ByName(n); !c.Relay {
			t.Errorf("%q isn't a relay cinema", n)
		}
	}
}

func TestAcceptsGzip(t *testing.T) {
	for h, want := range map[string]bool{"gzip, deflate, br": true, "br": false, "gzip;q=0": false, "": false, "GZIP": true} {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Accept-Encoding", h)
		if got := acceptsGzip(r); got != want {
			t.Errorf("acceptsGzip(%q) = %v", h, got)
		}
	}
	if !etagMatch(`W/"abc", "def"`, `"def"`) || etagMatch(`"abc"`, `"def"`) {
		t.Error("etagMatch")
	}
	allowed := []string{"https://fazon.nexus", "http://localhost:1313", "http://127.0.0.1:*"}
	for o, want := range map[string]bool{"https://fazon.nexus": true, "http://localhost:1313": true, "http://localhost:8080": false,
		"http://127.0.0.1:5500": true, "http://127.0.0.1:": false, "http://127.0.0.1:1/x": false, "https://evil.example": false,
		"https://fazon.nexus.evil.example": false} {
		if got := originAllowed(allowed, o); got != want {
			t.Errorf("originAllowed(%q) = %v", o, got)
		}
	}
}

func TestFeedback(t *testing.T) {
	ts := testServer(t)
	send := func(body string) (int, map[string]any) {
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/feedback", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "https://fazon.nexus")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.Header.Get("Access-Control-Allow-Origin") != "https://fazon.nexus" {
			t.Errorf("POST CORS: %v", res.Header)
		}
		var out map[string]any
		json.NewDecoder(res.Body).Decode(&out)
		return res.StatusCode, out
	}
	if code, _ := send(`{"text":"   "}`); code != 400 {
		t.Errorf("empty: %d", code)
	}
	if code, _ := send(`{"text":"` + strings.Repeat("a", 1001) + `"}`); code != 400 {
		t.Errorf("too long: %d", code)
	}
	if code, _ := send(`{"text":"spam","website":"x"}`); code != 400 {
		t.Errorf("honeypot: %d", code)
	}
	var id string
	for i := 0; i < feedback.Limit; i++ {
		code, out := send(`{"name":"Bo","text":"great planner"}`)
		if code != 201 {
			t.Fatalf("post %d: %d %v", i, code, out)
		}
		id = out["item"].(map[string]any)["id"].(string)
	}
	if code, out := send(`{"text":"sixth"}`); code != 429 || out["remaining"].(float64) != 0 {
		t.Errorf("sixth: %d %v", code, out)
	}

	res, err := http.Get(ts.URL + "/v1/feedback")
	if err != nil {
		t.Fatal(err)
	}
	var st feedback.Status
	json.NewDecoder(res.Body).Decode(&st)
	res.Body.Close()
	if len(st.Items) != feedback.Limit || st.Used != feedback.Limit || st.MaxChars != 1000 || res.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("list: %+v %v", st, res.Header)
	}

	req, _ := http.NewRequest(http.MethodOptions, ts.URL+"/v1/feedback", nil)
	req.Header.Set("Origin", "https://fazon.nexus")
	res, _ = http.DefaultClient.Do(req)
	if res.StatusCode != 204 || res.Header.Get("Access-Control-Allow-Headers") != "Content-Type" {
		t.Errorf("preflight: %d %v", res.StatusCode, res.Header)
	}

	del := func(token, id string) int {
		req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/v1/feedback/"+id, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	reply := func(token, path, body string) (int, map[string]any) {
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/feedback/"+path+"/replies", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Origin", "https://fazon.nexus")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.Header.Get("Access-Control-Allow-Origin") != "https://fazon.nexus" {
			t.Errorf("reply CORS: %v", res.Header)
		}
		var out map[string]any
		json.NewDecoder(res.Body).Decode(&out)
		return res.StatusCode, out
	}
	if c, _ := reply("nope", id, `{"text":"hi"}`); c != 401 {
		t.Errorf("reply bad token: %d", c)
	}
	if c, _ := reply("secret", "missing", `{"text":"hi"}`); c != 404 {
		t.Errorf("reply to missing: %d", c)
	}
	c, out := reply("secret", id, `{"text":"thanks!"}`)
	if c != 201 {
		t.Fatalf("reply: %d %v", c, out)
	}
	if r := out["reply"].(map[string]any); r["name"] != ownerName || r["text"] != "thanks!" {
		t.Errorf("reply body: %v", r)
	}
	rid := out["reply"].(map[string]any)["id"].(string)
	req, _ = http.NewRequest(http.MethodOptions, ts.URL+"/v1/feedback/"+id+"/replies", nil)
	req.Header.Set("Origin", "https://fazon.nexus")
	res, _ = http.DefaultClient.Do(req)
	if res.StatusCode != 204 || res.Header.Get("Access-Control-Allow-Headers") != "Content-Type, Authorization" {
		t.Errorf("owner preflight: %d %v", res.StatusCode, res.Header)
	}
	for tok, want := range map[string]int{"secret": 204, "nope": 401} {
		req, _ = http.NewRequest(http.MethodGet, ts.URL+"/v1/feedback/owner", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		res, _ = http.DefaultClient.Do(req)
		if res.StatusCode != want {
			t.Errorf("owner check %s: %d", tok, res.StatusCode)
		}
	}
	if c := del("secret", id+"/replies/"+rid); c != 204 {
		t.Errorf("delete reply: %d", c)
	}
	if c := del("secret", id+"/replies/"+rid); c != 404 {
		t.Errorf("delete reply again: %d", c)
	}

	if c := del("nope", id); c != 401 {
		t.Errorf("delete bad token: %d", c)
	}
	if c := del("secret", id); c != 204 {
		t.Errorf("delete: %d", c)
	}
	if c := del("secret", id); c != 404 {
		t.Errorf("delete again: %d", c)
	}
}
