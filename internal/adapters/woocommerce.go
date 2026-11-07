package adapters

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/rbscholtus/fujiwatch/internal/config"
	"github.com/rbscholtus/fujiwatch/internal/match"
)

const storeAPIPath = "/wp-json/wc/store/v1/products"

// WooCommerce handles WooCommerce shops through the public Store API
// (docs/shops.md, WooCommerce), which returns clean JSON with prices and stock:
//
//   - product page  /…/<slug>/  → /wp-json/wc/store/v1/products?slug=<slug>
//   - search page   /?s=<term>  → …/products?search=<term>&per_page=100
//   - category page /…/<cat>/   → …/products?category=<cat>&per_page=100
//
// A Store API URL in sources.yaml is used as is. Given HTML instead (API
// disabled), it falls back to the standard WooCommerce product grid.
//
// Listings are paged: a full page (per_page products) is followed by
// &page=N+1. A page past the last one is an empty array, so a last page that
// happens to be full costs one extra request.
type WooCommerce struct {
	shop string
	html CSS
}

// NewWooCommerce returns a WooCommerce adapter for shop.
func NewWooCommerce(shop string) WooCommerce {
	return WooCommerce{shop: shop, html: NewCSS(shop, Layout{
		Name:  "woocommerce grid",
		Card:  "li.product, div.product.type-product",
		Title: ".woocommerce-loop-product__title, .product-title, h2, h3",
		Link:  "a.woocommerce-LoopProduct-link, a[href]",
		Price: ".price ins .amount, .price .amount",
		Stock: ".stock, .out-of-stock",
	})}
}

// Name returns the shop name.
func (w WooCommerce) Name() string { return w.shop }

// FetchURL maps shop pages to the Store API.
func (w WooCommerce) FetchURL(pageURL, mode string) (string, error) {
	u, err := url.Parse(pageURL)
	if err != nil {
		return "", fmt.Errorf("parse url: %w", err)
	}
	if strings.Contains(u.Path, "/wp-json/") {
		return pageURL, nil
	}
	api := url.URL{Scheme: u.Scheme, Host: u.Host, Path: storeAPIPath}
	q := url.Values{}
	slug, _ := lastPathSegment(pageURL)

	switch {
	case mode == config.ModeProduct:
		if slug == "" {
			return "", fmt.Errorf("no product slug in %q", pageURL)
		}
		q.Set("slug", slug)
	case u.Query().Get("s") != "":
		q.Set("search", u.Query().Get("s"))
		q.Set("per_page", "100")
	case slug != "":
		q.Set("category", slug)
		q.Set("per_page", "100")
	default:
		return pageURL, nil
	}
	api.RawQuery = q.Encode()
	return api.String(), nil
}

type wooProduct struct {
	ID        int    `json:"id"`
	Type      string `json:"type"`      // simple | variable | variation | …
	Variation string `json:"variation"` // variation records: "Phân loại: Hàng mới nhập khẩu"
	Name      string `json:"name"`
	Permalink string `json:"permalink"`
	IsInStock *bool  `json:"is_in_stock"`
	Backorder bool   `json:"is_on_backorder"`
	Prices    struct {
		Price        string `json:"price"`
		CurrencyCode string `json:"currency_code"`
		MinorUnit    int    `json:"currency_minor_unit"`
		PriceRange   *struct {
			Min string `json:"min_amount"`
			Max string `json:"max_amount"`
		} `json:"price_range"`
	} `json:"prices"`
}

// hasVariations reports whether p is a variable product whose variations
// differ in price. Its own price is the cheapest variation, which may be
// a used unit, a kit or an add-on ("Len fuji 23 2.8 mua cùng" at 9M on an
// X-E5 of 33M), so the variations are fetched and stand in for it.
func (p wooProduct) hasVariations() bool {
	r := p.Prices.PriceRange
	return p.Type == "variable" && r != nil && r.Min != r.Max
}

// Extract reads Store API JSON, or falls back to the HTML grid.
func (w WooCommerce) Extract(pageURL string, doc *goquery.Document, raw []byte) ([]Offer, error) {
	if !looksLikeJSON(raw) {
		return w.html.Extract(pageURL, doc, raw)
	}
	var products []wooProduct
	if err := json.Unmarshal(raw, &products); err != nil {
		// Store API errors are objects like {"code":"…","message":"…"}.
		var apiErr struct {
			Code string `json:"code"`
		}
		if json.Unmarshal(raw, &apiErr) == nil && apiErr.Code != "" {
			return nil, fmt.Errorf("store api error %q", apiErr.Code)
		}
		return nil, fmt.Errorf("decode store api json: %w", err)
	}

	offers := make([]Offer, 0, len(products))
	skipped := 0
	for _, p := range products {
		if p.Name == "" {
			continue
		}
		if p.hasVariations() {
			skipped++ // its variations follow as the next page
			continue
		}
		if p.Prices.CurrencyCode != "" && !strings.EqualFold(p.Prices.CurrencyCode, "VND") {
			continue
		}
		o := Offer{
			Shop:     w.shop,
			URL:      p.Permalink,
			Title:    strings.TrimSpace(html.UnescapeString(p.Name)),
			PriceVND: wooPrice(p.Prices.Price, p.Prices.MinorUnit),
		}
		if p.Type == "variation" {
			// A variation is its own offer: "X-E5 (Phân loại: Hàng mới
			// chính hãng)"; its permalink carries the attributes.
			if v := strings.TrimSpace(html.UnescapeString(p.Variation)); v != "" {
				o.Title += " (" + v + ")"
			}
			if usedVariation(p.Variation) || match.DetectCondition(o.Title, "") == match.ConditionUsed {
				o.Condition = match.ConditionUsed
			}
		}
		if o.URL == "" {
			o.URL = pageURL
		}
		if p.IsInStock != nil {
			// Backorder ("Cho phép đặt hàng trước") is orderable but not in stock.
			o.InStock = new(*p.IsInStock && !p.Backorder)
		}
		offers = append(offers, o)
	}
	if len(products) > skipped && len(offers) == 0 {
		return nil, errors.New("store api returned products without usable names or VND prices")
	}
	return offers, nil
}

// NextPage pages through the Store API. After a listing page with variable
// products, their variations are fetched (one request, ?type=variation&
// parent=<ids>), carrying the listing page to continue with in State; a
// full page (per_page items) is followed by &page=N+1, and a page past the
// last one is an empty array.
func (w WooCommerce) NextPage(cur Page, raw []byte) (Page, error) {
	// An API error object is not an array: no next page (Extract reports
	// the error).
	if !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("[")) {
		return Page{}, nil
	}
	var items []wooProduct
	if err := json.Unmarshal(raw, &items); err != nil {
		return Page{}, fmt.Errorf("decode store api json: %w", err)
	}
	u, err := url.Parse(cur.URL)
	if err != nil {
		return Page{}, fmt.Errorf("parse url: %w", err)
	}
	q := u.Query()

	if q.Get("type") == "variation" {
		// A page of variations: more of them, else back to the listing.
		if next := nextFull(u, q, len(items)); next != "" {
			return Page{URL: next, State: cur.State}, nil
		}
		return Page{URL: cur.State}, nil
	}

	following := nextFull(u, q, len(items)) // the listing's next page, or ""
	var parents []string
	for _, p := range items {
		if p.hasVariations() {
			parents = append(parents, strconv.Itoa(p.ID))
		}
	}
	if len(parents) == 0 {
		return Page{URL: following}, nil
	}
	vq := url.Values{"type": {"variation"}, "parent": {strings.Join(parents, ",")}, "per_page": {"100"}}
	v := url.URL{Scheme: u.Scheme, Host: u.Host, Path: u.Path, RawQuery: vq.Encode()}
	return Page{URL: v.String(), State: following}, nil
}

// usedVariation reports whether a variation label is a used grade. Besides
// the usual markers ("Cũ hoạt động tốt"), mayanhhoangto grades used stock
// "Hàng đẹp" (nice condition), "Hàng khá đẹp" or "Khá đẹp 90%", against
// "Hàng mới chính hãng" / "Hàng mới nhập khẩu" (new). "Đẹp" alone is too
// common in titles ("giá đẹp", nice price) for general condition detection,
// so it only counts in a variation label without "mới" (new).
func usedVariation(label string) bool {
	n := match.Normalize(html.UnescapeString(label))
	return variationDepRe.MatchString(n) && !variationNewRe.MatchString(n)
}

var (
	variationDepRe = regexp.MustCompile(`\bdep\b`)
	variationNewRe = regexp.MustCompile(`\bmoi\b`)
)

// nextFull returns the URL of the page after u if it held a full page of n
// items, else "". Without per_page (a product by slug) there is no next page.
func nextFull(u *url.URL, q url.Values, n int) string {
	perPage, _ := strconv.Atoi(q.Get("per_page"))
	if perPage <= 0 || n < perPage {
		return ""
	}
	page, _ := strconv.Atoi(q.Get("page"))
	q.Set("page", strconv.Itoa(max(page, 1)+1))
	next := *u
	next.RawQuery = q.Encode()
	return next.String()
}

// wooPrice converts a Store API price string in minor units.
func wooPrice(s string, minorUnit int) int64 {
	v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || v <= 0 {
		return 0
	}
	if minorUnit > 0 {
		v /= int64(math.Pow10(minorUnit))
	}
	return parsePrice(float64(v))
}
