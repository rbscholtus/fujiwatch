// Package diff compares this run's offers with the saved state, updates the
// state and produces events.
package diff

import (
	"fmt"
	"sort"
	"time"

	"github.com/rbscholtus/fujiwatch/internal/config"
	"github.com/rbscholtus/fujiwatch/internal/state"
)

// Type is an event type.
type Type string

// Event types (README §7).
const (
	PriceDrop     Type = "price_drop"
	PriceRise     Type = "price_rise"
	BackInStock   Type = "back_in_stock"
	OutOfStock    Type = "out_of_stock"
	NewListing    Type = "new_listing"
	Gone          Type = "gone"
	AdapterBroken Type = "adapter_broken"
	BelowTarget   Type = "below_target" // price reached the product's target
)

// GoneAfter is how many consecutive successful runs of its source an offer
// may be missing before it is reported gone and dropped from state.
const GoneAfter = 3

// Event is one change worth reporting.
type Event struct {
	Type      Type   `json:"type"`
	Shop      string `json:"shop"`
	URL       string `json:"url"`
	Product   string `json:"product,omitempty"`
	Title     string `json:"title,omitempty"`
	Condition string `json:"condition,omitempty"`
	OldPrice  int64  `json:"old_price_vnd,omitempty"`
	PriceVND  int64  `json:"price_vnd,omitempty"`
	InStock   *bool  `json:"in_stock,omitempty"`
	Detail    string `json:"detail,omitempty"` // adapter_broken: what went wrong
	TargetVND int64  `json:"target_vnd,omitempty"`
}

// Observation is one matched offer seen in this run.
type Observation struct {
	Shop      string
	URL       string
	Title     string
	Product   string
	Condition string // "new" | "used", already resolved
	PriceVND  int64  // 0 = not shown
	InStock   *bool  // nil = unknown
}

// SourceRun is the outcome of fetching and extracting one source page.
type SourceRun struct {
	Shop       string
	URL        string
	Mode       string // config.ModeProduct | config.ModeListing
	Err        error  // fetch / HTTP / extraction failure
	OfferCount int    // offers extracted, before matching
	// Empty: the page itself states its listing has no items, so zero
	// offers is not an extraction failure.
	Empty        bool
	Observations []Observation
}

// Key is the source's state key.
func (r SourceRun) Key() string { return state.Key(r.Shop, r.URL) }

// Options tunes event generation.
type Options struct {
	// A price drop is reported if it is at least MinDropPct percent OR at
	// least MinDropVND. Both zero: every drop is reported.
	MinDropPct float64
	MinDropVND int64

	// Targets returns the target price for a product and condition
	// (0 = none). Nil: no targets.
	Targets func(product, condition string) int64
}

func (o Options) target(ob Observation) int64 {
	if o.Targets == nil {
		return 0
	}
	return o.Targets(ob.Product, ob.Condition)
}

func (o Options) dropCounts(oldP, newP int64) bool {
	if o.MinDropPct <= 0 && o.MinDropVND <= 0 {
		return true
	}
	d := oldP - newP
	return (o.MinDropVND > 0 && d >= o.MinDropVND) ||
		(o.MinDropPct > 0 && float64(d)*100 >= o.MinDropPct*float64(oldP))
}

// Apply updates st with this run's results and returns the events.
//
// configured holds the keys of every source in sources.yaml; offers whose
// source is no longer configured are aged out like missing ones. Sources that
// are configured but absent from runs (e.g. filtered by --only) are left
// untouched.
func Apply(st *state.State, runs []SourceRun, configured map[string]bool, now time.Time, opts Options) []Event {
	var events []Event
	healthy := map[string]bool{} // sources whose offers can be trusted this run
	seen := map[string]bool{}    // offer keys seen this run

	for _, run := range runs {
		src := st.Sources[run.Key()]
		if src == nil {
			src = &state.Source{}
			st.Sources[run.Key()] = src
		}

		switch {
		case run.Err != nil:
			src.Failures++
			src.LastError = run.Err.Error()
			events = append(events, broken(run, run.Err.Error()))
			continue
		case run.OfferCount == 0 && src.LastCount > 0 && !run.Empty:
			src.Failures++
			src.LastError = "no offers extracted"
			events = append(events, broken(run, fmt.Sprintf("no offers extracted (previous run: %d)", src.LastCount)))
			continue
		}
		src.Failures, src.LastError = 0, ""
		src.LastCount = run.OfferCount
		src.LastOK = now
		healthy[run.Key()] = true

		for _, ob := range run.Observations {
			key := state.Key(ob.Shop, ob.URL)
			if seen[key] {
				continue // same listing twice on a page, or on two source pages
			}
			seen[key] = true
			events = append(events, observe(st, run, key, ob, now, opts)...)
		}
	}

	events = append(events, ageOut(st, seen, healthy, configured)...)
	return events
}

func broken(run SourceRun, detail string) Event {
	return Event{Type: AdapterBroken, Shop: run.Shop, URL: run.URL, Detail: detail}
}

// observe records one observation and returns its events.
func observe(st *state.State, run SourceRun, key string, ob Observation, now time.Time, opts Options) []Event {
	old, ok := st.Offers[key]
	if !ok {
		st.Offers[key] = &state.Offer{
			Product:   ob.Product,
			Shop:      ob.Shop,
			URL:       ob.URL,
			Title:     ob.Title,
			PriceVND:  ob.PriceVND,
			InStock:   ob.InStock,
			Condition: ob.Condition,
			Source:    run.Key(),
			FirstSeen: now,
			LastSeen:  now,
		}
		var events []Event
		if run.Mode == config.ModeListing {
			events = append(events, event(NewListing, ob, 0))
		}
		if t := opts.target(ob); t > 0 && ob.PriceVND > 0 && ob.PriceVND <= t {
			e := event(BelowTarget, ob, 0)
			e.TargetVND = t
			events = append(events, e)
		}
		return events
	}

	var events []Event
	if old.PriceVND > 0 && ob.PriceVND > 0 && ob.PriceVND != old.PriceVND {
		switch {
		case ob.PriceVND < old.PriceVND && opts.dropCounts(old.PriceVND, ob.PriceVND):
			events = append(events, event(PriceDrop, ob, old.PriceVND))
		case ob.PriceVND > old.PriceVND:
			events = append(events, event(PriceRise, ob, old.PriceVND))
		}
	}
	// Target: fire once when the price crosses to or below it (or when the
	// last known price was hidden), not on every run it stays there.
	if t := opts.target(ob); t > 0 && ob.PriceVND > 0 && ob.PriceVND <= t && (old.PriceVND == 0 || old.PriceVND > t) {
		e := event(BelowTarget, ob, old.PriceVND)
		e.TargetVND = t
		events = append(events, e)
	}
	if old.InStock != nil && ob.InStock != nil && *old.InStock != *ob.InStock {
		if *ob.InStock {
			events = append(events, event(BackInStock, ob, old.PriceVND))
		} else {
			events = append(events, event(OutOfStock, ob, old.PriceVND))
		}
	}

	// Keep the last known price and stock when this run shows none
	// ("Liên hệ", or a page that hides stock).
	if ob.PriceVND > 0 {
		old.PriceVND = ob.PriceVND
	}
	if ob.InStock != nil {
		old.InStock = ob.InStock
	}
	old.Product = ob.Product
	old.Title = ob.Title
	old.Condition = ob.Condition
	old.Source = run.Key()
	old.LastSeen = now
	old.Missed = 0
	return events
}

func event(t Type, ob Observation, oldPrice int64) Event {
	return Event{
		Type:      t,
		Shop:      ob.Shop,
		URL:       ob.URL,
		Product:   ob.Product,
		Title:     ob.Title,
		Condition: ob.Condition,
		OldPrice:  oldPrice,
		PriceVND:  ob.PriceVND,
		InStock:   ob.InStock,
	}
}

// ageOut counts misses for offers not seen this run and removes those missing
// GoneAfter times. Only offers whose source ran healthily (or was removed from
// the config) are counted, so a broken shop never makes its items "gone".
func ageOut(st *state.State, seen, healthy, configured map[string]bool) []Event {
	keys := make([]string, 0, len(st.Offers))
	for k := range st.Offers {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var events []Event
	for _, k := range keys {
		o := st.Offers[k]
		if seen[k] || (!healthy[o.Source] && configured[o.Source]) {
			continue
		}
		o.Missed++
		if o.Missed < GoneAfter {
			continue
		}
		events = append(events, Event{
			Type:      Gone,
			Shop:      o.Shop,
			URL:       o.URL,
			Product:   o.Product,
			Title:     o.Title,
			Condition: o.Condition,
			PriceVND:  o.PriceVND,
		})
		delete(st.Offers, k)
	}

	// Forget the health of sources that are no longer configured.
	for k := range st.Sources {
		if !configured[k] {
			delete(st.Sources, k)
		}
	}
	return events
}
