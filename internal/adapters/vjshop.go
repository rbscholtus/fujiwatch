package adapters

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/rbscholtus/fujiwatch/internal/config"
	"github.com/rbscholtus/fujiwatch/internal/match"
)

// vjshopAPI serves product JSON; the vjshop.vn product pages themselves are
// rendered client-side and contain no product data.
const vjshopAPI = "https://api.vjshop.vn/api/products/"

// saigon is Vietnam time; vjshop's sale windows are local times. A fixed zone
// avoids depending on tzdata on the CI runner.
var saigon = time.FixedZone("ICT", 7*60*60)

// VJShop handles vjshop.vn (Next.js).
//
//   - Listing pages (categories, "Đồ cũ chất" used goods) embed the product
//     list in <script id="__NEXT_DATA__">, at props.pageProps.products.
//   - Product pages carry no data, so product mode fetches
//     api.vjshop.vn/api/products/<slug> instead.
type VJShop struct {
	// Now returns the current time, used to pick the active sale price.
	Now func() time.Time
}

// NewVJShop returns the vjshop adapter.
func NewVJShop() *VJShop { return &VJShop{Now: time.Now} }

// Name returns "vjshop".
func (*VJShop) Name() string { return "vjshop" }

// FetchURL maps product pages to the product API.
func (*VJShop) FetchURL(pageURL, mode string) (string, error) {
	if mode != config.ModeProduct {
		return pageURL, nil
	}
	slug, err := lastPathSegment(pageURL)
	if err != nil {
		return "", err
	}
	return vjshopAPI + url.PathEscape(slug), nil
}

type vjPrice struct {
	Price    *float64 `json:"price"`
	From     *string  `json:"from"`
	To       *string  `json:"to"`
	Priority int      `json:"priority"`
}

type vjProduct struct {
	Core struct {
		Name   string `json:"name"`
		Slug   string `json:"slug"`
		Status *struct {
			Raw string `json:"raw"`
		} `json:"status"`
		ProductCategory *struct {
			Slug string `json:"slug"`
		} `json:"product_category"`
		Condition json.RawMessage `json:"condition"`
	} `json:"core"`
	RealPrice *float64           `json:"real_price"`
	Prices    map[string]vjPrice `json:"prices"`
}

// Extract reads either the product API JSON or a listing page.
func (v *VJShop) Extract(pageURL string, doc *goquery.Document, raw []byte) ([]Offer, error) {
	if looksLikeJSON(raw) {
		var resp struct {
			Data *vjProduct `json:"data"`
		}
		if err := json.Unmarshal(raw, &resp); err != nil {
			return nil, fmt.Errorf("decode product json: %w", err)
		}
		if resp.Data == nil || resp.Data.Core.Name == "" {
			return nil, errors.New("product json has no data")
		}
		o := v.offer(pageURL, *resp.Data)
		o.URL = pageURL
		return []Offer{o}, nil
	}

	script := doc.Find(`script#__NEXT_DATA__`).First().Text()
	if script == "" {
		return nil, errors.New("__NEXT_DATA__ not found")
	}
	var next struct {
		Props struct {
			PageProps struct {
				Products []vjProduct `json:"products"`
			} `json:"pageProps"`
		} `json:"props"`
	}
	if err := json.Unmarshal([]byte(script), &next); err != nil {
		return nil, fmt.Errorf("decode __NEXT_DATA__: %w", err)
	}

	products := next.Props.PageProps.Products
	offers := make([]Offer, 0, len(products))
	for _, p := range products {
		if p.Core.Name == "" || p.Core.Slug == "" {
			continue
		}
		offers = append(offers, v.offer(pageURL, p))
	}
	return offers, nil
}

func (v *VJShop) offer(pageURL string, p vjProduct) Offer {
	o := Offer{
		Shop:     "vjshop",
		URL:      resolveURL(pageURL, "/"+p.Core.Slug),
		Title:    strings.TrimSpace(p.Core.Name),
		PriceVND: v.price(p),
	}
	if p.Core.Status != nil {
		o.InStock = stockFromText(p.Core.Status.Raw)
	}
	// Used items live in "*-cu" categories (may-anh-cu, ong-kinh-cu) and
	// carry a condition grade; new items have condition null.
	cond := strings.TrimSpace(string(p.Core.Condition))
	if (p.Core.ProductCategory != nil && strings.HasSuffix(p.Core.ProductCategory.Slug, "-cu")) ||
		(cond != "" && cond != "null") {
		o.Condition = match.ConditionUsed
	}
	return o
}

// price returns real_price when the listing provides it; otherwise the
// highest-priority price whose sale window contains now.
func (v *VJShop) price(p vjProduct) int64 {
	if p.RealPrice != nil && *p.RealPrice > 0 {
		return parsePrice(*p.RealPrice)
	}
	now := v.Now().In(saigon)
	best, bestPrio := 0.0, -1
	for _, pr := range p.Prices {
		if pr.Price == nil || *pr.Price <= 0 || pr.Priority <= bestPrio {
			continue
		}
		if pr.From != nil && now.Before(parseVJTime(*pr.From, time.Time{})) {
			continue
		}
		if pr.To != nil && now.After(parseVJTime(*pr.To, now.Add(time.Hour))) {
			continue
		}
		best, bestPrio = *pr.Price, pr.Priority
	}
	return parsePrice(best)
}

// parseVJTime parses "2026-11-30 23:00:59" (Vietnam time); fallback is
// returned for unparsable values so they don't exclude a price.
func parseVJTime(s string, fallback time.Time) time.Time {
	t, err := time.ParseInLocation(time.DateTime, s, saigon)
	if err != nil {
		return fallback
	}
	return t
}

func lastPathSegment(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse url: %w", err)
	}
	seg := u.Path[strings.LastIndex(strings.TrimSuffix(u.Path, "/"), "/")+1:]
	seg = strings.TrimSuffix(seg, "/")
	if seg == "" {
		return "", fmt.Errorf("no path segment in %q", rawURL)
	}
	return seg, nil
}

func (*VJShop) withNow(now func() time.Time) Adapter { return &VJShop{Now: now} }
