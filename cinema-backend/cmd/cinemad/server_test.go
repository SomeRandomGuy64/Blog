package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"fazon.nexus/cinema/internal/scrape"
	"fazon.nexus/cinema/internal/store"
)

func testServer(t *testing.T) *httptest.Server {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg := config{token: "secret", origins: []string{"https://fazon.nexus"}, maxBody: 1 << 20, days: 7}
	ts := httptest.NewServer(newServer(cfg, st).routes())
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
	if len(doc.Cinemas) != 57 || len(doc.Shows) != 1 {
		t.Fatalf("cinemas=%d shows=%d", len(doc.Cinemas), len(doc.Shows))
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
