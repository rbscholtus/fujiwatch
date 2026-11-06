package adapters

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rbscholtus/fujiwatch/internal/snapshot"
)

var update = flag.Bool("update", false, "rewrite testdata/**/*.golden.json from the current adapters")

const testdataDir = "../../testdata"

// snapshotCase is one saved page plus its metadata.
type snapshotCase struct {
	name string
	body string
	meta snapshot.Meta
}

// snapshots finds every testdata/<shop>/<name>.{html,json} that has a
// .meta.json written by `fujiwatch snapshot`.
func snapshots(t *testing.T) []snapshotCase {
	t.Helper()
	metas, err := filepath.Glob(filepath.Join(testdataDir, "*", "*"+snapshot.MetaSuffix))
	if err != nil {
		t.Fatal(err)
	}
	cases := make([]snapshotCase, 0, len(metas))
	for _, mp := range metas {
		base := strings.TrimSuffix(mp, snapshot.MetaSuffix)
		var body string
		for _, ext := range []string{".html", ".json"} {
			if _, err := os.Stat(base + ext); err == nil {
				body = base + ext
			}
		}
		if body == "" {
			t.Fatalf("%s has no .html or .json body", mp)
		}
		meta, err := snapshot.LoadMeta(body)
		if err != nil {
			t.Fatal(err)
		}
		rel, _ := filepath.Rel(testdataDir, base)
		cases = append(cases, snapshotCase{name: rel, body: body, meta: meta})
	}
	if len(cases) == 0 {
		t.Fatal("no snapshots found in testdata/")
	}
	return cases
}

func extractSnapshot(t *testing.T, c snapshotCase) []Offer {
	t.Helper()
	raw, err := os.ReadFile(c.body)
	if err != nil {
		t.Fatal(err)
	}
	a := WithClock(ForShop(c.meta.Shop), func() time.Time { return c.meta.FetchedAt })
	offers, err := Extract(a, c.meta.PageURL, raw)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return offers
}

// TestGolden compares every snapshot's extracted offers with its
// .golden.json. Run `go test ./internal/adapters -update` after adding a
// snapshot or changing an adapter, and review the diff.
func TestGolden(t *testing.T) {
	for _, c := range snapshots(t) {
		t.Run(c.name, func(t *testing.T) {
			offers := extractSnapshot(t, c)
			if len(offers) == 0 {
				t.Fatal("no offers extracted")
			}
			got, err := json.MarshalIndent(offers, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, '\n')

			goldenPath := filepath.Join(testdataDir, c.name+".golden.json")
			if *update {
				if err := os.WriteFile(goldenPath, got, 0o600); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("%v (run with -update to create it)", err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("offers differ from %s (run with -update and review the diff)\ngot:\n%s", goldenPath, got)
			}
		})
	}
}
