package adapters

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"math"
	"net/url"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/rbscholtus/fujiwatch/internal/config"
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
	Name      string `json:"name"`
	Permalink string `json:"permalink"`
	IsInStock *bool  `json:"is_in_stock"`
	Backorder bool   `json:"is_on_backorder"`
	Prices    struct {
		Price        string `json:"price"`
		CurrencyCode string `json:"currency_code"`
		MinorUnit    int    `json:"currency_minor_unit"`
	} `json:"prices"`
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
	for _, p := range products {
		if p.Name == "" {
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
		if o.URL == "" {
			o.URL = pageURL
		}
		if p.IsInStock != nil {
			// Backorder ("Cho phép đặt hàng trước") is orderable but not in stock.
			o.InStock = new(*p.IsInStock && !p.Backorder)
		}
		offers = append(offers, o)
	}
	if len(products) > 0 && len(offers) == 0 {
		return nil, errors.New("store api returned products without usable names or VND prices")
	}
	return offers, nil
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
