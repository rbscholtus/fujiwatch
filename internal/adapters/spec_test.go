package adapters

// Tests of adapter behaviour against README §5, using real snapshots in
// testdata/ and small synthetic pages.

import (
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rbscholtus/fujiwatch/internal/config"
	"github.com/rbscholtus/fujiwatch/internal/match"
	"github.com/rbscholtus/fujiwatch/internal/snapshot"
)

const xspacePage = "https://fujifilm-xspace.vn/products/fujinon-xf10-24mmf4-r-ois-wr-ii"

func loadSnapshot(t *testing.T, rel string) []Offer {
	t.Helper()
	body := testdataDir + "/" + rel
	meta, err := snapshot.LoadMeta(body)
	if err != nil {
		t.Fatal(err)
	}
	return extractSnapshot(t, snapshotCase{name: rel, body: body, meta: meta})
}

func TestRegistry(t *testing.T) {
	if got := Shops(); !slices.Contains(got, "vjshop") || !slices.Contains(got, "xspace") || !slices.IsSorted(got) {
		t.Errorf("Shops() = %v", got)
	}
	if a := ForShop("unknown-shop"); a.Name() != "unknown-shop" {
		t.Errorf("fallback adapter name = %q", a.Name())
	}
	if _, ok := ForShop("unknown-shop").(Generic); !ok {
		t.Error("unknown shop should use the Generic adapter")
	}
}

// ── fujifilm-xspace (Haravan) ────────────────────────────────────

func TestXSpaceFetchURL(t *testing.T) {
	a := ForShop("xspace")
	tests := []struct{ in, mode, want string }{
		{xspacePage, config.ModeProduct, xspacePage + ".js"},
		{xspacePage + "?variant=1", config.ModeProduct, xspacePage + ".js"},
		{xspacePage + ".js", config.ModeProduct, xspacePage + ".js"},
		{"https://fujifilm-xspace.vn/collections/ong-kinh", config.ModeListing, "https://fujifilm-xspace.vn/collections/ong-kinh"},
		{"https://fujifilm-xspace.vn/collections/ong-kinh", config.ModeProduct, "https://fujifilm-xspace.vn/collections/ong-kinh"},
	}
	for _, tt := range tests {
		got, err := FetchURL(a, tt.in, tt.mode)
		if err != nil || got != tt.want {
			t.Errorf("FetchURL(%q, %s) = %q, %v; want %q", tt.in, tt.mode, got, err, tt.want)
		}
	}
}

// The .js JSON and the HTML page of the same product must agree, and the
// offer URL must be the page URL from sources.yaml, not the .js endpoint.
func TestXSpaceProduct(t *testing.T) {
	for _, rel := range []string{
		"xspace/fujinon-xf10-24mmf4-r-ois-wr-ii.json",
		"xspace/fujinon-xf10-24mmf4-r-ois-wr-ii-page.html",
	} {
		t.Run(rel, func(t *testing.T) {
			offers := loadSnapshot(t, rel)
			if len(offers) != 1 {
				t.Fatalf("got %d offers, want 1", len(offers))
			}
			o := offers[0]
			if o.Shop != "xspace" || o.URL != xspacePage {
				t.Errorf("shop/url = %q %q", o.Shop, o.URL)
			}
			if o.PriceVND != 27_490_000 {
				t.Errorf("PriceVND = %d, want 27490000", o.PriceVND)
			}
			if o.InStock == nil || !*o.InStock {
				t.Errorf("InStock = %v, want true", o.InStock)
			}
			if o.Title != "Ống kính Fujinon XF10-24mm F4 R OIS WR II" {
				t.Errorf("Title = %q", o.Title)
			}
		})
	}
}

func TestHaravanJSONVariantsAndStock(t *testing.T) {
	a := ForShop("xspace")
	tests := []struct {
		name    string
		json    string
		price   int64
		inStock bool
	}{
		{
			name:    "cheapest available variant",
			json:    `{"title":"X-T5","price":3699000000,"available":true,"variants":[{"price":3999000000,"available":true},{"price":3699000000,"available":false},{"price":3799000000,"available":true}]}`,
			price:   37_990_000,
			inStock: true,
		},
		{
			name:    "sold out uses product price",
			json:    `{"title":"X-T5","price":3699000000,"available":false,"variants":[{"price":3699000000,"available":false}]}`,
			price:   36_990_000,
			inStock: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			offers, err := Extract(a, xspacePage, []byte(tt.json))
			if err != nil {
				t.Fatal(err)
			}
			if offers[0].PriceVND != tt.price || *offers[0].InStock != tt.inStock {
				t.Errorf("got price %d stock %v, want %d %v", offers[0].PriceVND, *offers[0].InStock, tt.price, tt.inStock)
			}
		})
	}
	if _, err := Extract(a, xspacePage, []byte(`{"price":1}`)); err == nil {
		t.Error("expected error for JSON without a title")
	}
}

func TestXSpaceHTMLOutOfStock(t *testing.T) {
	page := `<html><head>
<meta property="og:title" content="Fujifilm X-T5" />
<meta property="og:price:amount" content="36990000" />
<meta property="og:price:currency" content="VND" />
</head><body><div class="checkProduct"><strong>Tình trạng: </strong>Hết hàng</div></body></html>`
	offers, err := Extract(ForShop("xspace"), xspacePage, []byte(page))
	if err != nil {
		t.Fatal(err)
	}
	if o := offers[0]; o.InStock == nil || *o.InStock || o.PriceVND != 36_990_000 {
		t.Errorf("offer = %+v, want out of stock at 36990000", o)
	}
}

// ── vjshop (Next.js) ─────────────────────────────────────────────

func TestVJShopFetchURL(t *testing.T) {
	a := ForShop("vjshop")
	got, err := FetchURL(a, "https://vjshop.vn/fujifilm-x-t5-silver-chinh-hang", config.ModeProduct)
	if err != nil || got != "https://api.vjshop.vn/api/products/fujifilm-x-t5-silver-chinh-hang" {
		t.Errorf("product FetchURL = %q, %v", got, err)
	}
	got, err = FetchURL(a, "https://vjshop.vn/do-cu-chat", config.ModeListing)
	if err != nil || got != "https://vjshop.vn/do-cu-chat" {
		t.Errorf("listing FetchURL = %q, %v", got, err)
	}
	if _, err := FetchURL(a, "https://vjshop.vn/", config.ModeProduct); err == nil {
		t.Error("expected error for product URL without a slug")
	}
}

// The used-goods section: every offer is marked used, has a price and a
// product URL on vjshop.vn.
func TestVJShopUsedListing(t *testing.T) {
	offers := loadSnapshot(t, "vjshop/do-cu-chat.html")
	if len(offers) == 0 {
		t.Fatal("no offers")
	}
	for _, o := range offers {
		if o.Condition != match.ConditionUsed {
			t.Errorf("%q: condition %q, want used", o.Title, o.Condition)
		}
		if o.PriceVND < 500_000 {
			t.Errorf("%q: price %d", o.Title, o.PriceVND)
		}
		u, err := url.Parse(o.URL)
		if err != nil || u.Host != "vjshop.vn" || len(u.Path) < 2 {
			t.Errorf("%q: bad url %q", o.Title, o.URL)
		}
	}
}

func TestVJShopNewListing(t *testing.T) {
	offers := loadSnapshot(t, "vjshop/may-anh-fujifilm.html")
	i := slices.IndexFunc(offers, func(o Offer) bool {
		return o.URL == "https://vjshop.vn/fujifilm-x-t5-silver-chinh-hang"
	})
	if i < 0 {
		t.Fatal("X-T5 silver not found in listing")
	}
	o := offers[i]
	if o.PriceVND != 36_990_000 || o.InStock == nil || !*o.InStock || o.Condition != "" {
		t.Errorf("X-T5 offer = %+v", o)
	}
	for _, o := range offers {
		if o.Condition == match.ConditionUsed {
			t.Errorf("new-goods category produced a used offer: %q", o.Title)
		}
	}
}

func TestVJShopNextPage(t *testing.T) {
	v := NewVJShop()
	for _, tc := range []struct{ file, url, want string }{
		{"vjshop/may-anh-fujifilm.html", "https://vjshop.vn/may-anh-fujifilm", "https://vjshop.vn/may-anh-fujifilm?page=2"},
		{"vjshop/may-anh-fujifilm.html", "https://vjshop.vn/may-anh-fujifilm?sort=latest", "https://vjshop.vn/may-anh-fujifilm?page=2&sort=latest"},
		{"vjshop/do-cu-chat.html", "https://vjshop.vn/do-cu-chat", ""},       // page 1 of 1
		{"vjshop/fujifilm-x-t5-silver-chinh-hang.json", vjshopAPI + "x", ""}, // product API
	} {
		raw, err := os.ReadFile("../../testdata/" + tc.file)
		if err != nil {
			t.Fatal(err)
		}
		got, err := v.NextPage(tc.url, raw)
		if err != nil || got != tc.want {
			t.Errorf("NextPage(%s, %s) = %q, %v; want %q", tc.url, tc.file, got, err, tc.want)
		}
	}
}

func TestVJShopListingTotal(t *testing.T) {
	v := NewVJShop()
	for _, tc := range []struct {
		body  string
		total int
		ok    bool
	}{
		{`<script id="__NEXT_DATA__">{"props":{"pageProps":{"products":[],"meta":{"current_page":1,"last_page":1,"total":0}}}}</script>`, 0, true},
		{`<script id="__NEXT_DATA__">{"props":{"pageProps":{"products":[],"meta":{"current_page":1,"last_page":1}}}}</script>`, 0, false},
		{`<html>maintenance</html>`, 0, false},
		{`{"data":{}}`, 0, false},
	} {
		total, ok := v.ListingTotal([]byte(tc.body))
		if total != tc.total || ok != tc.ok {
			t.Errorf("ListingTotal(%.40q) = %d, %v; want %d, %v", tc.body, total, ok, tc.total, tc.ok)
		}
	}
	raw, err := os.ReadFile("../../testdata/vjshop/may-anh-fujifilm.html")
	if err != nil {
		t.Fatal(err)
	}
	if total, ok := v.ListingTotal(raw); !ok || total < 36 {
		t.Errorf("may-anh-fujifilm total = %d, %v", total, ok)
	}
}

// The listing's real_price and the product API's active-price selection
// must agree for the same product at the same time.
func TestVJShopProductAPIMatchesListing(t *testing.T) {
	offers := loadSnapshot(t, "vjshop/fujifilm-x-t5-silver-chinh-hang.json")
	if len(offers) != 1 {
		t.Fatalf("got %d offers", len(offers))
	}
	if o := offers[0]; o.PriceVND != 36_990_000 || o.URL != "https://vjshop.vn/fujifilm-x-t5-silver-chinh-hang" {
		t.Errorf("offer = %+v", o)
	}
}

func TestVJShopPriceWindows(t *testing.T) {
	raw, err := os.ReadFile(testdataDir + "/vjshop/fujifilm-x-t5-silver-chinh-hang.json")
	if err != nil {
		t.Fatal(err)
	}
	// The snapshot has default 45.49M, sale 40.49M (no window) and a
	// flash sale of 36.99M from 2026-11-01 00:00 to 2026-11-30 23:00:59 ICT.
	tests := []struct {
		at   time.Time
		want int64
	}{
		{time.Date(2026, 10, 31, 16, 59, 0, 0, time.UTC), 40_490_000}, // 23:59 ICT, before
		{time.Date(2026, 10, 31, 17, 0, 0, 0, time.UTC), 36_990_000},  // 00:00 ICT Nov 1
		{time.Date(2026, 11, 30, 16, 0, 59, 0, time.UTC), 36_990_000}, // 23:00:59 ICT
		{time.Date(2026, 11, 30, 16, 1, 0, 0, time.UTC), 40_490_000},  // after
	}
	for _, tt := range tests {
		a := WithClock(ForShop("vjshop"), func() time.Time { return tt.at })
		offers, err := Extract(a, "https://vjshop.vn/fujifilm-x-t5-silver-chinh-hang", raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := offers[0].PriceVND; got != tt.want {
			t.Errorf("at %v: price %d, want %d", tt.at, got, tt.want)
		}
	}
}

func TestVJShopBrokenPage(t *testing.T) {
	a := ForShop("vjshop")
	if _, err := Extract(a, "https://vjshop.vn/do-cu-chat", []byte("<html><body>Cloudflare</body></html>")); err == nil {
		t.Error("expected an error when __NEXT_DATA__ is missing, so the run can report adapter_broken")
	}
	if _, err := Extract(a, "https://vjshop.vn/x", []byte(`{"data":null}`)); err == nil {
		t.Error("expected an error for empty API data")
	}
}

// ── generic extraction (README §5 order) ─────────────────────────

func TestGenericJSONLD(t *testing.T) {
	page := `<html><head>
<script type="application/ld+json">{"@context":"https://schema.org","@graph":[
  {"@type":"BreadcrumbList"},
  {"@type":"Product","name":"Fujifilm X-T4 Body","url":"/x-t4",
   "offers":{"@type":"Offer","price":"29990000","priceCurrency":"VND","availability":"https://schema.org/InStock"}}]}</script>
<script type="application/ld+json">{"@type":"ItemList","itemListElement":[
  {"@type":"ListItem","item":{"@type":"Product","name":"XF10-24mm WR","offers":[{"price":27490000,"availability":"http://schema.org/OutOfStock"}]}},
  {"@type":"ListItem","item":{"@type":["Product","Thing"],"name":"Tamron 16-50 X","offers":{"@type":"AggregateOffer","lowPrice":"18.990.000"}}}]}</script>
<script type="application/ld+json">{"@type":"Product","name":"Imported","offers":{"price":"999","priceCurrency":"USD"}}</script>
<script type="application/ld+json">{ broken json </script>
<meta property="og:price:amount" content="1" />
</head></html>`
	offers, err := Extract(Generic{Shop: "s"}, "https://shop.vn/cat/page", []byte(page))
	if err != nil {
		t.Fatal(err)
	}
	if len(offers) != 3 {
		t.Fatalf("got %d offers, want 3 (JSON-LD wins over og; USD skipped): %+v", len(offers), offers)
	}
	want := []struct {
		title, url string
		price      int64
		stock      *bool
	}{
		{"Fujifilm X-T4 Body", "https://shop.vn/x-t4", 29_990_000, new(true)},
		{"XF10-24mm WR", "https://shop.vn/cat/page", 27_490_000, new(false)},
		{"Tamron 16-50 X", "https://shop.vn/cat/page", 18_990_000, nil},
	}
	for i, w := range want {
		o := offers[i]
		if o.Title != w.title || o.URL != w.url || o.PriceVND != w.price || !sameBool(o.InStock, w.stock) {
			t.Errorf("offer %d = %+v, want %+v", i, o, w)
		}
	}
}

func TestGenericOpenGraph(t *testing.T) {
	page := `<html><head><title>Fallback title</title>
<meta property="og:price:amount" content="27.490.000" />
<meta property="og:price:currency" content="VND" />
<meta property="product:availability" content="out of stock" />
</head></html>`
	offers, _ := Extract(Generic{Shop: "s"}, "https://shop.vn/p", []byte(page))
	if len(offers) != 1 {
		t.Fatalf("got %d offers", len(offers))
	}
	o := offers[0]
	if o.Title != "Fallback title" || o.URL != "https://shop.vn/p" || o.PriceVND != 27_490_000 || !sameBool(o.InStock, new(false)) {
		t.Errorf("offer = %+v", o)
	}

	usd := strings.Replace(page, `content="VND"`, `content="USD"`, 1)
	if offers, _ := Extract(Generic{Shop: "s"}, "https://shop.vn/p", []byte(usd)); len(offers) != 0 {
		t.Errorf("non-VND og price should be ignored, got %+v", offers)
	}
}

func TestGenericMicrodata(t *testing.T) {
	page := `<html><body>
<div itemscope itemtype="https://schema.org/Product">
  <h1 itemprop="name">Fujifilm X-T3 cũ 98%</h1>
  <a itemprop="url" href="/san-pham/x-t3-cu"></a>
  <span itemprop="price" content="15500000">15.500.000₫</span>
  <link itemprop="availability" href="https://schema.org/InStock" />
</div>
<div itemscope itemtype="http://schema.org/Product">
  <span itemprop="name">Liên hệ item</span><span itemprop="price">Liên hệ</span>
</div></body></html>`
	offers, _ := Extract(Generic{Shop: "s"}, "https://shop.vn/used/", []byte(page))
	if len(offers) != 2 {
		t.Fatalf("got %d offers", len(offers))
	}
	if o := offers[0]; o.URL != "https://shop.vn/san-pham/x-t3-cu" || o.PriceVND != 15_500_000 || !sameBool(o.InStock, new(true)) {
		t.Errorf("offer 0 = %+v", o)
	}
	if o := offers[1]; o.PriceVND != 0 || o.InStock != nil {
		t.Errorf("'Liên hệ' offer = %+v, want price 0 and unknown stock", o)
	}
}

func TestGenericNothingFound(t *testing.T) {
	offers, err := Extract(Generic{Shop: "s"}, "https://shop.vn/", []byte("<html><body>hello</body></html>"))
	if err != nil || len(offers) != 0 {
		t.Errorf("got %v, %v; want no offers and no error", offers, err)
	}
}

func TestStockFromText(t *testing.T) {
	tests := []struct {
		in   string
		want *bool
	}{
		{"Còn hàng", new(true)},
		{"Hàng có sẵn", new(true)},
		{"Tình trạng: Hết hàng", new(false)},
		{"Tạm hết hàng", new(false)},
		{"Ngừng kinh doanh", new(false)},
		{"In stock", new(true)},
		{"Out of stock", new(false)},
		{"Liên hệ", nil},
		{"", nil},
	}
	for _, tt := range tests {
		if got := stockFromText(tt.in); !sameBool(got, tt.want) {
			t.Errorf("stockFromText(%q) = %v, want %v", tt.in, fmtBool(got), fmtBool(tt.want))
		}
	}
}

func TestStockFromSchema(t *testing.T) {
	tests := []struct {
		in   string
		want *bool
	}{
		{"https://schema.org/InStock", new(true)},
		{"http://schema.org/OutOfStock", new(false)},
		{"schema:SoldOut", new(false)},
		{"instock", new(true)},
		{"in stock", new(true)},
		{"oos", new(false)},
		{"PreOrder", nil},
		{"", nil},
	}
	for _, tt := range tests {
		if got := stockFromSchema(tt.in); !sameBool(got, tt.want) {
			t.Errorf("stockFromSchema(%q) = %v, want %v", tt.in, fmtBool(got), fmtBool(tt.want))
		}
	}
}

func sameBool(a, b *bool) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func fmtBool(b *bool) string {
	if b == nil {
		return "nil"
	}
	if *b {
		return "true"
	}
	return "false"
}
