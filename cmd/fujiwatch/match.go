package main

import (
	"flag"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/rbscholtus/fujiwatch/internal/match"
)

// cmdMatch shows which product a title matches, and why the others don't.
func cmdMatch(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("match", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfgDir := fs.String("config", "config", "directory with watchlist.yaml and sources.yaml")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() < 1 || fs.NArg() > 2 {
		_, _ = fmt.Fprintln(stderr, usage)
		return exitUsage
	}
	title, pageURL := fs.Arg(0), fs.Arg(1)

	cfg, err := loadConfig(*cfgDir)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "fujiwatch: %v\n", err)
		return exitError
	}
	m, err := match.New(cfg.Watchlist.Products)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "fujiwatch: %v\n", err)
		return exitError
	}

	res := m.Match(title)
	_, traces := m.Explain(title)

	_, _ = fmt.Fprintf(stdout, "normalised: %s\n", res.Normalized)
	switch res.Status {
	case match.Matched:
		_, _ = fmt.Fprintf(stdout, "result:     matched %s\n", res.ProductID)
	case match.Ambiguous:
		_, _ = fmt.Fprintf(stdout, "result:     ambiguous (%s)\n", strings.Join(res.Candidates, ", "))
	case match.Unmatched:
		_, _ = fmt.Fprintln(stdout, "result:     unmatched")
	}
	_, _ = fmt.Fprintf(stdout, "condition:  %s\n\n", match.DetectCondition(title, pageURL))

	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	for _, tr := range traces {
		mark := "✗"
		if tr.Matched {
			mark = "✓"
		}
		_, _ = fmt.Fprintf(tw, "  %s\t%s\t%s\n", mark, tr.ProductID, tr.Reason)
	}
	_ = tw.Flush()
	return exitOK
}
