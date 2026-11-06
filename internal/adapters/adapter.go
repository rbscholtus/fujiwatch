// Package adapters turns fetched shop pages into offers. Each shop has an
// adapter; shops without one use the generic extractor (JSON-LD, Open Graph,
// microdata).
package adapters

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/rbscholtus/fujiwatch/internal/match"
)

// Offer is one product as seen on a shop page.
type Offer struct {
	Shop      string `json:"shop"`
	URL       string `json:"url"`
	Title     string `json:"title"`
	PriceVND  int64  `json:"price_vnd"`           // 0 = price not shown ("Liên hệ")
	InStock   *bool  `json:"in_stock,omitempty"`  // nil = unknown
	Condition string `json:"condition,omitempty"` // "new" | "used" | "" (unknown; the source decides)
}

// Adapter extracts offers from one shop's pages.
type Adapter interface {
	Name() string
	// Extract parses a fetched page. pageURL is the URL from sources.yaml
	// (not the rewritten fetch URL); raw is the response body and doc the
	// same body parsed as HTML.
	Extract(pageURL string, doc *goquery.Document, raw []byte) ([]Offer, error)
}

// URLRewriter is implemented by adapters that fetch something other than the
// configured page, e.g. a JSON endpoint behind a product page.
type URLRewriter interface {
	FetchURL(pageURL, mode string) (string, error)
}

// registry holds the shop-specific adapters, keyed by shop name.
var registry = map[string]Adapter{
	"xspace": NewHaravan("xspace"),
	"vjshop": NewVJShop(),
}

// ForShop returns the adapter for shop, or a generic one if the shop has no
// dedicated adapter.
func ForShop(shop string) Adapter {
	if a, ok := registry[shop]; ok {
		return a
	}
	return Generic{Shop: shop}
}

// Shops lists the shops with a dedicated adapter.
func Shops() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// FetchURL returns the URL a adapter wants fetched for pageURL.
func FetchURL(a Adapter, pageURL, mode string) (string, error) {
	if r, ok := a.(URLRewriter); ok {
		return r.FetchURL(pageURL, mode)
	}
	return pageURL, nil
}

// Extract parses raw as HTML and runs the adapter on it.
func Extract(a Adapter, pageURL string, raw []byte) ([]Offer, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("%s: parse html: %w", a.Name(), err)
	}
	offers, err := a.Extract(pageURL, doc, raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", a.Name(), err)
	}
	return offers, nil
}

// stockFromText interprets Vietnamese stock labels.
func stockFromText(s string) *bool {
	n := match.Normalize(s)
	switch {
	case n == "":
		return nil
	case strings.Contains(n, "het hang"), strings.Contains(n, "ngung kinh doanh"),
		strings.Contains(n, "ngung ban"), strings.Contains(n, "tam het"), strings.Contains(n, "out of stock"):
		return new(false)
	case strings.Contains(n, "con hang"), strings.Contains(n, "co san"),
		strings.Contains(n, "san hang"), strings.Contains(n, "in stock"):
		return new(true)
	default:
		return nil
	}
}

func looksLikeJSON(raw []byte) bool {
	t := bytes.TrimSpace(raw)
	return len(t) > 0 && (t[0] == '{' || t[0] == '[')
}

// clockSetter is implemented by adapters whose output depends on the time.
type clockSetter interface {
	withNow(now func() time.Time) Adapter
}

// WithClock returns a copy of a that uses now as the current time, for
// adapters that care (e.g. vjshop sale windows). Others are returned as is.
func WithClock(a Adapter, now func() time.Time) Adapter {
	if c, ok := a.(clockSetter); ok {
		return c.withNow(now)
	}
	return a
}
