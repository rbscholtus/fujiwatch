package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var ts = time.Date(2026, 11, 7, 23, 17, 0, 0, time.UTC)

func TestLoadMissingIsEmpty(t *testing.T) {
	st, err := Load(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Offers == nil || st.Sources == nil || len(st.Offers) != 0 {
		t.Errorf("state = %+v, want empty maps", st)
	}
}

func TestLoadCorrupt(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(p, []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil {
		t.Error("expected error for corrupt state")
	}
}

// docs/architecture.md, State, history and events: state.json keyed by shop|url with the documented fields.
func TestSaveFormatAndRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "data", "state.json")

	st := New()
	url := "https://fujifilm-xspace.vn/products/fujinon-xf10-24mmf4-r-ois-wr-ii"
	st.Offers[Key("xspace", url)] = &Offer{
		Product: "xf10-24-ii", Shop: "xspace", URL: url,
		Title: "Ống kính Fujinon XF10-24mm F4 R OIS WR II", PriceVND: 27_490_000, InStock: new(true),
		Condition: "new", Source: Key("xspace", url), FirstSeen: ts, LastSeen: ts,
	}
	st.Sources[Key("xspace", url)] = &Source{LastCount: 1, LastOK: ts}

	if err := st.Save(p); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var generic map[string]map[string]map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatal(err)
	}
	entry := generic["offers"]["xspace|"+url]
	for _, field := range []string{"product", "title", "price_vnd", "in_stock", "condition", "first_seen", "last_seen"} {
		if _, ok := entry[field]; !ok {
			t.Errorf("state entry missing documented field %q", field)
		}
	}
	if entry["first_seen"] != "2026-11-07T23:17:00Z" {
		t.Errorf("first_seen = %v", entry["first_seen"])
	}
	if _, ok := entry["missed"]; ok {
		t.Error("missed=0 should be omitted")
	}

	got, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	o := got.Offers[Key("xspace", url)]
	if o == nil || o.PriceVND != 27_490_000 || !*o.InStock || !o.FirstSeen.Equal(ts) {
		t.Errorf("round trip offer = %+v", o)
	}

	// No temp files left behind.
	entries, _ := os.ReadDir(filepath.Dir(p))
	if len(entries) != 1 {
		t.Errorf("files in data dir = %d, want only state.json", len(entries))
	}
}

// docs/architecture.md, State, history and events: one line per offer per run, in data/history/<product-id>.jsonl.
func TestHistoryAppendAndRead(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "history")
	r1 := Record{TS: ts, Shop: "xspace", URL: "https://x/p", PriceVND: 27_490_000, InStock: new(true), Condition: "new"}
	r2 := Record{TS: ts, Shop: "vjshop", URL: "https://v/p", PriceVND: 26_990_000, Condition: "used"}
	r3 := Record{TS: ts.Add(24 * time.Hour), Shop: "xspace", URL: "https://x/p", PriceVND: 25_990_000, InStock: new(false), Condition: "new"}

	if err := AppendHistory(dir, map[string][]Record{"xf10-24-ii": {r1, r2}, "x-t5": {r2}}); err != nil {
		t.Fatal(err)
	}
	if err := AppendHistory(dir, map[string][]Record{"xf10-24-ii": {r3}}); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "xf10-24-ii.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}
	want := `{"ts":"2026-11-07T23:17:00Z","shop":"xspace","url":"https://x/p","price_vnd":27490000,"in_stock":true,"condition":"new"}`
	if lines[0] != want {
		t.Errorf("line 1 = %s\nwant     %s", lines[0], want)
	}
	if !strings.Contains(lines[1], `"in_stock":null`) {
		t.Errorf("unknown stock should be null: %s", lines[1])
	}

	recs, err := ReadHistory(dir, "xf10-24-ii")
	if err != nil || len(recs) != 3 || recs[2].PriceVND != 25_990_000 {
		t.Errorf("ReadHistory = %+v, %v", recs, err)
	}
	if recs, err := ReadHistory(dir, "nope"); err != nil || recs != nil {
		t.Errorf("missing history = %v, %v", recs, err)
	}
}

func TestHistoryRejectsUnsafeProductID(t *testing.T) {
	for _, id := range []string{"../evil", "a/b", "", "X-T5"} {
		if err := AppendHistory(t.TempDir(), map[string][]Record{id: {{TS: ts}}}); err == nil {
			t.Errorf("product id %q accepted", id)
		}
	}
}

func TestReadHistoryCorruptLine(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x-t5.jsonl"), []byte("{}\nnot json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadHistory(dir, "x-t5"); err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Errorf("err = %v, want line 2 error", err)
	}
}
