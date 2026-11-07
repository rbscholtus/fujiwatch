package adapters

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/rbscholtus/fujiwatch/internal/config"
)

// Haravan handles shops on the Haravan platform (fujifilm-xspace.vn).
//
// In product mode it fetches /products/<handle>.js, a JSON document with
// prices in VND×100 and a per-variant "available" flag. Given an HTML product
// page instead, it falls back to og:price and the ".checkProduct" stock label.
type Haravan struct {
	shop string
}

// NewHaravan returns a Haravan adapter for shop.
func NewHaravan(shop string) Haravan { return Haravan{shop: shop} }

// Name returns the shop name.
func (h Haravan) Name() string { return h.shop }

// FetchURL maps a product page to its .js JSON endpoint.
func (h Haravan) FetchURL(pageURL, mode string) (string, error) {
	if mode != config.ModeProduct {
		return pageURL, nil
	}
	u, err := url.Parse(pageURL)
	if err != nil {
		return "", fmt.Errorf("parse url: %w", err)
	}
	if !strings.HasPrefix(u.Path, "/products/") || strings.HasSuffix(u.Path, ".js") {
		return pageURL, nil
	}
	u.Path = strings.TrimSuffix(u.Path, "/") + ".js"
	u.RawQuery = ""
	return u.String(), nil
}

// haravanProduct is the subset of /products/<handle>.js we use.
type haravanProduct struct {
	Title     string  `json:"title"`
	Price     float64 `json:"price"` // VND × 100; min over variants
	Available bool    `json:"available"`
	Variants  []struct {
		Title     string  `json:"title"` // "Body / Đen / …", "XF16-50mm Kit / Bạc / …"
		Price     float64 `json:"price"`
		Available bool    `json:"available"`
	} `json:"variants"`
}

// Extract reads either the .js JSON or the HTML product page.
func (h Haravan) Extract(pageURL string, doc *goquery.Document, raw []byte) ([]Offer, error) {
	if looksLikeJSON(raw) {
		return h.extractJSON(pageURL, raw)
	}

	offers := ExtractGeneric(h.shop, pageURL, doc)
	if len(offers) == 0 {
		return nil, errors.New("no product data found")
	}
	if offers[0].InStock == nil {
		label := doc.Find(".checkProduct").First().Clone()
		label.Find("strong").Remove() // drop the "Tình trạng:" (status) caption
		offers[0].InStock = stockFromText(label.Text())
	}
	return offers[:1], nil
}

func (h Haravan) extractJSON(pageURL string, raw []byte) ([]Offer, error) {
	var p haravanProduct
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("decode product json: %w", err)
	}
	if p.Title == "" {
		return nil, errors.New("product json has no title")
	}

	// One offer per variant, named after it, so the matcher can tell a body
	// from a kit ("XF16-50mm Kit / Bạc / …"); in product mode the runner
	// picks the variant that matches the product.
	if len(p.Variants) <= 1 {
		price := p.Price
		if len(p.Variants) == 1 && p.Variants[0].Price > 0 {
			price = p.Variants[0].Price
		}
		return []Offer{{Shop: h.shop, URL: pageURL, Title: p.Title, PriceVND: parsePrice(price / 100), InStock: new(p.Available)}}, nil
	}
	offers := make([]Offer, 0, len(p.Variants))
	for _, v := range p.Variants {
		title := p.Title
		if t := strings.TrimSpace(v.Title); t != "" && t != "Default Title" {
			title += " (" + t + ")"
		}
		offers = append(offers, Offer{Shop: h.shop, URL: pageURL, Title: title, PriceVND: parsePrice(v.Price / 100), InStock: new(v.Available)})
	}
	return offers, nil
}
