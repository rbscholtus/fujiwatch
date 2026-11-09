package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
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
	mu         sync.Mutex // shops fetch concurrently
	bodies     map[string][]byte
	errs       map[string]error
	disallowed map[string]bool
	gets       []string
}

func (f *fakeFetcher) Get(_ context.Context, u string) (*fetch.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
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

// PostForm serves bodies keyed "POST <url> <encoded form>".
func (f *fakeFetcher) PostForm(ctx context.Context, u string, form url.Values) (*fetch.Response, error) {
	return f.Get(ctx, "POST "+u+" "+form.Encode())
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
			xspaceURL + ".js":     readFile(t, "xspace/fujinon-xf10-24mmf4-r-ois-wr-ii.json"),
			vjUsedURL:             readFile(t, "vjshop/do-cu-chat.html"),
			vjFujiURL:             readFile(t, "vjshop/may-anh-fujifilm.html"),
			vjFujiURL + "?page=2": vjPage(2, 3),
			vjFujiURL + "?page=3": vjPage(3, 3),
			vjXT5API:              readFile(t, "vjshop/fujifilm-x-t5-silver-chinh-hang.json"),
		},
		errs:       map[string]error{brokenURL: &fetch.StatusError{URL: brokenURL, Code: 503}},
		disallowed: map[string]bool{blockedURL: true},
	}
}

// vjPage builds a vjshop listing page (page cur of last) holding a product
// per title, each priced 10,000,000 VND at /<slug>, the slug being the title.
func vjPage(cur, last int, titles ...string) []byte {
	products := make([]string, 0, len(titles))
	for _, t := range titles {
		products = append(products, fmt.Sprintf(`{"core":{"name":%q,"slug":%q},"real_price":10000000}`, t, t))
	}
	return fmt.Appendf(nil, `<html><script id="__NEXT_DATA__" type="application/json">`+
		`{"props":{"pageProps":{"products":[%s],"meta":{"current_page":%d,"last_page":%d,"total":%d}}}}</script></html>`,
		strings.Join(products, ","), cur, last, len(titles))
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

// One full cycle over real snapshots (docs/architecture.md, Pipeline).
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

	// Events: 2 new X-T5, 3 new X-T50 and 3 new X-E5 body listings (the kits on that page
	// don't match the body-only products), the product-mode source's product
	// (all baseline), 2 broken sources.
	if n := countType(rep.Events, diff.NewListing); n != 9 {
		t.Errorf("new_listing = %d, want 9", n)
	}
	if n := countType(rep.Events, diff.AdapterBroken); n != 2 {
		t.Errorf("adapter_broken = %d, want 2", n)
	}
	if len(rep.Events) != 11 {
		t.Errorf("events = %d, want 11", len(rep.Events))
	}

	// Unmatched: every non-watched offer from both listings (43 offers
	// minus 2 X-T5, 3 X-T50 and 3 X-E5 bodies; the kits stay unmatched).
	if len(rep.Unmatched) != 34 {
		t.Errorf("unmatched = %d, want 34", len(rep.Unmatched))
	}

	// History: one record per matched offer.
	if len(hist["xf10-24-ii"]) != 1 || len(hist["x-t5"]) != 2 || len(hist["x-e5"]) != 3 {
		t.Errorf("history = xf10-24-ii:%d x-t5:%d x-e5:%d", len(hist["xf10-24-ii"]), len(hist["x-t5"]), len(hist["x-e5"]))
	}
	if r := hist["xf10-24-ii"][0]; r.PriceVND != 27_490_000 || !r.TS.Equal(runTime) || r.Condition != "new" || r.URL != xspaceURL {
		t.Errorf("xspace history record = %+v", r)
	}

	// State: the xspace product is keyed by its page URL, not the .js URL.
	if o := st.Offers[state.Key("xspace", xspaceURL)]; o == nil || o.Product != "xf10-24-ii" {
		t.Errorf("xspace state = %+v", o)
	}
	if len(st.Offers) != 9 {
		t.Errorf("state offers = %d, want 9", len(st.Offers))
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

// docs/architecture.md, Matching and normalisation: condition detection and source condition precedence.
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

func pagedConfig(t *testing.T, url string) *config.Config {
	t.Helper()
	cfg := testConfig(t)
	cfg.Sources.Sources = []config.Source{{Shop: "vjshop", Mode: config.ModeListing, Condition: config.ConditionAuto, URL: url}}
	return cfg
}

// A listing is followed page by page until the adapter reports the last
// one; an offer that shifts onto the next page while paging is counted once.
func TestRunFollowsPages(t *testing.T) {
	const u = "https://vjshop.vn/may-anh-ong-kinh-cu"
	f := &fakeFetcher{bodies: map[string][]byte{
		u:             vjPage(1, 3, "Fujifilm X-T5 Body", "Fujifilm X-E3 Body"),
		u + "?page=2": vjPage(2, 3, "Fujifilm X-E3 Body", "Fujifilm X-T4 Body"),
		u + "?page=3": vjPage(3, 3, "Fujifilm X-S20 Body"),
	}}
	rep, _, err := Run(t.Context(), pagedConfig(t, u), state.New(), f, Options{Now: func() time.Time { return runTime }})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{u, u + "?page=2", u + "?page=3"}; !slices.Equal(f.gets, want) {
		t.Errorf("fetched %v, want %v", f.gets, want)
	}
	if sr := rep.Sources[0]; sr.Err != "" || sr.Offers != 4 {
		t.Errorf("source = %+v, want 4 offers and no error", sr)
	}
}

// If a later page fails, the whole source fails: reporting only the pages
// that loaded would make the missing page's offers look gone.
func TestRunFailedPageFailsSource(t *testing.T) {
	const u = "https://vjshop.vn/may-anh-ong-kinh-cu"
	f := &fakeFetcher{bodies: map[string][]byte{
		u: vjPage(1, 2, "Fujifilm X-T5 Body"),
	}}
	rep, _, err := Run(t.Context(), pagedConfig(t, u), state.New(), f, Options{Now: func() time.Time { return runTime }})
	if err != nil {
		t.Fatal(err)
	}
	if sr := rep.Sources[0]; !strings.Contains(sr.Err, "page=2") || sr.Offers != 0 {
		t.Errorf("source = %+v, want page 2 error and no offers", sr)
	}
	if len(rep.Seen) != 0 {
		t.Errorf("seen %d offers, want 0", len(rep.Seen))
	}
}

// A used-gear listing whose last item sold is not reported as broken; one
// whose page can't be read still is.
func TestRunEmptyListing(t *testing.T) {
	const u = "https://vjshop.vn/may-anh-ong-kinh-cu-sigma"
	cfg := pagedConfig(t, u)
	st := state.New()
	opts := Options{Now: func() time.Time { return runTime }}
	run := func(body []byte) *Report {
		t.Helper()
		rep, _, err := Run(t.Context(), cfg, st, &fakeFetcher{bodies: map[string][]byte{u: body}}, opts)
		if err != nil {
			t.Fatal(err)
		}
		return rep
	}

	run(vjPage(1, 1, "Ong kinh Sigma 18-50mm f/2.8 DC DN (C) For Fujifilm X 2nd"))
	if rep := run(vjPage(1, 1)); countType(rep.Events, diff.AdapterBroken) != 0 {
		t.Errorf("sold-out listing: events = %v", rep.Events)
	}
	run(vjPage(1, 1, "Ong kinh Sigma 18-50mm f/2.8 DC DN (C) For Fujifilm X 2nd"))
	if rep := run([]byte(`<html><script id="__NEXT_DATA__">{"props":{"pageProps":{"products":[]}}}</script></html>`)); countType(rep.Events, diff.AdapterBroken) != 1 {
		t.Errorf("listing without a total: events = %v, want adapter_broken", rep.Events)
	}
}

// An unmatched offer found by two overlapping sources (a category and the
// promotions page) is reported once.
func TestRunUnmatchedOnce(t *testing.T) {
	const a, b = "https://vjshop.vn/ong-kinh-mirrorless-sigma", "https://vjshop.vn/khuyen-mai"
	cfg := pagedConfig(t, a)
	cfg.Sources.Sources = append(cfg.Sources.Sources, config.Source{Shop: "vjshop", Mode: config.ModeListing, Condition: config.ConditionAuto, URL: b})
	page := vjPage(1, 1, "Sigma 56mm F1.4 DC DN for Sony E")
	f := &fakeFetcher{bodies: map[string][]byte{a: page, b: page}}
	rep, _, err := Run(t.Context(), cfg, state.New(), f, Options{Now: func() time.Time { return runTime }})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Unmatched) != 1 {
		t.Errorf("unmatched = %+v, want one", rep.Unmatched)
	}
}

// A category card without a mount is not reported as unmatched when its page
// is a source of its own: that source reports the page's units.
func TestRunUnmatchedCardOfWatchedPage(t *testing.T) {
	const category = "https://vjshop.vn/ong-kinh-mirrorless-sigma"
	const card = "Sigma 10-18mm f/2.8 DC DN"
	cfg := pagedConfig(t, category)
	f := &fakeFetcher{bodies: map[string][]byte{category: vjPage(1, 1, card)}}
	now := Options{Now: func() time.Time { return runTime }}
	rep, _, err := Run(t.Context(), cfg, state.New(), f, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Unmatched) != 1 || !slices.Equal(rep.Unmatched[0].NearMiss, []string{"sigma-10-18"}) {
		t.Fatalf("without the page as a source: unmatched = %+v, want one near miss", rep.Unmatched)
	}

	page := rep.Unmatched[0].URL
	cfg.Sources.Sources = append(cfg.Sources.Sources, config.Source{Shop: "vjshop", Mode: config.ModeListing, Condition: config.ConditionAuto, URL: page})
	f.bodies[page] = vjPage(1, 1, card+" for Fujifilm X")
	rep, _, err = Run(t.Context(), cfg, state.New(), f, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Unmatched) != 0 {
		t.Errorf("with the page as a source: unmatched = %+v, want none", rep.Unmatched)
	}
}

// kyma: the category page is followed by one POST for the remaining
// products; offers on both are counted once.
func TestRunFollowsPostPage(t *testing.T) {
	const u = "https://kyma.vn/may-anh-fujifilm"
	cfg := testConfig(t)
	cfg.Sources.Sources = []config.Source{{Shop: "kyma", Mode: config.ModeListing, Condition: config.ConditionAuto, URL: u}}
	page := readFile(t, "kyma/may-anh-fujifilm.html")
	first, err := adapters.Extract(adapters.ForShop("kyma"), u, page)
	if err != nil {
		t.Fatal(err)
	}
	more := url.Values{"categoryId": {"2123"}, "paging": {"0"}, "pageSize": {"300"}, "sortBy": {"3"}, "priceMin": {"590000"}, "priceMax": {"200500000"}}
	// The response repeats one card from the first page and adds one.
	resp := fmt.Sprintf(`{"list":%q,"paging":1,"remainingProducts":-404}`,
		`<li><div class="item__product"><a class="item__name" href="`+strings.TrimPrefix(first[0].URL, "https://kyma.vn")+`"><h3>`+first[0].Title+`</h3></a><div class="item__price"><span>1,000,000</span></div></div></li>`+
			`<li><div class="item__product"><a class="item__name" href="/may-anh-fujifilm-x-e5.html"><h3>Máy ảnh Fujifilm X-E5 (Body)</h3></a><div class="item__price"><span>33,990,000</span></div></div></li>`)
	f := &fakeFetcher{bodies: map[string][]byte{
		u: page,
		"POST https://kyma.vn/Product/ShowMoreNextProCate " + more.Encode(): []byte(resp),
	}}
	rep, _, err := Run(t.Context(), cfg, state.New(), f, Options{Now: func() time.Time { return runTime }})
	if err != nil {
		t.Fatal(err)
	}
	if sr := rep.Sources[0]; sr.Err != "" || sr.Offers != len(first)+1 {
		t.Errorf("source = %+v, want %d offers", sr, len(first)+1)
	}
	if !slices.ContainsFunc(rep.Seen, func(o diff.Observation) bool { return o.Product == "x-e5" }) {
		t.Error("X-E5 from the show-more response not matched")
	}
}

// A product page with variants: the matching variant is used (a kit never
// stands in for the body), preferring in stock, then the lowest price.
func TestRunProductVariants(t *testing.T) {
	const page = "https://fujifilm-xspace.vn/products/fujifilm-x-t5"
	cfg := testConfig(t)
	cfg.Sources.Sources = []config.Source{{Shop: "xspace", Mode: config.ModeProduct, Product: "x-t5", Condition: config.ConditionNew, URL: page}}
	js := `{"title":"Máy ảnh FUJIFILM X-T5","price":4549000000,"available":true,"variants":[` +
		`{"title":"Body / Đen / Máy ảnh không gương lật","price":4549000000,"available":false},` +
		`{"title":"Body / Bạc / Máy ảnh không gương lật","price":4549000000,"available":false},` +
		`{"title":"XF16-80mm Kit / Đen / Máy ảnh không gương lật","price":5689000000,"available":true},` +
		`{"title":"XF16-50mm Kit / Bạc / Máy ảnh không gương lật","price":5599000000,"available":true}]}`
	f := &fakeFetcher{bodies: map[string][]byte{page + ".js": []byte(js)}}
	rep, _, err := Run(t.Context(), cfg, state.New(), f, Options{Now: func() time.Time { return runTime }})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Seen) != 1 {
		t.Fatalf("seen = %+v", rep.Seen)
	}
	if o := rep.Seen[0]; o.PriceVND != 45_490_000 || o.InStock == nil || *o.InStock || o.URL != page {
		t.Errorf("observation = %+v, want the body at 45.490.000, out of stock", o)
	}
	if len(rep.Unmatched) != 0 {
		t.Errorf("unmatched = %+v", rep.Unmatched)
	}
}

// Shops run concurrently, but results stay in config order, and within a
// shop the first source still wins a URL that two sources find.
func TestRunConfigOrder(t *testing.T) {
	cfg := testConfig(t)
	const vj1, vj2, kyma = "https://vjshop.vn/a", "https://vjshop.vn/b", "https://kyma.vn/may-anh-fujifilm"
	cfg.Sources.Sources = []config.Source{
		{Shop: "vjshop", Mode: config.ModeListing, Condition: config.ConditionUsed, URL: vj1},
		{Shop: "kyma", Mode: config.ModeListing, Condition: config.ConditionAuto, URL: kyma},
		{Shop: "vjshop", Mode: config.ModeListing, Condition: config.ConditionNew, URL: vj2},
	}
	page := vjPage(1, 1, "Fujifilm X-T5 Body")
	f := &fakeFetcher{bodies: map[string][]byte{vj1: page, vj2: page, kyma: readFile(t, "kyma/may-anh-fujifilm.html")}}
	st := state.New()
	rep, _, err := Run(t.Context(), cfg, st, f, Options{Now: func() time.Time { return runTime }})
	if err != nil {
		t.Fatal(err)
	}
	urls := make([]string, 0, len(rep.Sources))
	for _, s := range rep.Sources {
		urls = append(urls, s.URL)
	}
	if !slices.Equal(urls, []string{vj1, kyma, vj2}) {
		t.Errorf("sources in order %v", urls)
	}
	var vjOffers []*state.Offer
	for _, o := range st.Offers {
		if o.Shop == "vjshop" {
			vjOffers = append(vjOffers, o)
		}
	}
	if len(vjOffers) != 1 || vjOffers[0].Condition != "used" || vjOffers[0].Source != state.Key("vjshop", vj1) {
		t.Errorf("vjshop offers = %+v, want one, from the first source (used)", vjOffers)
	}
}

// Product mode: a used variation never stands in for a new product, even
// when it is in stock and cheaper.
func TestPickVariantPrefersCondition(t *testing.T) {
	src := config.Source{Shop: "mayanhhoangto", Mode: config.ModeProduct, Product: "x-t5", Condition: config.ConditionNew}
	offers := []adapters.Offer{
		{Title: "X-T5 (Hàng khá đẹp)", PriceVND: 29_000_000, InStock: new(true), Condition: "used"},
		{Title: "X-T5 (Hàng mới chính hãng)", PriceVND: 36_500_000, InStock: new(false)},
		{Title: "X-T5 (Hàng mới nhập khẩu)", PriceVND: 34_500_000, InStock: new(false)},
	}
	got, ok := pickVariant(offers, func(string) bool { return true },
		func(o adapters.Offer) bool { return resolveCondition(o, src) == src.Condition })
	if !ok || got.PriceVND != 34_500_000 {
		t.Errorf("picked %+v, want the cheapest new variation (34.500.000)", got)
	}
}

// bncamera's flash sale: the homepage's slots are requested by POST; a
// watched product on offer is one flash_sale event (a kit is not), and
// nothing is recorded as an offer or in the price history.
func TestRunFlashSale(t *testing.T) {
	const home = "https://bncamera.com/"
	start := runTime.Add(24 * time.Hour).Truncate(time.Hour)
	page := fmt.Sprintf(`<html><body><section class="bnfs-section" data-bnfs-campaign="1" data-bnfs-active-tab="tab_all">
<button data-day-id="%s"></button>
<button class="bnfs-tab-btn--time" data-timeslot-id="slot_a" data-start="%d" data-end="%d"></button>
<button data-category="all" data-tab-id="tab_all"></button></section>
<script>var bnfsPublic = {"ajaxUrl":"x","nonce":"n0nce","serverTime":"%d"};</script></body></html>`,
		"day_auto_"+start.In(time.FixedZone("ICT", 7*3600)).Format("20060102"), start.Unix(), start.Add(2*time.Hour).Unix(), runTime.Unix())
	card := func(title, variant, sale, regular string) string {
		return `<article class="bnfs-product-card"><h3 class="bnfs-product-card__title"><a href="https://bncamera.com/san-pham/fujifilm-x-t5?attribute_pa_phien-ban=` + variant + `">` + title + `</a></h3>` +
			`<div class="bnfs-product-card__prices"><span class="bnfs-product-card__sale-price">` + sale + `</span><del>` + regular + `</del></div><div class="bnfs-product-card__sold">Đã bán 1/5 suất</div></article>`
	}
	slot, err := json.Marshal(map[string]any{"success": true, "data": map[string]any{
		"html":      card("Fujifilm X-T5", "body-only", "33.900.000 ₫", "38.490.000 ₫") + card("Fujifilm X-T5", "lens-xf-16-50mm", "45.000.000 ₫", "50.490.000 ₫"),
		"countdown": map[string]int64{"start": start.Unix(), "end": start.Add(2 * time.Hour).Unix()},
	}})
	if err != nil {
		t.Fatal(err)
	}
	form := url.Values{"action": {"bnfs_get_products"}, "nonce": {"n0nce"}, "campaign_id": {"1"}, "tab_id": {"tab_all"},
		"limit": {"0"}, "ui": {""}, "day_id": {"day_auto_" + start.In(time.FixedZone("ICT", 7*3600)).Format("20060102")}, "timeslot_id": {"slot_a"}}
	f := &fakeFetcher{bodies: map[string][]byte{
		home: []byte(page),
		"POST https://bncamera.com/wp-admin/admin-ajax.php " + form.Encode(): slot,
	}}
	cfg := testConfig(t)
	cfg.Sources.Sources = []config.Source{{Shop: "bncamera", Mode: config.ModeFlash, Condition: config.ConditionAuto, URL: home}}
	st := state.New()
	rep, hist, err := Run(t.Context(), cfg, st, f, Options{Now: func() time.Time { return runTime }})
	if err != nil {
		t.Fatal(err)
	}
	if sr := rep.Sources[0]; sr.Err != "" || sr.Offers != 2 || sr.Matched != 1 || sr.Pages != 2 {
		t.Fatalf("source = %+v", sr)
	}
	if len(rep.Events) != 1 {
		t.Fatalf("events = %+v", rep.Events)
	}
	e := rep.Events[0]
	if e.Type != diff.FlashSale || e.Product != "x-t5" || e.PriceVND != 33_900_000 || e.OldPrice != 38_490_000 ||
		!e.Start.Equal(start) || e.Quota != "1/5" || e.Baseline {
		t.Errorf("event = %+v", e)
	}
	if got := EventPrice(e); !strings.Contains(got, "33.900.000₫ (normally 38.490.000₫, -12%)") || !strings.Contains(got, "1/5 sold") {
		t.Errorf("EventPrice = %q", got)
	}
	if len(st.Offers) != 0 || len(hist) != 0 {
		t.Errorf("flash sale recorded: offers %v, history %v", st.Offers, hist)
	}
	if len(st.Flash) != 1 {
		t.Errorf("flash state = %v", st.Flash)
	}
}
