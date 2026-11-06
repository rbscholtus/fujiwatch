package adapters

import (
	"errors"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/rbscholtus/fujiwatch/internal/match"
)

// Layout describes how offers sit on one kind of page, as CSS selectors
// (goquery/cascadia syntax, including :contains). In the per-card fields
// (Title, Link, Price, Stock) a comma-separated list is a priority order:
// the first alternative that matches wins, regardless of document order.
type Layout struct {
	Name   string // for errors and tests
	Marker string // page must contain this to use the layout ("" = any page)
	Card   string // one element per offer
	Title  string // within the card; text, else its title attribute
	Link   string // within the card; href. "" or not found: the page URL
	Price  string // within the card; first match's text (or content attribute)
	Stock  string // within the card; optional stock label
}

// CSS is an adapter for shops without structured data, driven by layouts.
// The first layout whose marker matches and that yields offers wins; if none
// does, the generic extractor gets a try.
type CSS struct {
	shop    string
	layouts []Layout
}

// NewCSS returns a selector-based adapter.
func NewCSS(shop string, layouts ...Layout) CSS { return CSS{shop: shop, layouts: layouts} }

// Name returns the shop name.
func (c CSS) Name() string { return c.shop }

// Extract applies the layouts.
func (c CSS) Extract(pageURL string, doc *goquery.Document, _ []byte) ([]Offer, error) {
	for _, l := range c.layouts {
		if offers := l.extract(c.shop, pageURL, doc); len(offers) > 0 {
			return offers, nil
		}
	}
	if offers := ExtractGeneric(c.shop, pageURL, doc); len(offers) > 0 {
		return offers, nil
	}
	return nil, errors.New("no layout matched the page")
}

func (l Layout) extract(shop, pageURL string, doc *goquery.Document) []Offer {
	if l.Marker != "" && doc.Find(l.Marker).Length() == 0 {
		return nil
	}
	cards := doc.Find(l.Card)
	var offers []Offer
	cards.Each(func(_ int, card *goquery.Selection) {
		title := selText(card, l.Title)
		if title == "" {
			return
		}
		o := Offer{Shop: shop, Title: title, URL: pageURL}
		if l.Link != "" {
			if href, ok := first(card, l.Link).Attr("href"); ok && strings.TrimSpace(href) != "" {
				o.URL = resolveURL(pageURL, strings.TrimSpace(href))
			}
		}
		if o.URL == pageURL && cards.Length() > 1 {
			// Several offers on one page without their own links (e.g.
			// used units listed on a product page): keep them apart.
			o.URL = pageURL + "#" + slugify(title)
		}
		if l.Price != "" {
			p := first(card, l.Price)
			v, ok := p.Attr("content")
			if !ok {
				v = p.Text()
			}
			o.PriceVND = parsePrice(v)
		}
		if l.Stock != "" {
			o.InStock = stockFromText(first(card, l.Stock).Text())
		}
		offers = append(offers, o)
	})
	return offers
}

// selText returns the trimmed text of the first match, or its title
// attribute; sel "" means the card itself.
func selText(card *goquery.Selection, sel string) string {
	s := card
	if sel != "" {
		s = first(card, sel)
	}
	if t := strings.Join(strings.Fields(s.Text()), " "); t != "" {
		return t
	}
	t, _ := s.Attr("title")
	return strings.TrimSpace(t)
}

var nonSlugRe = regexp.MustCompile(`[^a-z0-9]+`)

// slugify turns a title into a stable URL fragment:
// "Fujifilm X-T4 Body Black (85% - BH 03 Tháng) HN" →
// "fujifilm-x-t4-body-black-85-bh-03-thang-hn".
func slugify(title string) string {
	return strings.Trim(nonSlugRe.ReplaceAllString(match.Normalize(title), "-"), "-")
}

// first returns the first element matching the earliest alternative in a
// comma-separated selector list.
func first(card *goquery.Selection, sel string) *goquery.Selection {
	var last *goquery.Selection
	for alt := range strings.SplitSeq(sel, ",") {
		last = card.Find(strings.TrimSpace(alt)).First()
		if last.Length() > 0 {
			return last
		}
	}
	return last
}
