package films

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// TMDB looks titles up in The Movie Database. The key is optional: without one, listings are
// still matched by the title rules and the saved decisions.
type TMDB struct {
	Key    string
	Base   string // default https://api.themoviedb.org/3
	Client *http.Client
}

type tmdbResult struct {
	ID            int    `json:"id"`
	Title         string `json:"title"`
	OriginalTitle string `json:"original_title"`
	ReleaseDate   string `json:"release_date"`
}

// Lookup asks for a title and keeps only an exact match, so a wrong film is never linked. The
// first exact result is the one TMDB ranks highest.
func (t *TMDB) Lookup(ctx context.Context, title string, now time.Time) (Lookup, error) {
	base, client := t.Base, t.Client
	if base == "" {
		base = "https://api.themoviedb.org/3"
	}
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	q := url.Values{"api_key": {t.Key}, "query": {title}, "include_adult": {"false"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/search/movie?"+q.Encode(), nil)
	if err != nil {
		return Lookup{}, errors.New("building the request failed")
	}
	req.Header.Set("Accept", "application/json")
	res, err := client.Do(req)
	if err != nil {
		var ue *url.Error // its message holds the URL, which holds the key
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return Lookup{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return Lookup{}, fmt.Errorf("tmdb answered HTTP %d", res.StatusCode)
	}
	var body struct {
		Results []tmdbResult `json:"results"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return Lookup{}, err
	}
	want := Key(title)
	for _, r := range body.Results {
		if Key(r.Title) == want || Key(r.OriginalTitle) == want {
			y := r.ReleaseDate
			if len(y) >= 4 {
				y = y[:4]
			}
			return Lookup{ID: r.ID, Title: r.Title, Year: y, Key: Key(r.Title), Checked: now}, nil
		}
	}
	return Lookup{Checked: now}, nil
}
