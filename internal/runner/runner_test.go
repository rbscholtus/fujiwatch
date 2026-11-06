package runner

import (
	"bytes"
	"context"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rbscholtus/fujiwatch/internal/adapters"
	"github.com/rbscholtus/fujiwatch/internal/config"
	"github.com/rbscholtus/fujiwatch/internal/diff"
	"github.com/rbscholtus/fujiwatch/internal/fetch"
	"github.com/rbscholtus/fujiwatch/internal/state"
)

// fakeFetcher serves canned bodies by URL.
type fakeFetcher struct {
	bodies     map[string][]byte
	errs       map[string]error
	disallowed map[string]bool
	gets       []string
}

func (f *fakeFetcher) Get(_ context.Context, u string) (*fetch.Response, error) {
	f.gets = append(f.gets, u)
	if err := f.errs[u]; err != nil {
		return nil, err
	}
	b, ok := f.bodies[u]
	if !ok {
		return nil, &fetch.StatusError{URL: u, Code: 404}
	}
	return &fetch.Response{URL: u, Status: 200, Body: b}, nil
}

func (f *fakeFetcher) Allowed(_ context.Context, u string) (bool, error) {
	return !f.disallowed[u], nil
}

const (
	xspaceURL  = "https://fujifilm-xspace.vn/products/fujinon-xf10-24mmf4-r-ois-wr-ii"
	vjUsedURL  = "https://vjshop.vn/do-cu-chat"
	vjFujiURL  = "https://vjshop.vn/may-anh-fujifilm"
	vjXT5URL   = "https://vjshop.vn/fujifilm-x-t5-silver-chinh-hang"
	vjXT5API   = "https://api.vjshop.vn/api/products/fujifilm-x-t5-silver-chinh-hang"
	brokenURL  = "https://kyma.vn/may-anh-fujifilm"
	blockedURL = "https://vjshop.vn/may-anh-cu"
)

var runTime = time.Date(2026, 11, 7, 23, 17, 0, 0, time.UTC)

func readFile(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile("../../testdata/" + p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func testConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Load("../../config/watchlist.yaml", "../../config/sources.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Sources.Sources = []config.Source{
		{Shop: "xspace", Mode: config.ModeProduct, Product: "xf10-24-ii", Condition: config.ConditionNew, URL: xspaceURL},
		{Shop: "vjshop", Mode: config.ModeListing, Condition: config.ConditionUsed, URL: vjUsedURL},
		{Shop: "vjshop", Mode: config.ModeListing, Condition: config.ConditionAuto, URL: vjFujiURL},
		{Shop: "kyma", Mode: config.ModeListing, Condition: config.ConditionAuto, URL: brokenURL},
		{Shop: "vjshop", Mode: config.ModeListing, Condition: config.ConditionUsed, URL: blockedURL},
	}
	return cfg
}

func testFetcher(t *testing.T) *fakeFetcher {
	t.Helper()
	return &fakeFetcher{
		bodies: map[string][]byte{
			xspaceURL + ".js": readFile(t, "xspace/fujinon-xf10-24mmf4-r-ois-wr-ii.json"),
			vjUsedURL:         readFile(t, "vjshop/do-cu-chat.html"),
			vjFujiURL:         readFile(t, "vjshop/may-anh-fujifilm.html"),
			vjXT5API:          readFile(t, "vjshop/fujifilm-x-t5-silver-chinh-hang.json"),
		},
		errs:       map[string]error{brokenURL: &fetch.StatusError{URL: brokenURL, Code: 503}},
		disallowed: map[string]bool{blockedURL: true},
	}
}

func countType(evs []diff.Event, typ diff.Type) int {
	n := 0
	for _, e := range evs {
		if e.Type == typ {
			n++
		}
	}
	return n
}

// One full cycle over real snapshots (README §2 pipeline).
func TestRunFirstCycle(t *testing.T) {
	cfg := testConfig(t)
	f := testFetcher(t)
	st := state.New()

	rep, hist, err := Run(t.Context(), cfg, st, f, Options{Now: func() time.Time { return runTime }})
	if err != nil {
		t.Fatal(err)
	}

	// The product source fetches the adapter's JSON endpoint; the blocked
	// URL is never fetched.
	if !slices.Contains(f.gets, xspaceURL+".js") || slices.Contains(f.gets, blockedURL) {
		t.Errorf("fetched %v", f.gets)
	}

	// Sources: 3 ok, 2 failing (HTTP 503, robots.txt).
	if len(rep.Sources) != 5 {
		t.Fatalf("sources = %d", len(rep.Sources))
	}
	for _, s := range rep.Sources {
		wantErr := s.URL == brokenURL || s.URL == blockedURL
		if (s.Err != "") != wantErr {
			t.Errorf("source %s err = %q", s.URL, s.Err)
		}
	}
	if s := rep.Sources[3]; !strings.Contains(s.Err, "503") {
		t.Errorf("broken source err = %q", s.Err)
	}
	if s := rep.Sources[4]; !strings.Contains(s.Err, "robots.txt") {
		t.Errorf("blocked source err = %q", s.Err)
	}

	// Events: 5 new X-T5 listings, 2 broken sources, nothing for the
	// product-mode source on its first run.
	if n := countType(rep.Events, diff.NewListing); n != 5 {
		t.Errorf("new_listing = %d, want 5", n)
	}
	if n := countType(rep.Events, diff.AdapterBroken); n != 2 {
		t.Errorf("adapter_broken = %d, want 2", n)
	}
	if len(rep.Events) != 7 {
		t.Errorf("events = %d, want 7", len(rep.Events))
	}

	// Unmatched: every non-watched offer from both listings (6 used + 31 new).
	if len(rep.Unmatched) != 37 {
		t.Errorf("unmatched = %d, want 37", len(rep.Unmatched))
	}

	// History: one record per matched offer.
	if len(hist["xf10-24-ii"]) != 1 || len(hist["x-t5"]) != 5 {
		t.Errorf("history = xf10-24-ii:%d x-t5:%d", len(hist["xf10-24-ii"]), len(hist["x-t5"]))
	}
	if r := hist["xf10-24-ii"][0]; r.PriceVND != 27_490_000 || !r.TS.Equal(runTime) || r.Condition != "new" || r.URL != xspaceURL {
		t.Errorf("xspace history record = %+v", r)
	}

	// State: the xspace product is keyed by its page URL, not the .js URL.
	if o := st.Offers[state.Key("xspace", xspaceURL)]; o == nil || o.Product != "xf10-24-ii" {
		t.Errorf("xspace state = %+v", o)
	}
	if len(st.Offers) != 6 {
		t.Errorf("state offers = %d, want 6", len(st.Offers))
	}
}

// Second cycle: xspace drops its price and sells out; a used listing page
// starts returning nothing.
func TestRunSecondCycleEvents(t *testing.T) {
	cfg := testConfig(t)
	f := testFetcher(t)
	st := state.New()
	if _, _, err := Run(t.Context(), cfg, st, f, Options{Now: func() time.Time { return runTime }}); err != nil {
		t.Fatal(err)
	}

	f.bodies[xspaceURL+".js"] = []byte(`{"title":"Ống kính Fujinon XF10-24mm F4 R OIS WR II","price":2599000000,"available":false,"variants":[{"price":2599000000,"available":false}]}`)
	f.bodies[vjUsedURL] = []byte(`<html><script id="__NEXT_DATA__" type="application/json">{"props":{"pageProps":{"products":[]}}}</script></html>`)

	rep, _, err := Run(t.Context(), cfg, st, f, Options{Now: func() time.Time { return runTime.Add(24 * time.Hour) }})
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(rep.Events))
	for _, e := range rep.Events {
		got = append(got, string(e.Type)+" "+e.Shop)
	}
	slices.Sort(got)
	want := []string{
		"adapter_broken kyma",
		"adapter_broken vjshop", // robots.txt
		"adapter_broken vjshop", // used page: 0 offers after 6
		"out_of_stock xspace",
		"price_drop xspace",
	}
	if !slices.Equal(got, want) {
		t.Errorf("events =\n  %s\nwant\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
	for _, e := range rep.Events {
		if e.Type == diff.PriceDrop && (e.OldPrice != 27_490_000 || e.PriceVND != 25_990_000) {
			t.Errorf("price_drop = %d → %d", e.OldPrice, e.PriceVND)
		}
	}
}

func TestRunOnlyShop(t *testing.T) {
	f := testFetcher(t)
	rep, _, err := Run(t.Context(), testConfig(t), state.New(), f, Options{Only: "xspace", Now: func() time.Time { return runTime }})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Sources) != 1 || rep.Sources[0].Shop != "xspace" || len(f.gets) != 1 {
		t.Errorf("sources = %+v, gets = %v", rep.Sources, f.gets)
	}
}

// A product page whose title doesn't match its configured product is
// flagged (wrong URL in sources.yaml), but still tracked.
func TestRunProductMismatch(t *testing.T) {
	cfg := testConfig(t)
	cfg.Sources.Sources = []config.Source{
		{Shop: "vjshop", Mode: config.ModeProduct, Product: "x-t4", Condition: config.ConditionNew, URL: vjXT5URL},
	}
	rep, hist, err := Run(t.Context(), cfg, state.New(), testFetcher(t), Options{Now: func() time.Time { return runTime }})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Unmatched) != 1 || rep.Unmatched[0].Status != "mismatch" || !slices.Equal(rep.Unmatched[0].Candidates, []string{"x-t5"}) {
		t.Errorf("unmatched = %+v", rep.Unmatched)
	}
	if len(hist["x-t4"]) != 1 || hist["x-t4"][0].PriceVND != 36_990_000 {
		t.Errorf("history = %+v", hist)
	}
}

func TestRunCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := Run(ctx, testConfig(t), state.New(), testFetcher(t), Options{}); err == nil {
		t.Error("expected error for cancelled context")
	}
}

// README §6 condition detection and source condition precedence.
func TestResolveCondition(t *testing.T) {
	tests := []struct {
		offerCond, srcCond, title, url, want string
	}{
		{"used", config.ConditionNew, "X-T5", "https://s/p", "used"}, // shop says used
		{"", config.ConditionUsed, "X-T5 Chính hãng", "https://s/p", "used"},
		{"", config.ConditionNew, "X-T5 cũ 98%", "https://s/p", "new"}, // source wins over title
		{"", config.ConditionAuto, "X-T5 cũ 98%", "https://s/p", "used"},
		{"", config.ConditionAuto, "X-T5 Chính hãng", "https://s/may-anh-cu/x-t5", "used"},
		{"", config.ConditionAuto, "X-T5 Chính hãng", "https://s/x-t5", "new"},
	}
	for _, tt := range tests {
		o := adapters.Offer{Condition: tt.offerCond, Title: tt.title, URL: tt.url}
		if got := resolveCondition(o, config.Source{Condition: tt.srcCond}); got != tt.want {
			t.Errorf("resolveCondition(%+v, %s) = %s, want %s", o, tt.srcCond, got, tt.want)
		}
	}
}

func TestWriteText(t *testing.T) {
	cfg := testConfig(t)
	f := testFetcher(t)
	st := state.New()
	if _, _, err := Run(t.Context(), cfg, st, f, Options{Now: func() time.Time { return runTime }}); err != nil {
		t.Fatal(err)
	}
	f.bodies[xspaceURL+".js"] = []byte(`{"title":"XF10-24 WR II","price":2599000000,"available":true}`)
	rep, _, err := Run(t.Context(), cfg, st, f, Options{Only: "xspace", Now: func() time.Time { return runTime.Add(time.Hour) }})
	if err != nil {
		t.Fatal(err)
	}

	var b bytes.Buffer
	if err := WriteText(&b, rep, true); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{
		"(dry run",
		"Events (1)",
		"PRICE DROP",
		"27.490.000₫ → 25.990.000₫ (-5.5%)",
		"Sources (1)",
		"ok  xspace",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %q:\n%s", want, out)
		}
	}
}
