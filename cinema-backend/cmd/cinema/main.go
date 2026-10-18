// Command cinema scrapes listings locally (testing / parity with the Python script).
//
//	cinema scrape [--only odeon,vue] [--days 7] [--from 2026-10-07] [--out listings.json] [--raw-out raw.json]
//	cinema build --raw raw.json --out listings.json   # rebuild from a raw dump (no network)
//	cinema discover [--out internal/registry/chains.json] [--only odeon,vue]   # re-list the chains' cinemas
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"time"
	_ "time/tzdata"

	"fazon.nexus/cinema/internal/listings"
	"fazon.nexus/cinema/internal/registry"
	"fazon.nexus/cinema/internal/scrape"
)

// rawDump is the --raw-out / --raw format (same as the parity tooling's Python dump).
type rawDump struct {
	From    string          `json:"from"`
	To      string          `json:"to"`
	Results []scrape.Result `json:"results"`
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "scrape":
		scrapeCmd(os.Args[2:])
	case "build":
		buildCmd(os.Args[2:])
	case "discover":
		discoverCmd(os.Args[2:])
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: cinema scrape [--only x,y] [--days 7] [--from YYYY-MM-DD] [--out listings.json] [--raw-out raw.json] [--relay|--server]\n       cinema build --raw raw.json [--out listings.json]\n       cinema discover [--out internal/registry/chains.json] [--only odeon,vue]")
	os.Exit(2)
}

func scrapeCmd(args []string) {
	fs := flag.NewFlagSet("scrape", flag.ExitOnError)
	only := fs.String("only", "", "comma-separated substrings of cinema name/chain")
	days := fs.Int("days", scrape.DefaultDays, "days in the window")
	fromS := fs.String("from", "", "first day (default today, Europe/London)")
	out := fs.String("out", "listings.json", "listings JSON output ('' to skip)")
	rawOut := fs.String("raw-out", "", "also write raw per-cinema results here")
	workers := fs.Int("workers", 8, "parallel cinemas")
	relayOnly := fs.Bool("relay", false, "only relay-flagged cinemas")
	serverOnly := fs.Bool("server", false, "only non-relay cinemas")
	fs.Parse(args)

	from := scrape.Day(time.Now().In(scrape.London))
	if *fromS != "" {
		var err error
		if from, err = scrape.ParseDate(*fromS); err != nil {
			fatal(err)
		}
	}
	to := from.AddDate(0, 0, *days-1)
	var relay *bool
	if *relayOnly || *serverOnly {
		relay = relayOnly
	}
	cinemas := registry.Select(*only, relay)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	fmt.Fprintf(os.Stderr, "Fetching %d cinemas, %s → %s\n", len(cinemas), from.Format(scrape.DateFmt), to.Format(scrape.DateFmt))
	results := scrape.Run(ctx, cinemas, from, to, scrape.Options{Workers: *workers, Progress: func(r scrape.Result, n int, took time.Duration) {
		status := "ok  "
		if r.Error != "" {
			status = "ERR "
		}
		fmt.Fprintf(os.Stderr, "  %s %-40s %4d shows %5.1fs   %s\n", status, r.Name, n, took.Seconds(), r.Error)
	}})
	if *rawOut != "" {
		writeJSON(*rawOut, rawDump{from.Format(scrape.DateFmt), to.Format(scrape.DateFmt), results}, true)
	}
	if *out != "" {
		data := listings.Build(cinemas, listings.FromResults(results), from, to, time.Now())
		writeJSON(*out, data, true)
		bad := 0
		for _, c := range data.Cinemas {
			if c.Error != nil {
				bad++
			}
		}
		fmt.Fprintf(os.Stderr, "\n%d showings of %d films across %d/%d cinemas → %s\n",
			len(data.Shows), len(data.Films), len(cinemas)-bad, len(cinemas), *out)
	}
}

func buildCmd(args []string) {
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	raw := fs.String("raw", "", "raw dump to build from")
	out := fs.String("out", "listings.json", "output")
	fs.Parse(args)
	b, err := os.ReadFile(*raw)
	if err != nil {
		fatal(err)
	}
	var d rawDump
	if err := json.Unmarshal(b, &d); err != nil {
		fatal(err)
	}
	from, err1 := scrape.ParseDate(d.From)
	to, err2 := scrape.ParseDate(d.To)
	if err1 != nil || err2 != nil {
		fatal(fmt.Errorf("bad from/to in %s", *raw))
	}
	var cinemas []scrape.Cinema
	for _, r := range d.Results {
		c, ok := registry.ByName(r.Name)
		if !ok {
			fatal(fmt.Errorf("unknown cinema %q", r.Name))
		}
		cinemas = append(cinemas, c)
	}
	writeJSON(*out, listings.Build(cinemas, listings.FromResults(d.Results), from, to, time.Now()), true)
}

func writeJSON(path string, v any, indent bool) {
	f, err := os.Create(path)
	if err != nil {
		fatal(err)
	}
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	if indent {
		enc.SetIndent("", " ")
	}
	if err := enc.Encode(v); err != nil {
		fatal(err)
	}
	if err := f.Close(); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "cinema:", err)
	os.Exit(1)
}
