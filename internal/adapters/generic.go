package adapters

import (
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/rbscholtus/fujiwatch/internal/price"
)

// Generic extracts offers using only standard page metadata. It is the
// fallback for shops without a dedicated adapter.
type Generic struct {
	Shop string
}

// Name returns the shop name.
func (g Generic) Name() string { return g.Shop }

// Extract runs ExtractGeneric.
func (g Generic) Extract(pageURL string, doc *goquery.Document, _ []byte) ([]Offer, error) {
	return ExtractGeneric(g.Shop, pageURL, doc), nil
}

// ExtractGeneric tries, in order, JSON-LD, Open Graph price tags and
// microdata, and returns the offers from the first one that yields any.
func ExtractGeneric(shop, pageURL string, doc *goquery.Document) []Offer {
	for _, extract := range []func(string, string, *goquery.Document) []Offer{
		extractJSONLD, extractOpenGraph, extractMicrodata,
	} {
		if offers := extract(shop, pageURL, doc); len(offers) > 0 {
			return offers
		}
	}
	return nil
}

// ── JSON-LD ──────────────────────────────────────────────────────

func extractJSONLD(shop, pageURL string, doc *goquery.Document) []Offer {
	var offers []Offer
	doc.Find(`script[type="application/ld+json"]`).Each(func(_ int, s *goquery.Selection) {
		var v any
		if err := json.Unmarshal([]byte(s.Text()), &v); err != nil {
			return
		}
		offers = append(offers, walkJSONLD(shop, pageURL, v, 0)...)
	})
	return offers
}

func walkJSONLD(shop, pageURL string, v any, depth int) []Offer {
	if depth > 6 {
		return nil
	}
	switch t := v.(type) {
	case []any:
		var out []Offer
		for _, e := range t {
			out = append(out, walkJSONLD(shop, pageURL, e, depth+1)...)
		}
		return out
	case map[string]any:
		if hasType(t, "Product") {
			if o, ok := jsonLDProduct(shop, pageURL, t); ok {
				return []Offer{o}
			}
			return nil
		}
		var out []Offer
		for _, key := range []string{"@graph", "itemListElement", "item", "mainEntity"} {
			if child, ok := t[key]; ok {
				out = append(out, walkJSONLD(shop, pageURL, child, depth+1)...)
			}
		}
		return out
	default:
		return nil
	}
}

func hasType(m map[string]any, want string) bool {
	switch t := m["@type"].(type) {
	case string:
		return t == want
	case []any:
		for _, e := range t {
			if s, ok := e.(string); ok && s == want {
				return true
			}
		}
	}
	return false
}

func jsonLDProduct(shop, pageURL string, m map[string]any) (Offer, bool) {
	o := Offer{
		Shop:  shop,
		Title: strings.TrimSpace(str(m["name"])),
		URL:   resolveURL(pageURL, str(m["url"])),
	}
	if o.Title == "" {
		return Offer{}, false
	}

	offer := m["offers"]
	if list, ok := offer.([]any); ok && len(list) > 0 {
		offer = list[0]
	}
	if om, ok := offer.(map[string]any); ok {
		if cur := str(om["priceCurrency"]); cur != "" && !strings.EqualFold(cur, "VND") {
			return Offer{}, false
		}
		p := om["price"]
		if p == nil {
			p = om["lowPrice"] // AggregateOffer
		}
		o.PriceVND = parsePrice(p)
		o.InStock = stockFromSchema(str(om["availability"]))
		if u := str(om["url"]); u != "" && str(m["url"]) == "" {
			o.URL = resolveURL(pageURL, u)
		}
	}
	return o, true
}

// ── Open Graph ───────────────────────────────────────────────────

func extractOpenGraph(shop, pageURL string, doc *goquery.Document) []Offer {
	amount := metaContent(doc, "og:price:amount", "product:price:amount")
	if amount == "" {
		return nil
	}
	if cur := metaContent(doc, "og:price:currency", "product:price:currency"); cur != "" && !strings.EqualFold(cur, "VND") {
		return nil
	}
	title := metaContent(doc, "og:title")
	if title == "" {
		title = strings.TrimSpace(doc.Find("title").First().Text())
	}
	return []Offer{{
		Shop:     shop,
		URL:      resolveURL(pageURL, metaContent(doc, "og:url")),
		Title:    title,
		PriceVND: parsePrice(amount),
		InStock:  stockFromSchema(metaContent(doc, "product:availability", "og:availability")),
	}}
}

func metaContent(doc *goquery.Document, names ...string) string {
	for _, n := range names {
		sel := doc.Find(`meta[property="` + n + `"], meta[name="` + n + `"]`).First()
		if v, ok := sel.Attr("content"); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// ── Microdata ────────────────────────────────────────────────────

func extractMicrodata(shop, pageURL string, doc *goquery.Document) []Offer {
	var offers []Offer
	doc.Find(`[itemtype$="schema.org/Product"]`).Each(func(_ int, s *goquery.Selection) {
		title := itemprop(s, "name")
		if title == "" {
			return
		}
		offers = append(offers, Offer{
			Shop:     shop,
			URL:      resolveURL(pageURL, itemprop(s, "url")),
			Title:    title,
			PriceVND: parsePrice(itemprop(s, "price")),
			InStock:  stockFromSchema(itemprop(s, "availability")),
		})
	})
	return offers
}

func itemprop(s *goquery.Selection, name string) string {
	el := s.Find(`[itemprop="` + name + `"]`).First()
	for _, attr := range []string{"content", "href"} {
		if v, ok := el.Attr(attr); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return strings.TrimSpace(el.Text())
}

// ── helpers ──────────────────────────────────────────────────────

// stockFromSchema reads schema.org / Open Graph availability values such as
// "https://schema.org/InStock", "instock", "out of stock" or "oos".
func stockFromSchema(s string) *bool {
	s = strings.ToLower(s)
	s = s[strings.LastIndexAny(s, "/:")+1:]
	s = strings.NewReplacer(" ", "", "_", "", "-", "").Replace(s)
	switch s {
	case "instock", "limitedavailability", "onlineonly", "instoreonly":
		return new(true)
	case "outofstock", "oos", "soldout", "discontinued":
		return new(false)
	default:
		return nil
	}
}

// parsePrice accepts a JSON number or a price string; invalid or implausible
// prices become 0 (unknown).
func parsePrice(v any) int64 {
	var s string
	switch t := v.(type) {
	case float64:
		s = strconv.FormatFloat(t, 'f', -1, 64)
	case string:
		s = t
	default:
		return 0
	}
	p, err := price.ParseVND(s)
	if err != nil {
		return 0
	}
	return p
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

// resolveURL resolves ref against base; an empty ref yields base.
func resolveURL(base, ref string) string {
	if ref == "" {
		return base
	}
	b, err := url.Parse(base)
	if err != nil {
		return ref
	}
	r, err := url.Parse(ref)
	if err != nil {
		return base
	}
	return b.ResolveReference(r).String()
}
