package main

import (
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/rbscholtus/fujiwatch/internal/sitedata"
	"github.com/rbscholtus/fujiwatch/internal/state"
)

// cmdSiteData writes the history site's JSON (docs/architecture.md, Price-history site).
func cmdSiteData(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("sitedata", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfgDir := fs.String("config", "config", "directory with watchlist.yaml and sources.yaml")
	dataDir := fs.String("data", "data", "directory with state.json and history/")
	outDir := fs.String("out", "site/public/data", "output directory")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 0 {
		_, _ = fmt.Fprintln(stderr, usage)
		return exitUsage
	}

	cfg, err := loadConfig(*cfgDir)
	if err != nil {
		return errorf(stderr, "%v", err)
	}
	st, err := state.Load(filepath.Join(*dataDir, "state.json"))
	if err != nil {
		return errorf(stderr, "%v", err)
	}
	idx, products, err := sitedata.Build(cfg.Watchlist.Products, st, filepath.Join(*dataDir, "history"), time.Now())
	if err != nil {
		return errorf(stderr, "%v", err)
	}
	if err := sitedata.Write(*outDir, idx, products); err != nil {
		return errorf(stderr, "%v", err)
	}
	_, _ = fmt.Fprintf(stdout, "wrote %d products to %s\n", len(products), *outDir)
	return exitOK
}
