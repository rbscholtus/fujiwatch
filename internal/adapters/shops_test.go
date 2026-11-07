package adapters

// Spec tests for milestone 6 (README shops table, docs/shops.md): the
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

// Every shop in the README shops table has a dedicated adapter and at least one
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

func paginator(t *testing.T, shop string) Paginator {
	t.Helper()
	p, ok := ForShop(shop).(Paginator)
	if !ok {
		t.Fatalf("%s: adapter does not paginate", shop)
	}
	return p
}

func TestWooNextPage(t *testing.T) {
	a := paginator(t, "bncamera")
	full := "[" + strings.TrimSuffix(strings.Repeat(`{"name":"x"},`, 3), ",") + "]"
	for _, tc := range []struct{ url, body, want string }{
		{"https://b/wp-json/wc/store/v1/products?category=do-cu&per_page=3", full,
			"https://b/wp-json/wc/store/v1/products?category=do-cu&page=2&per_page=3"},
		{"https://b/wp-json/wc/store/v1/products?category=do-cu&page=2&per_page=3", full,
			"https://b/wp-json/wc/store/v1/products?category=do-cu&page=3&per_page=3"},
		{"https://b/wp-json/wc/store/v1/products?category=do-cu&per_page=3", `[{"name":"x"}]`, ""}, // last page
		{"https://b/wp-json/wc/store/v1/products?category=do-cu&per_page=3", `[]`, ""},
		{"https://b/wp-json/wc/store/v1/products?slug=x", `[{"name":"x"}]`, ""}, // product
		{"https://b/danh-muc/do-cu", "<html></html>", ""},                       // HTML fallback
	} {
		got, err := a.NextPage(Page{URL: tc.url}, []byte(tc.body))
		if err != nil || got.URL != tc.want || got.Form != nil {
			t.Errorf("NextPage(%s, %.20s) = %q, %v; want %q", tc.url, tc.body, got, err, tc.want)
		}
	}
}

// A variable product with a price range stands aside for its variations:
// the listing page is followed by one request for them (State: the listing
// page to continue with), each variation an offer named after its label.
func TestWooVariations(t *testing.T) {
	w := paginator(t, "mayanhhoangto")
	const list = "https://m.vn/wp-json/wc/store/v1/products?category=mirrorless-fujifirm&per_page=2"
	page := `[{"id":32479,"type":"variable","name":"Máy ảnh FUJIFILM X-E5 BODY","permalink":"https://m.vn/x-e5/","is_in_stock":true,` +
		`"prices":{"price":"9000000","currency_code":"VND","currency_minor_unit":0,"price_range":{"min_amount":"9000000","max_amount":"33000000"}}},` +
		`{"id":7,"type":"simple","name":"Máy ảnh FUJIFILM X-T3 Body cũ","permalink":"https://m.vn/x-t3/","is_in_stock":true,` +
		`"prices":{"price":"15000000","currency_code":"VND","currency_minor_unit":0}}]`
	offers, err := Extract(ForShop("mayanhhoangto"), list, []byte(page))
	if err != nil {
		t.Fatal(err)
	}
	if len(offers) != 1 || offers[0].URL != "https://m.vn/x-t3/" {
		t.Errorf("listing offers = %+v, want only the simple product", offers)
	}

	next, err := w.NextPage(Page{URL: list}, []byte(page))
	if err != nil {
		t.Fatal(err)
	}
	if next.URL != "https://m.vn/wp-json/wc/store/v1/products?parent=32479&per_page=100&type=variation" ||
		next.State != "https://m.vn/wp-json/wc/store/v1/products?category=mirrorless-fujifirm&page=2&per_page=2" {
		t.Fatalf("after the listing page: %+v", next)
	}

	vars := `[` +
		`{"id":1,"parent":32479,"type":"variation","name":"Máy ảnh FUJIFILM X-E5 BODY","variation":"Phân loại: Hàng mới chính hãng","permalink":"https://m.vn/x-e5/?attribute_pa_hinh-thuc=hang-moi-chinh-hang","is_in_stock":true,"prices":{"price":"33000000","currency_code":"VND","currency_minor_unit":0}},` +
		`{"id":2,"parent":32479,"type":"variation","name":"Máy ảnh FUJIFILM X-E5 BODY","variation":"Phân loại: Hàng khá đẹp","permalink":"https://m.vn/x-e5/?attribute_pa_hinh-thuc=hang-kha-dep","is_in_stock":true,"prices":{"price":"29000000","currency_code":"VND","currency_minor_unit":0}},` +
		`{"id":3,"parent":32479,"type":"variation","name":"Máy ảnh FUJIFILM X-E5 BODY","variation":"Phân loại: Cũ hoạt động tốt","permalink":"https://m.vn/x-e5/?attribute_pa_hinh-thuc=cu","is_in_stock":false,"prices":{"price":"25000000","currency_code":"VND","currency_minor_unit":0}}]`
	vo, err := Extract(ForShop("mayanhhoangto"), list, []byte(vars))
	if err != nil {
		t.Fatal(err)
	}
	if len(vo) != 3 ||
		vo[0].Title != "Máy ảnh FUJIFILM X-E5 BODY (Phân loại: Hàng mới chính hãng)" || vo[0].Condition != "" || vo[0].PriceVND != 33_000_000 ||
		vo[1].Condition != match.ConditionUsed || vo[2].Condition != match.ConditionUsed ||
		vo[0].URL == vo[1].URL {
		t.Errorf("variation offers = %+v", vo)
	}
	if after, err := w.NextPage(next, []byte(vars)); err != nil || after.URL != next.State || after.State != "" {
		t.Errorf("after the variations: %+v, %v; want the listing's page 2", after, err)
	}

	// The last listing page with variable products: variations, then the end.
	last, _ := w.NextPage(Page{URL: list}, []byte(`[`+page[1:strings.Index(page, `},{"id":7`)+1]+`]`))
	if last.State != "" {
		t.Errorf("last page: %+v, want variations without a page to continue with", last)
	}
	if end, _ := w.NextPage(last, []byte(vars)); !end.IsZero() {
		t.Errorf("after the last variations: %+v, want the end", end)
	}
}

func TestUsedVariation(t *testing.T) {
	for label, want := range map[string]bool{
		"Phân loại: Hàng đẹp":                  true,
		"Phân loại: Hàng khá đẹp":              true,
		"Phân loại: Khá đẹp 90% sử dụng tốt":   true,
		"Phân loại: Hàng mới chính hãng":       false,
		"Phân loại: Hàng mới nhập khẩu":        false,
		"Màu sắc: Silver, Biến thể: Body only": false,
		"Phân loại: Mới đẹp":                   false,
	} {
		if got := usedVariation(label); got != want {
			t.Errorf("usedVariation(%q) = %v, want %v", label, got, want)
		}
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

// CSS layouts with a Next selector: zshop (CS-Cart) links to /page-N/,
// giangduydat to <category>_pN, until the last page.
func TestCSSNextPage(t *testing.T) {
	z := paginator(t, "zshop")
	page := func(next string) []byte {
		link := ""
		if next != "" {
			link = `<a class="ty-pagination__item ty-pagination__btn ty-pagination__next cm-history cm-ajax" href="` + next + `">›</a>`
		}
		return []byte(`<html><body><div class="ty-grid-list__item"><a class="product-title" href="/x/">Fujifilm X-T5</a></div>` + link + `</body></html>`)
	}
	got, err := z.NextPage(Page{URL: "https://zshop.vn/lens-fujifilm/"}, page("https://zshop.vn/lens-fujifilm/page-2/"))
	if err != nil || got.URL != "https://zshop.vn/lens-fujifilm/page-2/" {
		t.Errorf("first page: next = %+v, %v", got, err)
	}
	if got, err := z.NextPage(Page{URL: "https://zshop.vn/lens-fujifilm/page-2/"}, page("")); err != nil || !got.IsZero() {
		t.Errorf("last page: next = %+v, %v", got, err)
	}
	// giangduydat: "tiếp" (next) is a link until the last page.
	g := paginator(t, "giangduydat")
	gpage := func(next string) []byte {
		tail := `<span id="no-page">tiếp</span>`
		if next != "" {
			tail = `<span class="page"><a href="` + next + `">tiếp <i></i></a></span>`
		}
		return []byte(`<html><body><ul><li class="idx-prod"><div class="name"><a href="/x">Fujifilm X-T5</a></div></li></ul><div id="page" class="c"><span class="page"><a href="https://giangduydat.vn/c/ong-kinh-fujifilm_p2">2</a></span>` + tail + `</div></body></html>`)
	}
	if got, err := g.NextPage(Page{URL: "https://giangduydat.vn/c/ong-kinh-fujifilm"}, gpage("https://giangduydat.vn/c/ong-kinh-fujifilm_p2")); err != nil || got.URL != "https://giangduydat.vn/c/ong-kinh-fujifilm_p2" {
		t.Errorf("giangduydat first page: next = %+v, %v", got, err)
	}
	if got, err := g.NextPage(Page{URL: "https://giangduydat.vn/c/ong-kinh-fujifilm_p3"}, gpage("")); err != nil || !got.IsZero() {
		t.Errorf("giangduydat last page: next = %+v, %v", got, err)
	}

	// Other CSS shops have no Next selector: one page.
	if got, _ := paginator(t, "mayanh24h").NextPage(Page{URL: "https://mayanh24h.com/x.html"}, page("https://mayanh24h.com/x.html?p=2")); !got.IsZero() {
		t.Errorf("mayanh24h: next = %+v", got)
	}
}

// ── kyma ─────────────────────────────────────────────────────────

// A category page with remaining products leads to one POST for all of
// them; the response's cards are extracted, and it ends the listing.
func TestKymaShowMore(t *testing.T) {
	k := NewKyma()
	raw, err := os.ReadFile(filepath.Join(testdataDir, "kyma", "may-anh-fujifilm.html"))
	if err != nil {
		t.Fatal(err)
	}
	next, err := k.NextPage(Page{URL: "https://kyma.vn/may-anh-fujifilm"}, raw)
	if err != nil {
		t.Fatal(err)
	}
	if next.URL != "https://kyma.vn/Product/ShowMoreNextProCate" ||
		next.Form.Encode() != "categoryId=2123&pageSize=300&paging=0&priceMax=200500000&priceMin=590000&sortBy=3" {
		t.Fatalf("next = %+v", next)
	}

	resp := `{"list":"<li><div class=\"item__product is--s4\"><a class=\"item__name\" href=\"/may-anh-fujifilm-x-t5.html\"><h3>Máy ảnh Fujifilm X-T5 (Body)</h3></a>` +
		`<div class=\"item__price\"><span>38,990,000 <i>đ</i></span><span class=\"is--sale\"><del>42,990,000</del></span></div></div></li>",` +
		`"paging":1,"remainingProducts":-404}`
	offers, err := Extract(k, "https://kyma.vn/may-anh-fujifilm", []byte(resp))
	if err != nil {
		t.Fatal(err)
	}
	if len(offers) != 1 || offers[0].URL != "https://kyma.vn/may-anh-fujifilm-x-t5.html" ||
		offers[0].Title != "Máy ảnh Fujifilm X-T5 (Body)" || offers[0].PriceVND != 38_990_000 {
		t.Errorf("offers = %+v", offers)
	}
	if after, err := k.NextPage(next, []byte(resp)); err != nil || !after.IsZero() {
		t.Errorf("after the show-more response: %+v, %v; want the end", after, err)
	}

	// A response that still reports remaining products is followed.
	partial := `{"list":"","paging":2,"remainingProducts":36}`
	if p, err := k.NextPage(next, []byte(partial)); err != nil || p.Form.Get("paging") != "2" || p.Form.Get("categoryId") != "2123" {
		t.Errorf("partial response: next = %+v, %v", p, err)
	}
	if next.Form.Get("paging") != "0" {
		t.Error("NextPage modified the current page's form")
	}
}

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
	// Category cards are discovery only: their price ("Từ 4.490.000 đ", or a
	// used unit's price under the plain name) is not recorded.
	if len(offers) < 30 {
		t.Fatalf("only %d offers", len(offers))
	}
	for _, o := range offers {
		if o.PriceVND != 0 || !strings.HasPrefix(o.URL, "https://giangduydat.vn/") {
			t.Errorf("category offer %+v, want a product URL and no price", o)
		}
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
