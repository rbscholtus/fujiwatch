package adapters

// Spec tests for milestone 6 (README §1 shops, §5 per-shop notes): the
// WooCommerce Store API shops and the selector-based shops.

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/rbscholtus/fujiwatch/internal/config"
	"github.com/rbscholtus/fujiwatch/internal/match"
)

// Every shop in README §1 has a dedicated adapter and at least one
// snapshot with a golden file, and every shop in sources.yaml is covered.
func TestAllShopsCovered(t *testing.T) {
	readmeShops := []string{"xspace", "vjshop", "kyma", "zshop", "mayanhhoangto", "bncamera", "mayanh24h", "x-camera", "giangduydat"}
	if got := Shops(); !slices.Equal(got, sorted(readmeShops)) {
		t.Errorf("Shops() = %v, want %v", got, sorted(readmeShops))
	}
	for _, shop := range readmeShops {
		golden, _ := filepath.Glob(filepath.Join(testdataDir, shop, "*.golden.json"))
		if len(golden) == 0 {
			t.Errorf("shop %s has no golden snapshot test", shop)
		}
	}

	cfg, err := config.Load("../../config/watchlist.yaml", "../../config/sources.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range cfg.Sources.Sources {
		if _, generic := ForShop(s.Shop).(Generic); generic {
			t.Errorf("sources.yaml shop %q has no dedicated adapter", s.Shop)
		}
	}
}

func sorted(s []string) []string {
	c := slices.Clone(s)
	slices.Sort(c)
	return c
}

// ── WooCommerce Store API ────────────────────────────────────────

func TestWooFetchURL(t *testing.T) {
	a := ForShop("bncamera")
	tests := []struct{ in, mode, want string }{
		{"https://bncamera.com/san-pham/tamron-16-50mm-f2-8-a-for-fujifilm-x", config.ModeProduct,
			"https://bncamera.com/wp-json/wc/store/v1/products?slug=tamron-16-50mm-f2-8-a-for-fujifilm-x"},
		{"https://bncamera.com/?s=fujifilm&post_type=product", config.ModeListing,
			"https://bncamera.com/wp-json/wc/store/v1/products?per_page=100&search=fujifilm"},
		{"https://mayanhhoangto.com/ong-kinh/ong-kinh-fuji/", config.ModeListing,
			"https://mayanhhoangto.com/wp-json/wc/store/v1/products?category=ong-kinh-fuji&per_page=100"},
		{"https://x-camera.vn/wp-json/wc/store/v1/products?search=x-t5", config.ModeListing,
			"https://x-camera.vn/wp-json/wc/store/v1/products?search=x-t5"},
		{"https://x-camera.vn/", config.ModeListing, "https://x-camera.vn/"},
	}
	for _, tt := range tests {
		got, err := FetchURL(a, tt.in, tt.mode)
		if err != nil || got != tt.want {
			t.Errorf("FetchURL(%q, %s) = %q, %v\n  want %q", tt.in, tt.mode, got, err, tt.want)
		}
	}
	if _, err := FetchURL(a, "https://bncamera.com/", config.ModeProduct); err == nil {
		t.Error("product URL without slug: expected error")
	}
}

func TestWooBackorderIsNotInStock(t *testing.T) {
	offers := loadSnapshot(t, "bncamera/tamron-16-50mm-f2-8-a-for-fujifilm-x.json")
	if len(offers) != 1 {
		t.Fatalf("got %d offers", len(offers))
	}
	o := offers[0]
	if o.Title != "Tamron 16-50mm F2.8 A For Fujifilm X" || o.PriceVND != 14_200_000 {
		t.Errorf("offer = %+v", o)
	}
	// The snapshot says is_in_stock=true but is_on_backorder=true
	// ("Cho phép đặt hàng trước").
	if o.InStock == nil || *o.InStock {
		t.Errorf("InStock = %v, want false for a backorder", fmtBool(o.InStock))
	}
}

func TestWooListingSnapshots(t *testing.T) {
	for _, rel := range []string{
		"bncamera/s-fujifilm-post_type-product.json",
		"x-camera/s-fujifilm-post_type-product.json",
		"mayanhhoangto/ong-kinh-fuji.json",
		"mayanhhoangto/mirrorless-fujifirm.json",
	} {
		t.Run(rel, func(t *testing.T) {
			offers := loadSnapshot(t, rel)
			if len(offers) < 10 {
				t.Fatalf("only %d offers", len(offers))
			}
			shop, _, _ := strings.Cut(rel, "/")
			for _, o := range offers {
				if strings.Contains(o.Title, "&#") || strings.Contains(o.Title, "&amp;") {
					t.Errorf("HTML entity left in title %q", o.Title)
				}
				if !strings.HasPrefix(o.URL, "https://") || !strings.Contains(o.URL, shop) || o.Shop != shop {
					t.Errorf("offer %q: shop %q url %q", o.Title, o.Shop, o.URL)
				}
				if o.InStock == nil {
					t.Errorf("offer %q: stock unknown, Store API always reports it", o.Title)
				}
			}
		})
	}
}

func TestWooUsedXT3AtBNCamera(t *testing.T) {
	m := repoMatcher(t)
	var used int
	for _, o := range loadSnapshot(t, "bncamera/s-fujifilm-post_type-product.json") {
		if r := m.Match(o.Title); r.ProductID == "x-t3" && match.DetectCondition(o.Title, o.URL) == match.ConditionUsed {
			used++
		}
	}
	if used == 0 {
		t.Error("expected used X-T3 listings in the bncamera snapshot")
	}
}

func TestWooSynthetic(t *testing.T) {
	a := ForShop("x-camera")
	tests := []struct {
		name  string
		body  string
		price int64
		stock *bool
		count int
	}{
		{"minor units", `[{"name":"X-T5","permalink":"https://x-camera.vn/shop/x-t5/","is_in_stock":true,"prices":{"price":"3699000000","currency_code":"VND","currency_minor_unit":2}}]`, 36_990_000, new(true), 1},
		{"entities", `[{"name":"Fujifilm X-T5 &#8211; Body &amp; Grip","permalink":"https://x/","is_in_stock":false,"prices":{"price":"36990000","currency_code":"VND","currency_minor_unit":0}}]`, 36_990_000, new(false), 1},
		{"no price", `[{"name":"X-T6","permalink":"https://x/","prices":{"price":"","currency_code":"VND"}}]`, 0, nil, 1},
		{"non-VND skipped", `[{"name":"X-T5","permalink":"https://x/","prices":{"price":"169900","currency_code":"USD","currency_minor_unit":2}},{"name":"X-T4","permalink":"https://y/","prices":{"price":"30000000","currency_code":"VND"}}]`, 30_000_000, nil, 1},
		{"empty", `[]`, 0, nil, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			offers, err := Extract(a, "https://x-camera.vn/?s=x", []byte(tt.body))
			if err != nil {
				t.Fatal(err)
			}
			if len(offers) != tt.count {
				t.Fatalf("got %d offers, want %d", len(offers), tt.count)
			}
			if tt.count == 0 {
				return
			}
			if o := offers[0]; o.PriceVND != tt.price || !sameBool(o.InStock, tt.stock) {
				t.Errorf("offer = %+v (stock %s)", o, fmtBool(o.InStock))
			}
		})
	}
	if offers, _ := Extract(a, "https://x/", []byte(`[{"name":"Fujifilm X-T5 &#8211; Body &amp; Grip","prices":{"price":"1","currency_code":"VND"}}]`)); offers[0].Title != "Fujifilm X-T5 – Body & Grip" {
		t.Errorf("title = %q", offers[0].Title)
	}
	if _, err := Extract(a, "https://x/", []byte(`{"code":"rest_no_route","message":"No route"}`)); err == nil || !strings.Contains(err.Error(), "rest_no_route") {
		t.Errorf("API error object: err = %v", err)
	}
}

// Store API disabled: the WooCommerce HTML grid is the fallback.
func TestWooHTMLFallback(t *testing.T) {
	page := `<html><body><ul class="products">
<li class="product"><a class="woocommerce-LoopProduct-link" href="/shop/fujifilm-x-t5/">
  <h2 class="woocommerce-loop-product__title">Fujifilm X-T5 Body</h2>
  <span class="price"><del><span class="amount">44.490.000 ₫</span></del> <ins><span class="amount">38.490.000 ₫</span></ins></span></a></li>
<li class="product outofstock"><a class="woocommerce-LoopProduct-link" href="/shop/fujifilm-x-t4/">
  <h2 class="woocommerce-loop-product__title">Fujifilm X-T4 Body</h2>
  <span class="price"><span class="amount">29.990.000 ₫</span></span></a><p class="stock out-of-stock">Hết hàng</p></li>
</ul></body></html>`
	offers, err := Extract(ForShop("x-camera"), "https://x-camera.vn/danh-muc/may-anh/", []byte(page))
	if err != nil {
		t.Fatal(err)
	}
	if len(offers) != 2 {
		t.Fatalf("got %d offers", len(offers))
	}
	if o := offers[0]; o.URL != "https://x-camera.vn/shop/fujifilm-x-t5/" || o.PriceVND != 38_490_000 || o.InStock != nil {
		t.Errorf("offer 0 = %+v (sale price expected)", o)
	}
	if o := offers[1]; o.PriceVND != 29_990_000 || !sameBool(o.InStock, new(false)) {
		t.Errorf("offer 1 = %+v", o)
	}
}

// ── kyma ─────────────────────────────────────────────────────────

func TestKymaCategory(t *testing.T) {
	offers := loadSnapshot(t, "kyma/may-anh-fujifilm.html")
	if len(offers) < 20 {
		t.Fatalf("only %d offers", len(offers))
	}
	i := slices.IndexFunc(offers, func(o Offer) bool { return o.URL == "https://kyma.vn/may-anh-fujifilm-x-t5.html" })
	if i < 0 {
		t.Fatal("X-T5 body (black) not found; relative links must resolve to absolute URLs")
	}
	if o := offers[i]; o.Title != "Máy ảnh Fujifilm X-T5 (Body, Đen)" || o.PriceVND != 37_900_000 {
		t.Errorf("X-T5 offer = %+v (want current price, not the struck-through one)", o)
	}
	for _, o := range offers {
		if o.PriceVND == 0 {
			t.Errorf("%q: no price", o.Title)
		}
	}
}

// ── zshop (CS-Cart) ──────────────────────────────────────────────

func TestZShop(t *testing.T) {
	offers := loadSnapshot(t, "zshop/may-anh-mirrorless-fujifilm.html")
	byURL := map[string]Offer{}
	for _, o := range offers {
		byURL[o.URL] = o
	}
	if o := byURL["https://zshop.vn/fujifilm-x-t30-iii.html"]; o.PriceVND != 25_490_000 {
		t.Errorf("X-T30 III = %+v (want discounted price 25.490.000)", o)
	}
	if o, ok := byURL["https://zshop.vn/fujifilm-x-t6.html"]; !ok || o.PriceVND != 0 {
		t.Errorf("upcoming X-T6 without price = %+v, want price 0", o)
	}

	product := loadSnapshot(t, "zshop/fujifilm-x-t5.html")
	if len(product) != 1 || product[0].PriceVND != 36_990_000 || product[0].URL != "https://zshop.vn/fujifilm-x-t5.html" {
		t.Errorf("product page = %+v", product)
	}
}

// ── mayanh24h ────────────────────────────────────────────────────

func TestMayAnh24h(t *testing.T) {
	offers := loadSnapshot(t, "mayanh24h/may-anh-fujifilm-cu.html")
	var in, out int
	for _, o := range offers {
		switch {
		case o.InStock == nil:
			t.Errorf("%q: stock label not read", o.Title)
		case *o.InStock:
			in++
		default:
			out++
		}
		if match.DetectCondition(o.Title, o.URL) != match.ConditionUsed {
			t.Errorf("%q on the used page not detected as used", o.Title)
		}
	}
	if in == 0 || out == 0 {
		t.Errorf("in stock %d, out of stock %d: expected both in the snapshot", in, out)
	}

	product := loadSnapshot(t, "mayanh24h/tamron-16-50mm-f-2-8-cho-fujifilm.html")
	if len(product) != 1 {
		t.Fatalf("product page: %d offers, want 1 (related products must be ignored)", len(product))
	}
	if o := product[0]; o.PriceVND != 13_480_000 || !sameBool(o.InStock, new(true)) ||
		o.Title != "Tamron 16-50mm F/2.8 A Cho Fujifilm, Mới 100% (Chính Hãng)" {
		t.Errorf("product = %+v", o)
	}
}

// ── giangduydat ──────────────────────────────────────────────────

func TestGiangDuyDatUnits(t *testing.T) {
	offers := loadSnapshot(t, "giangduydat/fujifilm-x-t4.html")
	if len(offers) != 3 {
		t.Fatalf("got %d units, want 3", len(offers))
	}
	urls := map[string]bool{}
	for _, o := range offers {
		urls[o.URL] = true
		if !strings.HasPrefix(o.URL, "https://giangduydat.vn/fujifilm-x-t4#fujifilm-x-t4-body-") {
			t.Errorf("unit URL %q", o.URL)
		}
		if match.DetectCondition(o.Title, o.URL) != match.ConditionUsed {
			t.Errorf("%q: graded unit not detected as used", o.Title)
		}
	}
	if len(urls) != 3 {
		t.Errorf("unit URLs not unique: %v", urls)
	}
	if offers[0].PriceVND != 24_800_000 {
		t.Errorf("first unit price = %d", offers[0].PriceVND)
	}
}

func TestGiangDuyDatCategory(t *testing.T) {
	offers := loadSnapshot(t, "giangduydat/ong-kinh-fujifilm.html")
	i := slices.IndexFunc(offers, func(o Offer) bool { return strings.Contains(o.Title, "XC 13-33mm") })
	if i < 0 || offers[i].PriceVND != 4_490_000 {
		t.Errorf(`"Từ 4.490.000 đ" not parsed: %+v`, offers)
	}
}

// ── CSS layouts ──────────────────────────────────────────────────

func TestCSSLayoutOrderAndFallback(t *testing.T) {
	a := NewCSS("s",
		Layout{Name: "product", Marker: "h1.p", Card: "body", Title: "h1.p", Price: ".now"},
		Layout{Name: "grid", Card: ".card", Title: "a", Link: "a", Price: ".now"},
	)
	grid := `<div class="card"><a href="/a">A</a><span class="now">1.000.000đ</span></div>
<div class="card"><a href="/b" title="B title"></a><span class="now">Liên hệ</span></div>`

	offers, err := Extract(a, "https://s.vn/c", []byte(`<html><body><h1 class="p">P</h1><span class="now">2.000.000đ</span>`+grid+`</body></html>`))
	if err != nil || len(offers) != 1 || offers[0].Title != "P" || offers[0].URL != "https://s.vn/c" {
		t.Errorf("product layout: %+v, %v (related cards must be ignored)", offers, err)
	}

	offers, err = Extract(a, "https://s.vn/c", []byte(`<html><body>`+grid+`</body></html>`))
	if err != nil || len(offers) != 2 || offers[1].Title != "B title" || offers[1].URL != "https://s.vn/b" || offers[1].PriceVND != 0 {
		t.Errorf("grid layout: %+v, %v", offers, err)
	}

	// Nothing matches and no structured data: an error, so the run reports
	// adapter_broken instead of silently returning nothing.
	if _, err := Extract(a, "https://s.vn/c", []byte(`<html><body>Cloudflare</body></html>`)); err == nil {
		t.Error("expected error when no layout matches")
	}
}

func TestSlugify(t *testing.T) {
	if got := slugify("Fujifilm X-T4 Body Black (85% - BH 03 Tháng) HN"); got != "fujifilm-x-t4-body-black-85-bh-03-thang-hn" {
		t.Errorf("slugify = %q", got)
	}
}

func TestSnapshotsAreSlim(t *testing.T) {
	// Store API snapshots are trimmed to the fields the adapter reads, so
	// the repository stays small.
	files, _ := filepath.Glob(filepath.Join(testdataDir, "*", "*.json"))
	for _, f := range files {
		if fi, err := os.Stat(f); err == nil && fi.Size() > 1<<20 {
			t.Errorf("%s is %d bytes; trim it", f, fi.Size())
		}
	}
}

func repoMatcher(t *testing.T) *match.Matcher {
	t.Helper()
	cfg, err := config.Load("../../config/watchlist.yaml", "../../config/sources.yaml")
	if err != nil {
		t.Fatal(err)
	}
	m, err := match.New(cfg.Watchlist.Products)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// Regression: real Store API responses embed full HTML descriptions; deep
// markup inside a JSON string must not reach the HTML parser (it fails with
// "open stack of elements exceeds 512 nodes").
func TestJSONWithDeepHTMLDescription(t *testing.T) {
	deep := strings.Repeat("<div>", 600) + "mô tả" + strings.Repeat("</div>", 600)
	body := `[{"name":"Fujifilm X-T5","permalink":"https://x-camera.vn/shop/x-t5/","is_in_stock":true,` +
		`"description":` + strconv.Quote(deep) + `,"prices":{"price":"38490000","currency_code":"VND","currency_minor_unit":0}}]`
	offers, err := Extract(ForShop("x-camera"), "https://x-camera.vn/?s=x", []byte(body))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(offers) != 1 || offers[0].PriceVND != 38_490_000 {
		t.Errorf("offers = %+v", offers)
	}
}
