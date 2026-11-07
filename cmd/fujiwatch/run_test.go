package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rbscholtus/fujiwatch/internal/fetch"
	"github.com/rbscholtus/fujiwatch/internal/runner"
	"github.com/rbscholtus/fujiwatch/internal/snapshot"
	"github.com/rbscholtus/fujiwatch/internal/state"
)

// snapshotFetcher serves the repo's testdata snapshots for the URLs in
// config/sources.yaml; everything else is a 404.
type snapshotFetcher struct{ bodies map[string]string }

func (f snapshotFetcher) Get(_ context.Context, u string) (*fetch.Response, error) {
	p, ok := f.bodies[u]
	if !ok {
		return nil, &fetch.StatusError{URL: u, Code: 404}
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	return &fetch.Response{URL: u, Status: 200, Body: b}, nil
}

func (snapshotFetcher) Allowed(context.Context, string) (bool, error) { return true, nil }

// useSnapshotFetcher serves every testdata snapshot at its recorded fetch
// URL, so `run` over config/sources.yaml works offline.
func useSnapshotFetcher(t *testing.T) {
	t.Helper()
	metas, err := filepath.Glob("../../testdata/*/*" + snapshot.MetaSuffix)
	if err != nil || len(metas) == 0 {
		t.Fatalf("no snapshots: %v", err)
	}
	bodies := map[string]string{}
	for _, mp := range metas {
		base := strings.TrimSuffix(mp, snapshot.MetaSuffix)
		for _, ext := range []string{".html", ".json"} {
			if _, err := os.Stat(base + ext); err != nil {
				continue
			}
			m, err := snapshot.LoadMeta(base + ext)
			if err != nil {
				t.Fatal(err)
			}
			bodies[m.FetchURL] = base + ext
		}
	}
	old := fetcherFactory
	t.Cleanup(func() { fetcherFactory = old })
	fetcherFactory = func(fetch.Options) runner.Fetcher { return snapshotFetcher{bodies: bodies} }
}

func repoProductCount(t *testing.T) int {
	t.Helper()
	cfg, err := loadConfig("../../config")
	if err != nil {
		t.Fatal(err)
	}
	return len(cfg.Watchlist.Products)
}

func repoSourceCount(t *testing.T) int {
	t.Helper()
	cfg, err := loadConfig("../../config")
	if err != nil {
		t.Fatal(err)
	}
	return len(cfg.Sources.Sources)
}

// README §9: --dry-run fetches and diffs but writes nothing.
func TestRunDryRunWritesNothing(t *testing.T) {
	useSnapshotFetcher(t)
	data := t.TempDir()
	var out, errOut bytes.Buffer
	code := run([]string{"run", "--dry-run", "--config", "../../config", "--data", data}, &out, &errOut)
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if entries, _ := os.ReadDir(data); len(entries) != 0 {
		t.Errorf("dry run wrote %d entries to the data dir", len(entries))
	}
	for _, want := range []string{"dry run", "NEW LISTING", "x-t5", fmt.Sprintf("Sources (%d)", repoSourceCount(t))} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q", want)
		}
	}
}

func TestRunWritesStateAndHistory(t *testing.T) {
	useSnapshotFetcher(t)
	data := t.TempDir()
	var out, errOut bytes.Buffer
	if code := run([]string{"run", "--config", "../../config", "--data", data}, &out, &errOut); code != exitOK {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}

	st, err := state.Load(filepath.Join(data, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Offers) < 20 {
		t.Errorf("state offers = %d, want ≥ 20 from all snapshots", len(st.Offers))
	}
	for _, id := range []string{"x-t3", "x-t4", "x-t5", "xf10-24-ii", "tamron-16-50"} {
		recs, err := state.ReadHistory(filepath.Join(data, "history"), id)
		if err != nil || len(recs) == 0 {
			t.Errorf("history %s: %d records, %v", id, len(recs), err)
		}
	}

	first, _ := state.ReadHistory(filepath.Join(data, "history"), "xf10-24-ii")

	// A second identical run: no new_listing events, history grows by one
	// line per offer.
	out.Reset()
	if code := run([]string{"run", "--config", "../../config", "--data", data}, &out, &errOut); code != exitOK {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if strings.Contains(out.String(), "NEW LISTING") {
		t.Errorf("second run reported new listings:\n%s", out.String())
	}
	recs, _ := state.ReadHistory(filepath.Join(data, "history"), "xf10-24-ii")
	if len(first) == 0 || len(recs) != 2*len(first) {
		t.Errorf("xf10-24-ii history = %d lines after 2 runs, %d after 1", len(recs), len(first))
	}
}

func TestRunUnknownShop(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"run", "--config", "../../config", "--only", "nope"}, &out, &errOut); code != exitError {
		t.Errorf("exit %d, want %d", code, exitError)
	}
}
