package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/rbscholtus/fujiwatch/internal/adapters"
	"github.com/rbscholtus/fujiwatch/internal/config"
	"github.com/rbscholtus/fujiwatch/internal/fetch"
	"github.com/rbscholtus/fujiwatch/internal/snapshot"
)

// cmdSnapshot fetches a page the way `run` would (including any adapter URL
// rewrite) and saves it under testdata/<shop>/.
func cmdSnapshot(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("snapshot", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfgDir := fs.String("config", "config", "directory with watchlist.yaml and sources.yaml")
	outDir := fs.String("out", "testdata", "snapshot directory")
	shop := fs.String("shop", "", "shop name (default: from sources.yaml by URL or host)")
	mode := fs.String("mode", "", "product or listing (default: from sources.yaml, else listing)")
	noRewrite := fs.Bool("no-rewrite", false, "fetch the page itself, not the adapter's preferred URL")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 1 {
		_, _ = fmt.Fprintln(stderr, usage)
		return exitUsage
	}
	pageURL := fs.Arg(0)

	cfg, err := loadConfig(*cfgDir)
	if err != nil {
		return errorf(stderr, "%v", err)
	}
	s, m, err := resolveSource(cfg, pageURL)
	if err != nil {
		return errorf(stderr, "%v", err)
	}
	if *shop != "" {
		s = *shop
	}
	if *mode != "" {
		m = *mode
	}

	a := adapters.ForShop(s)
	fetchURL := pageURL
	if !*noRewrite {
		if fetchURL, err = adapters.FetchURL(a, pageURL, m); err != nil {
			return errorf(stderr, "%v", err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	client := fetch.New(fetch.Options{Timeout: cfg.Sources.Defaults.Timeout(), Delay: cfg.Sources.Defaults.Delay()})
	if ok, err := client.Allowed(ctx, fetchURL); err != nil {
		return errorf(stderr, "%v", err)
	} else if !ok {
		return errorf(stderr, "%s is disallowed by robots.txt", fetchURL)
	}
	res, err := client.Get(ctx, fetchURL)
	if err != nil {
		return errorf(stderr, "%v", err)
	}

	path, err := snapshot.Save(*outDir, snapshot.Meta{
		Shop:      s,
		Mode:      m,
		PageURL:   pageURL,
		FetchURL:  fetchURL,
		FetchedAt: time.Now().UTC().Truncate(time.Second),
	}, res.Body)
	if err != nil {
		return errorf(stderr, "%v", err)
	}
	_, _ = fmt.Fprintf(stdout, "saved %s (%d bytes, shop %s, mode %s)\n", path, len(res.Body), s, m)
	return exitOK
}

// resolveSource finds the shop and mode for a URL: an exact sources.yaml
// entry first, then any entry on the same host, else a name derived from the
// host and listing mode.
func resolveSource(cfg *config.Config, pageURL string) (shop, mode string, err error) {
	u, err := url.Parse(pageURL)
	if err != nil || u.Host == "" {
		return "", "", fmt.Errorf("invalid url %q", pageURL)
	}
	for _, s := range cfg.Sources.Sources {
		if s.URL == pageURL {
			return s.Shop, s.Mode, nil
		}
	}
	host := strings.TrimPrefix(u.Hostname(), "www.")
	for _, s := range cfg.Sources.Sources {
		if su, err := url.Parse(s.URL); err == nil && strings.TrimPrefix(su.Hostname(), "www.") == host {
			return s.Shop, config.ModeListing, nil
		}
	}
	return strings.SplitN(host, ".", 2)[0], config.ModeListing, nil
}
