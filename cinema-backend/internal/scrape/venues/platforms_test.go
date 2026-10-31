package venues

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"

	"fazon.nexus/cinema/internal/scrape"
)

// Samples in testdata/ are trimmed copies of the live pages (2026-10-30).

func sample(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func shows(t *testing.T, got []scrape.Show, err error, n int) {
	t.Helper()
	if err != nil || len(got) != n {
		t.Fatalf("%d shows, %v; want %d", len(got), err, n)
	}
}

func TestAdmitOneArc(t *testing.T) { // panel_<epoch>, "13:20 - 15:35" ranges, perf-type icons
	doc, _ := scrape.Doc(sample(t, "admitone_arc.html"))
	got, err := admitOneParse(doc, "https://blackpool.arccinema.co.uk", d("2026-10-30"), d("2026-10-30"), d("2026-11-14"))
	shows(t, got, err, 7)
	if s := got[1]; s.Film != "Everest: The Other Side (IMAX)" || s.Start != "2026-10-31T13:20" || s.Duration != 135 ||
		s.End != "" || !slices.Equal(s.Formats, []string{"IMAX"}) || !strings.HasSuffix(s.URL, "perfCode=15895") {
		t.Errorf("IMAX show = %+v", s)
	}
	if s := got[3]; s.Start != "2026-11-01T10:15" || !slices.Equal(s.Formats, []string{"Relaxed"}) {
		t.Errorf("sensory show = %+v", s)
	}
	if s := got[6]; s.Film != "RBO 2026-27: The Royal Ballet - Manon" || !slices.Equal(s.Formats, []string{"Live/Event Cinema"}) { // cp1252 dash fixed
		t.Errorf("event show = %+v", s)
	}
	if got, _ = admitOneParse(doc, "https://x", d("2026-10-30"), d("2026-11-01"), d("2026-11-14")); len(got) != 4 {
		t.Errorf("window from 1 Nov: %d shows, want 4", len(got))
	}
}

func TestAdmitOneBarnsley(t *testing.T) { // panel<n> dated by the n-th tab: "Tomorrow", then "Sun 01 Nov"
	doc, _ := scrape.Doc(sample(t, "admitone_barnsley.html"))
	got, err := admitOneParse(doc, "https://barnsley.parkwaycinemas.co.uk", d("2026-10-30"), d("2026-10-30"), d("2026-11-14"))
	shows(t, got, err, 7)
	if s := got[0]; s.Start != "2026-10-31T13:45" || s.Duration != 100 || s.URL != "https://barnsleyparkway.admit-one.eu/tickets/?perfCode=33872" {
		t.Errorf("first show = %+v", s)
	}
	if got[3].Start != "2026-11-01T13:00" {
		t.Errorf("Sun 01 Nov tab dated %s", got[3].Start)
	}
}

func TestAdmitOneLockworks(t *testing.T) { // panel_<yyyymmdd>, bare times, a desktop and a mobile copy of each block
	doc, _ := scrape.Doc(sample(t, "admitone_lockworks.html"))
	got, err := admitOneParse(doc, "https://lockworkscinema.co.uk", d("2026-10-30"), d("2026-10-30"), d("2026-11-14"))
	shows(t, got, err, 3)
	if s := got[1]; s.Film != "Carrie" || s.Start != "2026-10-31T17:45" || s.Duration != 98 {
		t.Errorf("Carrie = %+v", s)
	}
}

func TestGlitterfish(t *testing.T) {
	got, err := glitterfishParse(sample(t, "glitterfish.js"), "https://cleethorpes.parkwaycinemas.co.uk", d("2026-10-30"), d("2026-11-14"))
	shows(t, got, err, 14)
	if s := got[0]; s.Film != "Room on the Broom" || s.Start != "2026-10-31T11:00" || s.Duration != 25 ||
		!strings.Contains(s.URL, "txtsessionid=") {
		t.Errorf("first show = %+v", s)
	}
	last := got[13]
	if last.Film != "Golden Boy" || last.Start != "2026-11-05T19:00" || last.Duration != 180 || !slices.Equal(last.Formats, []string{"Live/Event Cinema"}) {
		t.Errorf("event show = %+v", last)
	}
	if s := got[12]; !slices.Equal(s.Formats, []string{"Relaxed"}) || s.Duration != 111 { // "1h 51m"
		t.Errorf("relaxed show = %+v", s)
	}
	if got, _ = glitterfishParse(sample(t, "glitterfish.js"), "x", d("2026-11-02"), d("2026-11-02")); len(got) != 1 {
		t.Errorf("single day: %d shows, want 1", len(got))
	}
	if _, err = glitterfishParse([]byte("<html>blocked</html>"), "x", d("2026-11-02"), d("2026-11-02")); err == nil {
		t.Error("a page without data should fail")
	}
}

func TestJacroPrinceCharles(t *testing.T) { // "Saturday 31st October" headings, "10:00 am", tag spans
	doc, _ := scrape.Doc(sample(t, "jacro_pcc.html"))
	got, err := jacroParse(doc, "https://princecharlescinema.com/whats-on/", d("2026-10-30"), d("2026-10-30"), d("2026-11-14"))
	shows(t, got, err, 6)
	if s := got[0]; s.Film != "Perfect Blue" || s.Start != "2026-10-31T10:00" || s.Duration != 81 ||
		!slices.Equal(s.Formats, []string{"4K", "Subtitled"}) || !strings.HasSuffix(s.URL, "/booknow/31627688") {
		t.Errorf("first show = %+v", s)
	}
	if s := got[1]; s.Start != "2026-11-01T18:05" { // 6:05 pm
		t.Errorf("pm time = %+v", s)
	}
	if s := got[3]; s.Film != "The Odyssey" || !slices.Equal(s.Formats, []string{"70mm"}) || s.Duration != 173 {
		t.Errorf("70mm show = %+v", s)
	}
}

func TestJacroCottageRoad(t *testing.T) { // "Sat 31st Oct:" labels, 24-hour buttons
	doc, _ := scrape.Doc(sample(t, "jacro_cottage.html"))
	got, err := jacroParse(doc, "https://cottageroad.co.uk/", d("2026-10-30"), d("2026-10-30"), d("2026-11-14"))
	shows(t, got, err, 12)
	if s := got[0]; s.Film != "Forgotten Island" || s.Start != "2026-10-31T14:30" || s.Duration != 109 || s.URL != "https://www.cottageroad.co.uk/booknow/6764" {
		t.Errorf("first show = %+v", s)
	}
	if got, _ = jacroParse(doc, "https://cottageroad.co.uk/", d("2026-10-30"), d("2026-11-03"), d("2026-11-03")); len(got) != 2 {
		t.Errorf("3 Nov: %d shows, want 2", len(got))
	}
}

func TestJacroCinemac(t *testing.T) { // "Sunday, 1st November" labels, each link repeated in a hidden category list
	doc, _ := scrape.Doc(sample(t, "jacro_cinemac.html"))
	got, err := jacroParse(doc, "https://cinemac.org.uk/", d("2026-10-30"), d("2026-10-30"), d("2026-11-14"))
	shows(t, got, err, 3)
	if s := got[0]; s.Film != "Wildwood" || s.Start != "2026-11-01T17:00" || s.Duration != 130 {
		t.Errorf("first show = %+v", s)
	}
	if s := got[2]; s.Start != "2026-11-05T19:00" || !slices.Equal(s.Formats, []string{"Live/Event Cinema"}) {
		t.Errorf("event show = %+v", s)
	}
}

func TestLabelDate(t *testing.T) {
	ref := d("2026-10-30")
	for in, want := range map[string]string{"Sat 31 st Oct:": "2026-10-31", "Sunday, 1st November": "2026-11-01",
		"Sun 01 Nov": "2026-11-01", "Saturday 31st October": "2026-10-31", "Sat 17 Jul": "2027-07-17"} {
		if got, ok, err := labelDate(in, ref); err != nil || !ok || !got.Equal(d(want)) {
			t.Errorf("labelDate(%q) = %v, %v, %v; want %s", in, got, ok, err, want)
		}
	}
	if _, ok, _ := labelDate("Now Playing", ref); ok {
		t.Error("labelDate(Now Playing) should not be a date")
	}
}

func TestJacroRelativeDatesAndBaby(t *testing.T) { // "Today"/"Tomorrow" headings use the fetch date, not the window start
	html := `<div class="jacro-event"><h3><a>Rosemary's Baby</a></h3><ul class="performance-list-items">
<div class="heading">Today</div><li><a href="/booknow/1">6:00 pm</a></li>
<div class="heading">Tomorrow</div><li><a href="/booknow/2">20:30</a></li>
<div class="heading">Sat 7th Nov</div><li><a href="/booknow/3">14:00</a></li></ul></div>`
	doc, _ := scrape.Doc([]byte(html))
	got, err := jacroParse(doc, "https://x/", d("2026-10-30"), d("2026-10-30"), d("2026-11-14"))
	shows(t, got, err, 3)
	if got[0].Start != "2026-10-30T18:00" || got[1].Start != "2026-10-31T20:30" {
		t.Errorf("relative days = %s, %s", got[0].Start, got[1].Start)
	}
	for _, s := range got {
		if slices.Contains(s.Formats, "Parent & Baby") {
			t.Errorf("%q tagged %v", s.Film, s.Formats)
		}
	}
	if got, _ = jacroParse(doc, "https://x/", d("2026-10-30"), d("2026-10-31"), d("2026-10-31")); len(got) != 1 { // from is not "today"
		t.Errorf("window 31 Oct: %d shows, want 1", len(got))
	}
}

func TestTitleTagsBaby(t *testing.T) {
	for in, want := range map[string][]string{"The Boss Baby": {}, "Rosemary's Baby": {},
		"Parent & Baby: Wicked": {"Parent & Baby"}, "Parent and Baby Screening - Dune": {"Parent & Baby"},
		"RBO 2026-27: The Royal Ballet - Manon": {"Live/Event Cinema"}} {
		if got := titleTags(in); !slices.Equal(got, want) {
			t.Errorf("titleTags(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTaposappOptionalPages(t *testing.T) {
	page := `<div class="programme-film"><div class="programme-film-title"><a href="/film/a">Film A</a></div>
<div class="programme-showtime-date">31st Oct</div><div class="programme-performance-times">
<a class="programme-performance-time" href="/book/1">19:55</a></div></div>`
	var hits []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, r.URL.Path)
		if r.URL.Path == "/" || r.URL.Path == "/coming-soon" {
			w.Write([]byte(page))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	s, err := scrape.NewSession(context.Background(), scrape.NewMemo())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := Taposapp(srv.URL+"/")(s, d("2026-10-30"), d("2026-11-14")) // event-cinema 404s: harmless
	shows(t, got, err, 1)
	if len(hits) != 3 {
		t.Errorf("default pages: %v", hits)
	}
	hits = nil
	if _, err = Taposapp(srv.URL+"/", "", "coming-soon")(s, d("2026-10-30"), d("2026-11-14")); err != nil || len(hits) != 2 {
		t.Errorf("listed pages: %v, %v", hits, err)
	}
	if _, err = Taposapp(srv.URL+"/x/")(s, d("2026-10-30"), d("2026-11-14")); err == nil { // main page 404 still fails
		t.Error("main page 404 should fail")
	}
}
