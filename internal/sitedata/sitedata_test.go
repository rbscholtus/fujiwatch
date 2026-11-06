package sitedata

// Spec tests for README §11 (price-history site data).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rbscholtus/fujiwatch/internal/config"
	"github.com/rbscholtus/fujiwatch/internal/state"
)

func ts(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func writeHistory(t *testing.T, dir string, byProduct map[string][]state.Record) {
	t.Helper()
	if err := state.AppendHistory(dir, byProduct); err != nil {
		t.Fatal(err)
	}
}

// "aggregated per day and shop, with new and used as separate series".
func TestAggregate(t *testing.T) {
	recs := []state.Record{
		// 23:17 UTC on Nov 7 is Nov 8 in Vietnam.
		{TS: ts("2026-11-07T23:17:00Z"), Shop: "vjshop", URL: "a", PriceVND: 37_000_000, Condition: "new"},
		{TS: ts("2026-11-07T23:17:00Z"), Shop: "vjshop", URL: "b", PriceVND: 36_500_000, Condition: "new"}, // cheaper listing same day
		{TS: ts("2026-11-07T23:17:00Z"), Shop: "vjshop", URL: "u", PriceVND: 30_000_000, Condition: "used"},
		{TS: ts("2026-11-08T23:17:00Z"), Shop: "vjshop", URL: "a", PriceVND: 36_000_000, Condition: "new"},
		{TS: ts("2026-11-08T23:17:00Z"), Shop: "vjshop", URL: "b", PriceVND: 0, Condition: "new"}, // hidden price
		{TS: ts("2026-11-08T23:17:00Z"), Shop: "kyma", URL: "k", PriceVND: 37_900_000, Condition: "new"},
		// A manual run later the same Vietnam day: the day's minimum wins.
		{TS: ts("2026-11-09T10:00:00Z"), Shop: "kyma", URL: "k", PriceVND: 37_500_000, Condition: "new"},
		{TS: ts("2026-11-09T12:00:00Z"), Shop: "kyma", URL: "k", PriceVND: 37_700_000, Condition: "new"},
		{TS: ts("2026-11-09T23:17:00Z"), Shop: "kyma", URL: "k", PriceVND: 0, Condition: "new"}, // hidden all day
	}
	got := aggregate(recs)

	type pt struct {
		date  string
		price int64
		url   string
	}
	// kyma: 2026-11-08T23:17Z, 2026-11-09T10:00Z and 12:00Z are all Nov 9
	// in Vietnam, so one point at the day's minimum (37.5M); 2026-11-09T23:17Z
	// is Nov 10 with no visible price, so no point at all.
	want := map[string][]pt{
		"new kyma":    {{"2026-11-09", 37_500_000, "k"}},
		"new vjshop":  {{"2026-11-08", 36_500_000, "b"}, {"2026-11-09", 36_000_000, "a"}},
		"used vjshop": {{"2026-11-08", 30_000_000, "u"}},
	}

	if len(got) != 3 {
		t.Fatalf("got %d series, want 3: %+v", len(got), got)
	}
	var order []string
	for _, s := range got {
		k := s.Condition + " " + s.Shop
		order = append(order, k)
		w := want[k]
		if len(s.Points) != len(w) {
			t.Errorf("%s: %d points, want %d: %+v", k, len(s.Points), len(w), s.Points)
			continue
		}
		for i, p := range s.Points {
			if p.Date != w[i].date || p.PriceVND != w[i].price || p.URL != w[i].url {
				t.Errorf("%s point %d = %+v, want %+v", k, i, p, w[i])
			}
		}
	}
	if !slices.Equal(order, []string{"new kyma", "new vjshop", "used vjshop"}) {
		t.Errorf("series order = %v", order)
	}
}

func testProducts() []config.Product {
	return []config.Product{
		{ID: "x-t5", Name: "Fujifilm X-T5", Kind: "body", TargetUsedVND: 30_000_000},
		{ID: "x-t3", Name: "Fujifilm X-T3", Kind: "body"},
	}
}

func testState() *state.State {
	st := state.New()
	add := func(shop, url, product, cond string, p int64, stock *bool) {
		st.Offers[state.Key(shop, url)] = &state.Offer{Product: product, Shop: shop, URL: url, Title: url, Condition: cond, PriceVND: p, InStock: stock}
	}
	add("kyma", "https://kyma.vn/x-t5", "x-t5", "new", 37_900_000, nil)
	add("vjshop", "https://vjshop.vn/x-t5", "x-t5", "new", 36_990_000, new(false)) // cheapest but sold out
	add("zshop", "https://zshop.vn/x-t5", "x-t5", "new", 0, new(true))             // no price
	add("mayanh24h", "https://mayanh24h.com/x-t5-cu", "x-t5", "used", 32_990_000, new(true))
	add("vjshop", "https://vjshop.vn/x-e5", "x-e5", "new", 33_990_000, new(true)) // not watched
	return st
}

func TestBuild(t *testing.T) {
	hist := filepath.Join(t.TempDir(), "history")
	writeHistory(t, hist, map[string][]state.Record{
		"x-t5": {
			{TS: ts("2026-11-07T23:17:00Z"), Shop: "kyma", URL: "https://kyma.vn/x-t5", PriceVND: 37_900_000, Condition: "new"},
			{TS: ts("2026-11-08T23:17:00Z"), Shop: "kyma", URL: "https://kyma.vn/x-t5", PriceVND: 37_500_000, Condition: "new"},
		},
	})
	now := ts("2026-11-09T00:00:00Z")
	idx, products, err := Build(testProducts(), testState(), hist, now)
	if err != nil {
		t.Fatal(err)
	}

	if !idx.Generated.Equal(now) || len(idx.Products) != 2 || idx.Products[0].ID != "x-t5" || idx.Products[1].ID != "x-t3" {
		t.Fatalf("index = %+v (products must follow watchlist order)", idx)
	}
	xt5 := idx.Products[0]
	if xt5.Points != 2 || xt5.Name != "Fujifilm X-T5" || xt5.Kind != "body" {
		t.Errorf("x-t5 entry = %+v", xt5)
	}

	// Offers: priced cheapest first, unpriced last; other products excluded.
	urls := make([]string, 0, len(xt5.Offers))
	for _, o := range xt5.Offers {
		urls = append(urls, o.URL)
	}
	wantURLs := []string{"https://mayanh24h.com/x-t5-cu", "https://vjshop.vn/x-t5", "https://kyma.vn/x-t5", "https://zshop.vn/x-t5"}
	if !slices.Equal(urls, wantURLs) {
		t.Errorf("offers = %v\nwant     %v", urls, wantURLs)
	}

	// Best: skips sold-out and unpriced offers; unknown stock is allowed.
	if b := xt5.Best["new"]; b.URL != "https://kyma.vn/x-t5" {
		t.Errorf("best new = %+v", b)
	}
	if b := xt5.Best["used"]; b.PriceVND != 32_990_000 {
		t.Errorf("best used = %+v", b)
	}

	// A product without history or offers still gets an entry and a file.
	if xt3 := idx.Products[1]; len(xt3.Offers) != 0 || len(xt3.Best) != 0 || xt3.Points != 0 {
		t.Errorf("x-t3 entry = %+v", xt3)
	}
	if len(products) != 2 || len(products[0].Series) != 1 || products[1].Series == nil {
		t.Errorf("products = %+v", products)
	}
}

// The JSON shape is the contract with site/src/*.svelte: every field the
// app reads must be present in the files Write produces.
func TestWriteMatchesSiteContract(t *testing.T) {
	hist := filepath.Join(t.TempDir(), "history")
	writeHistory(t, hist, map[string][]state.Record{
		"x-t5": {{TS: ts("2026-11-07T23:17:00Z"), Shop: "kyma", URL: "u", PriceVND: 37_900_000, Condition: "new"}},
	})
	idx, products, err := Build(testProducts(), testState(), hist, ts("2026-11-09T00:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if err := Write(out, idx, products); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"index.json", "x-t5.json", "x-t3.json"} {
		if _, err := os.Stat(filepath.Join(out, f)); err != nil {
			t.Errorf("missing %s", f)
		}
	}

	var index map[string]any
	readJSON(t, filepath.Join(out, "index.json"), &index)
	p0 := first(t, index["products"])
	offer := first(t, p0["offers"])
	best := obj(t, obj(t, p0["best"])["new"])

	var prod map[string]any
	readJSON(t, filepath.Join(out, "x-t5.json"), &prod)
	s0 := first(t, prod["series"])
	pt := first(t, s0["points"])

	need := map[string]map[string]any{
		"index":   index,
		"product": p0,
		"offer":   offer,
		"best":    best,
		"series":  s0,
		"point":   pt,
	}
	fields := map[string][]string{
		"index":   {"generated", "products"},
		"product": {"id", "name", "best", "offers", "targets"},
		"offer":   {"shop", "url", "title", "condition", "price_vnd", "in_stock", "first_seen"},
		"best":    {"shop", "url", "price_vnd", "in_stock"},
		"series":  {"shop", "condition", "points"},
		"point":   {"date", "price_vnd", "url"},
	}
	for obj, keys := range fields {
		for _, k := range keys {
			if _, ok := need[obj][k]; !ok {
				t.Errorf("%s JSON lacks %q, which the site reads", obj, k)
			}
		}
	}
	if d, _ := pt["date"].(string); !regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`).MatchString(d) {
		t.Errorf("point date %v is not YYYY-MM-DD", pt["date"])
	}

	// And the site really reads these names (keeps both sides in sync).
	var src strings.Builder
	for _, f := range []string{"App.svelte", "Chart.svelte"} {
		b, err := os.ReadFile(filepath.Join("../../site/src", f))
		if err != nil {
			t.Fatal(err)
		}
		src.Write(b)
	}
	if obj(t, p0["targets"])["used"] != float64(30_000_000) {
		t.Errorf("targets = %v", p0["targets"])
	}
	for _, k := range []string{".targets", ".generated", ".products", ".best", ".offers", ".price_vnd", ".in_stock", ".first_seen", ".series", ".points", ".date", ".condition", ".shop", "./data/index.json", "./data/${id}.json"} {
		if !strings.Contains(src.String(), k) {
			t.Errorf("site source no longer uses %q; update the contract", k)
		}
	}
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatal(err)
	}
}

func TestBuildCorruptHistory(t *testing.T) {
	hist := t.TempDir()
	if err := os.WriteFile(filepath.Join(hist, "x-t5.jsonl"), []byte("nope\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Build(testProducts(), state.New(), hist, time.Now()); err == nil {
		t.Error("expected error for corrupt history")
	}
}

func obj(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("want a JSON object, got %T", v)
	}
	return m
}

func first(t *testing.T, v any) map[string]any {
	t.Helper()
	a, ok := v.([]any)
	if !ok || len(a) == 0 {
		t.Fatalf("want a non-empty JSON array, got %v", v)
	}
	return obj(t, a[0])
}
