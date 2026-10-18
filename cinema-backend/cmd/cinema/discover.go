package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"strings"

	"fazon.nexus/cinema/internal/chainfile"
	"fazon.nexus/cinema/internal/discover"
)

// discoverCmd re-lists every chain cinema from the chains' own sites and merges the result into
// --out (previous names are kept; a failed chain keeps its previous entries).
func discoverCmd(args []string) {
	fl := flag.NewFlagSet("discover", flag.ExitOnError)
	out := fl.String("out", "internal/registry/chains.json", "chains file to merge into and write")
	only := fl.String("only", "", "comma-separated chains (substrings, e.g. odeon,light)")
	fl.Parse(args)
	prev, err := chainfile.Load(*out)
	if errors.Is(err, fs.ErrNotExist) {
		prev, err = chainfile.File{}, nil
	}
	if err != nil {
		fatal(fmt.Errorf("reading %s: %w", *out, err))
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	var keys []string
	if *only != "" {
		keys = strings.Split(*only, ",")
	}
	f, sums, err := discover.Run(ctx, prev, discover.Options{Only: keys, Log: os.Stderr})
	fmt.Fprintln(os.Stderr)
	discover.PrintSummary(os.Stderr, f, sums)
	if err != nil {
		fatal(err)
	}
	if err := chainfile.Save(*out, f); err != nil {
		fatal(err)
	}
	fmt.Fprintf(os.Stderr, "wrote %s\n", *out)
}
