// Package sitedata turns data/history/*.jsonl and data/state.json into the
// static JSON the price-history site renders (README §11).
//
//	index.json     products, current offers and best prices
//	<product>.json one price series per shop and condition, one point per day
package sitedata

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/rbscholtus/fujiwatch/internal/config"
	"github.com/rbscholtus/fujiwatch/internal/state"
)

// saigon is Vietnam time: a run at 23:17 UTC belongs to the next local day.
var saigon = time.FixedZone("ICT", 7*60*60)

// Index is index.json.
type Index struct {
	Generated time.Time      `json:"generated"`
	Products  []ProductEntry `json:"products"`
}

// ProductEntry is one watched product in index.json.
type ProductEntry struct {
	ID     string           `json:"id"`
	Name   string           `json:"name"`
	Kind   string           `json:"kind"`
	Best   map[string]Offer `json:"best"`    // "new"/"used"; absent if none
	Offers []Offer          `json:"offers"`  // current offers, cheapest first
	Points int              `json:"points"`  // history points available
	Target map[string]int64 `json:"targets"` // "new"/"used" → target price; absent if none
}

// Offer is a current offer from state.json.
type Offer struct {
	Shop      string    `json:"shop"`
	URL       string    `json:"url"`
	Title     string    `json:"title"`
	Condition string    `json:"condition"`
	PriceVND  int64     `json:"price_vnd"`
	InStock   *bool     `json:"in_stock"`
	LastSeen  time.Time `json:"last_seen"`
	FirstSeen time.Time `json:"first_seen"`
}

// Product is <product-id>.json.
type Product struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Series []Series `json:"series"`
}

// Series is the daily lowest price of one shop for one condition.
type Series struct {
	Shop      string  `json:"shop"`
	Condition string  `json:"condition"`
	Points    []Point `json:"points"`
}

// Point is one day.
type Point struct {
	Date     string `json:"date"` // YYYY-MM-DD, Vietnam time
	PriceVND int64  `json:"price_vnd"`
	URL      string `json:"url"` // the listing that had this price
}

// Build reads history for every watched product and assembles the site data.
func Build(products []config.Product, st *state.State, historyDir string, now time.Time) (*Index, []Product, error) {
	idx := &Index{Generated: now.UTC().Truncate(time.Second), Products: []ProductEntry{}}
	out := make([]Product, 0, len(products))

	for _, p := range products {
		recs, err := state.ReadHistory(historyDir, p.ID)
		if err != nil {
			return nil, nil, err
		}
		prod := Product{ID: p.ID, Name: p.Name, Series: aggregate(recs)}
		out = append(out, prod)

		entry := ProductEntry{ID: p.ID, Name: p.Name, Kind: p.Kind, Best: map[string]Offer{}, Offers: currentOffers(st, p.ID), Target: map[string]int64{}}
		for _, c := range []string{config.ConditionNew, config.ConditionUsed} {
			if t := p.Target(c); t > 0 {
				entry.Target[c] = t
			}
		}
		for _, s := range prod.Series {
			entry.Points += len(s.Points)
		}
		for _, o := range entry.Offers {
			if _, ok := entry.Best[o.Condition]; ok || o.PriceVND <= 0 || (o.InStock != nil && !*o.InStock) {
				continue
			}
			entry.Best[o.Condition] = o // offers are sorted cheapest first
		}
		idx.Products = append(idx.Products, entry)
	}
	return idx, out, nil
}

type seriesKey struct{ shop, condition string }

func aggregate(recs []state.Record) []Series {
	days := map[seriesKey]map[string]Point{}
	for _, r := range recs {
		if r.PriceVND <= 0 {
			continue // price hidden that day
		}
		k := seriesKey{r.Shop, r.Condition}
		d := r.TS.In(saigon).Format(time.DateOnly)
		if days[k] == nil {
			days[k] = map[string]Point{}
		}
		if cur, ok := days[k][d]; !ok || r.PriceVND < cur.PriceVND {
			days[k][d] = Point{Date: d, PriceVND: r.PriceVND, URL: r.URL}
		}
	}

	series := make([]Series, 0, len(days))
	for k, byDay := range days {
		s := Series{Shop: k.shop, Condition: k.condition, Points: make([]Point, 0, len(byDay))}
		for _, p := range byDay {
			s.Points = append(s.Points, p)
		}
		slices.SortFunc(s.Points, func(a, b Point) int { return cmp.Compare(a.Date, b.Date) })
		series = append(series, s)
	}
	slices.SortFunc(series, func(a, b Series) int {
		return cmp.Or(cmp.Compare(a.Condition, b.Condition), cmp.Compare(a.Shop, b.Shop))
	})
	return series
}

// currentOffers lists the product's offers in state: priced ones cheapest
// first, then unpriced ones, ties broken by shop and URL.
func currentOffers(st *state.State, productID string) []Offer {
	offers := []Offer{}
	for _, o := range st.Offers {
		if o.Product != productID {
			continue
		}
		offers = append(offers, Offer{
			Shop: o.Shop, URL: o.URL, Title: o.Title, Condition: o.Condition,
			PriceVND: o.PriceVND, InStock: o.InStock, LastSeen: o.LastSeen, FirstSeen: o.FirstSeen,
		})
	}
	price := func(o Offer) int64 {
		if o.PriceVND <= 0 {
			return 1 << 62
		}
		return o.PriceVND
	}
	slices.SortFunc(offers, func(a, b Offer) int {
		return cmp.Or(cmp.Compare(price(a), price(b)), cmp.Compare(a.Shop, b.Shop), cmp.Compare(a.URL, b.URL))
	})
	return offers
}

// Write stores index.json and one <product-id>.json per product in dir.
func Write(dir string, idx *Index, products []Product) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	if err := writeJSON(filepath.Join(dir, "index.json"), idx); err != nil {
		return err
	}
	for _, p := range products {
		if err := writeJSON(filepath.Join(dir, p.ID+".json"), p); err != nil {
			return err
		}
	}
	return nil
}

func writeJSON(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	if err := os.WriteFile(path, append(b, '\n'), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
