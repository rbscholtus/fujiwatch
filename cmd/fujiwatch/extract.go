package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"

	"github.com/rbscholtus/fujiwatch/internal/adapters"
	"github.com/rbscholtus/fujiwatch/internal/match"
	"github.com/rbscholtus/fujiwatch/internal/snapshot"
)

// cmdExtract runs one adapter on a saved file and prints the offers with
// their match result.
func cmdExtract(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("extract", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfgDir := fs.String("config", "config", "directory with watchlist.yaml and sources.yaml")
	pageURL := fs.String("url", "", "page URL the file was fetched for (default: from the .meta.json)")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 2 {
		_, _ = fmt.Fprintln(stderr, usage)
		return exitUsage
	}
	shop, file := fs.Arg(0), fs.Arg(1)

	a := adapters.ForShop(shop)
	if meta, err := snapshot.LoadMeta(file); err == nil {
		if *pageURL == "" {
			*pageURL = meta.PageURL
		}
		a = adapters.WithClock(a, func() time.Time { return meta.FetchedAt })
	}
	if *pageURL == "" {
		return errorf(stderr, "no --url given and no %s found for %s", snapshot.MetaSuffix, file)
	}

	raw, err := os.ReadFile(file)
	if err != nil {
		return errorf(stderr, "%v", err)
	}
	offers, err := adapters.Extract(a, *pageURL, raw)
	if err != nil {
		return errorf(stderr, "%v", err)
	}

	cfg, err := loadConfig(*cfgDir)
	if err != nil {
		return errorf(stderr, "%v", err)
	}
	m, err := match.New(cfg.Watchlist.Products)
	if err != nil {
		return errorf(stderr, "%v", err)
	}

	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "MATCH\tPRICE_VND\tSTOCK\tCOND\tTITLE\tURL")
	for _, o := range offers {
		res := m.Match(o.Title)
		product := res.ProductID
		if res.Status != match.Matched {
			product = "(" + res.Status.String() + ")"
		}
		cond := o.Condition
		if cond == "" {
			cond = match.DetectCondition(o.Title, o.URL) + "?"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%d\t%s\t%s\t%s\t%s\n", product, o.PriceVND, stockString(o.InStock), cond, o.Title, o.URL)
	}
	_ = tw.Flush()
	_, _ = fmt.Fprintf(stdout, "\n%d offers\n", len(offers))
	return exitOK
}

func stockString(b *bool) string {
	switch {
	case b == nil:
		return "?"
	case *b:
		return "yes"
	default:
		return "no"
	}
}
