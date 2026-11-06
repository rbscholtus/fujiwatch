package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rbscholtus/fujiwatch/internal/fetch"
	"github.com/rbscholtus/fujiwatch/internal/runner"
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

func useSnapshotFetcher(t *testing.T) {
	t.Helper()
	old := fetcherFactory
	t.Cleanup(func() { fetcherFactory = old })
	fetcherFactory = func(fetch.Options) runner.Fetcher {
		return snapshotFetcher{bodies: map[string]string{
			"https://fujifilm-xspace.vn/products/fujinon-xf10-24mmf4-r-ois-wr-ii.js": "../../testdata/xspace/fujinon-xf10-24mmf4-r-ois-wr-ii.json",
			"https://vjshop.vn/do-cu-chat":                                           "../../testdata/vjshop/do-cu-chat.html",
			"https://vjshop.vn/may-anh-fujifilm":                                     "../../testdata/vjshop/may-anh-fujifilm.html",
			"https://api.vjshop.vn/api/products/fujifilm-x-t5-silver-chinh-hang":     "../../testdata/vjshop/fujifilm-x-t5-silver-chinh-hang.json",
		}}
	}
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
	for _, want := range []string{"dry run", "NEW LISTING", "x-t5", "Sources (5)"} {
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
	if len(st.Offers) != 6 {
		t.Errorf("state offers = %d, want 6", len(st.Offers))
	}
	for _, id := range []string{"x-t5", "xf10-24-ii"} {
		recs, err := state.ReadHistory(filepath.Join(data, "history"), id)
		if err != nil || len(recs) == 0 {
			t.Errorf("history %s: %d records, %v", id, len(recs), err)
		}
	}

	// A second identical run: no new_listing events, history grows.
	out.Reset()
	if code := run([]string{"run", "--config", "../../config", "--data", data}, &out, &errOut); code != exitOK {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if strings.Contains(out.String(), "NEW LISTING") {
		t.Errorf("second run reported new listings:\n%s", out.String())
	}
	recs, _ := state.ReadHistory(filepath.Join(data, "history"), "xf10-24-ii")
	if len(recs) != 2 {
		t.Errorf("xf10-24-ii history = %d lines after 2 runs, want 2", len(recs))
	}
}

func TestRunUnknownShop(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"run", "--config", "../../config", "--only", "nope"}, &out, &errOut); code != exitError {
		t.Errorf("exit %d, want %d", code, exitError)
	}
}
