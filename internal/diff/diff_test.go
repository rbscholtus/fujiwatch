package diff

// Spec tests for docs/architecture.md, State, history and events.

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/rbscholtus/fujiwatch/internal/config"
	"github.com/rbscholtus/fujiwatch/internal/state"
)

var t0 = time.Date(2026, 11, 7, 23, 17, 0, 0, time.UTC)

const (
	shop     = "vjshop"
	listURL  = "https://vjshop.vn/do-cu-chat"
	itemURL  = "https://vjshop.vn/x-t3-cu-98"
	item2URL = "https://vjshop.vn/x-t4-cu"
)

func ob(url string, p int64, stock *bool) Observation {
	return Observation{Shop: shop, URL: url, Title: "Fujifilm X-T3 cũ", Product: "x-t3", Condition: "used", PriceVND: p, InStock: stock}
}

func listing(obs ...Observation) SourceRun {
	return SourceRun{Shop: shop, URL: listURL, Mode: config.ModeListing, OfferCount: max(len(obs), 1), Observations: obs}
}

func configured() map[string]bool {
	return map[string]bool{state.Key(shop, listURL): true}
}

func types(evs []Event) []Type {
	out := make([]Type, len(evs))
	for i, e := range evs {
		out[i] = e.Type
	}
	return out
}

func day(n int) time.Time { return t0.Add(time.Duration(n) * 24 * time.Hour) }

func TestNewListing(t *testing.T) {
	st := state.New()
	evs := Apply(st, []SourceRun{listing(ob(itemURL, 15_000_000, new(true)))}, configured(), t0, Options{})
	if !slices.Equal(types(evs), []Type{NewListing}) {
		t.Fatalf("events = %v, want [new_listing]", types(evs))
	}
	e := evs[0]
	if e.URL != itemURL || e.Product != "x-t3" || e.PriceVND != 15_000_000 || e.Condition != "used" {
		t.Errorf("event = %+v", e)
	}
	o := st.Offers[state.Key(shop, itemURL)]
	if o == nil || !o.FirstSeen.Equal(t0) || !o.LastSeen.Equal(t0) || o.Source != state.Key(shop, listURL) {
		t.Errorf("state offer = %+v", o)
	}

	// Seen again: no new_listing, last_seen moves, first_seen stays.
	evs = Apply(st, []SourceRun{listing(ob(itemURL, 15_000_000, new(true)))}, configured(), day(1), Options{})
	if len(evs) != 0 {
		t.Errorf("second run events = %v, want none", types(evs))
	}
	if o := st.Offers[state.Key(shop, itemURL)]; !o.FirstSeen.Equal(t0) || !o.LastSeen.Equal(day(1)) {
		t.Errorf("first/last seen = %v / %v", o.FirstSeen, o.LastSeen)
	}
}

// Product mode: the first observation of a product page is a new listing,
// baseline in its source's first run; seen again, it is quiet.
func TestProductModeFirstSeen(t *testing.T) {
	run := SourceRun{Shop: "xspace", URL: "https://x/p", Mode: config.ModeProduct, OfferCount: 1,
		Observations: []Observation{{Shop: "xspace", URL: "https://x/p", Product: "xf10-24-ii", PriceVND: 27_490_000}}}
	cfg := map[string]bool{state.Key("xspace", "https://x/p"): true}
	st := state.New()
	evs := Apply(st, []SourceRun{run}, cfg, t0, Options{})
	if len(evs) != 1 || evs[0].Type != NewListing || !evs[0].Baseline || evs[0].PriceVND != 27_490_000 {
		t.Errorf("first run: %+v, want one baseline new_listing", evs)
	}
	if evs := Apply(st, []SourceRun{run}, cfg, day(1), Options{}); len(evs) != 0 {
		t.Errorf("second run: %v, want none", types(evs))
	}
}

func TestPriceChanges(t *testing.T) {
	tests := []struct {
		name     string
		old, new int64
		opts     Options
		want     []Type
	}{
		{"drop", 15_000_000, 14_000_000, Options{}, []Type{PriceDrop}},
		{"rise", 15_000_000, 16_000_000, Options{}, []Type{PriceRise}},
		{"same", 15_000_000, 15_000_000, Options{}, nil},
		{"price hidden now (Liên hệ)", 15_000_000, 0, Options{}, nil},
		{"price shown again", 0, 15_000_000, Options{}, nil},
		// Threshold: ≥ 2% OR ≥ 300,000 VND.
		{"below both thresholds", 15_000_000, 14_800_000, Options{MinDropPct: 2, MinDropVND: 300_000}, nil},
		{"meets VND threshold", 15_000_000, 14_700_000, Options{MinDropPct: 2, MinDropVND: 300_000}, []Type{PriceDrop}},
		{"meets pct threshold only", 10_000_000, 9_800_000, Options{MinDropPct: 2, MinDropVND: 300_000}, []Type{PriceDrop}},
		{"pct threshold alone", 10_000_000, 9_850_000, Options{MinDropPct: 2}, nil},
		{"rises ignore threshold", 15_000_000, 15_100_000, Options{MinDropPct: 2, MinDropVND: 300_000}, []Type{PriceRise}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := state.New()
			Apply(st, []SourceRun{listing(ob(itemURL, tt.old, nil))}, configured(), t0, Options{})
			evs := Apply(st, []SourceRun{listing(ob(itemURL, tt.new, nil))}, configured(), day(1), tt.opts)
			if !slices.Equal(types(evs), tt.want) {
				t.Fatalf("events = %v, want %v", types(evs), tt.want)
			}
			if len(evs) == 1 && (evs[0].OldPrice != tt.old || evs[0].PriceVND != tt.new) {
				t.Errorf("event prices = %d → %d", evs[0].OldPrice, evs[0].PriceVND)
			}
		})
	}
}

// A hidden price must not erase the last known one: the next visible price
// is compared against it.
func TestHiddenPriceKeepsLastKnown(t *testing.T) {
	st := state.New()
	Apply(st, []SourceRun{listing(ob(itemURL, 15_000_000, nil))}, configured(), t0, Options{})
	Apply(st, []SourceRun{listing(ob(itemURL, 0, nil))}, configured(), day(1), Options{})
	if p := st.Offers[state.Key(shop, itemURL)].PriceVND; p != 15_000_000 {
		t.Fatalf("stored price = %d, want 15000000 kept", p)
	}
	evs := Apply(st, []SourceRun{listing(ob(itemURL, 14_000_000, nil))}, configured(), day(2), Options{})
	if !slices.Equal(types(evs), []Type{PriceDrop}) || evs[0].OldPrice != 15_000_000 {
		t.Errorf("events = %+v", evs)
	}
}

func TestStockChanges(t *testing.T) {
	tests := []struct {
		name     string
		old, new *bool
		want     []Type
	}{
		{"back in stock", new(false), new(true), []Type{BackInStock}},
		{"out of stock", new(true), new(false), []Type{OutOfStock}},
		{"unknown now", new(true), nil, nil},
		{"known now", nil, new(false), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := state.New()
			Apply(st, []SourceRun{listing(ob(itemURL, 15_000_000, tt.old))}, configured(), t0, Options{})
			evs := Apply(st, []SourceRun{listing(ob(itemURL, 15_000_000, tt.new))}, configured(), day(1), Options{})
			if !slices.Equal(types(evs), tt.want) {
				t.Errorf("events = %v, want %v", types(evs), tt.want)
			}
		})
	}
	// Price and stock change together: both events.
	st := state.New()
	Apply(st, []SourceRun{listing(ob(itemURL, 15_000_000, new(false)))}, configured(), t0, Options{})
	evs := Apply(st, []SourceRun{listing(ob(itemURL, 14_000_000, new(true)))}, configured(), day(1), Options{})
	if !slices.Equal(types(evs), []Type{PriceDrop, BackInStock}) {
		t.Errorf("combined events = %v", types(evs))
	}
}

// gone: not seen for 3 consecutive runs → event, removed from state.
func TestGoneAfterThreeRuns(t *testing.T) {
	st := state.New()
	Apply(st, []SourceRun{listing(ob(itemURL, 15_000_000, nil), ob(item2URL, 20_000_000, nil))}, configured(), t0, Options{})

	for i := 1; i <= 2; i++ {
		evs := Apply(st, []SourceRun{listing(ob(item2URL, 20_000_000, nil))}, configured(), day(i), Options{})
		if len(evs) != 0 {
			t.Fatalf("run %d: events = %v, want none yet", i, types(evs))
		}
		if m := st.Offers[state.Key(shop, itemURL)].Missed; m != i {
			t.Fatalf("run %d: missed = %d", i, m)
		}
	}
	evs := Apply(st, []SourceRun{listing(ob(item2URL, 20_000_000, nil))}, configured(), day(3), Options{})
	if !slices.Equal(types(evs), []Type{Gone}) || evs[0].URL != itemURL || evs[0].PriceVND != 15_000_000 {
		t.Fatalf("events = %+v, want gone for %s", evs, itemURL)
	}
	if _, ok := st.Offers[state.Key(shop, itemURL)]; ok {
		t.Error("gone offer still in state")
	}

	// Coming back later is a new listing again.
	evs = Apply(st, []SourceRun{listing(ob(itemURL, 14_000_000, nil), ob(item2URL, 20_000_000, nil))}, configured(), day(4), Options{})
	if !slices.Equal(types(evs), []Type{NewListing}) {
		t.Errorf("events = %v, want [new_listing]", types(evs))
	}
}

func TestMissedResetsWhenSeenAgain(t *testing.T) {
	st := state.New()
	both := listing(ob(itemURL, 15_000_000, nil), ob(item2URL, 20_000_000, nil))
	only2 := listing(ob(item2URL, 20_000_000, nil))
	Apply(st, []SourceRun{both}, configured(), t0, Options{})
	Apply(st, []SourceRun{only2}, configured(), day(1), Options{})
	Apply(st, []SourceRun{only2}, configured(), day(2), Options{})
	Apply(st, []SourceRun{both}, configured(), day(3), Options{})
	if m := st.Offers[state.Key(shop, itemURL)].Missed; m != 0 {
		t.Errorf("missed = %d after being seen again, want 0", m)
	}
}

// adapter_broken: an error (non-200, fetch failure, parse failure) always;
// zero offers only if the source previously returned ≥ 1.
func TestAdapterBroken(t *testing.T) {
	st := state.New()
	key := state.Key(shop, listURL)

	evs := Apply(st, []SourceRun{{Shop: shop, URL: listURL, Mode: config.ModeListing, OfferCount: 0}}, configured(), t0, Options{})
	if len(evs) != 0 {
		t.Fatalf("0 offers on first run: events = %v, want none", types(evs))
	}

	Apply(st, []SourceRun{listing(ob(itemURL, 15_000_000, nil))}, configured(), day(1), Options{})
	if s := st.Sources[key]; s.LastCount != 1 || !s.LastOK.Equal(day(1)) || s.Failures != 0 {
		t.Fatalf("source health = %+v", s)
	}

	evs = Apply(st, []SourceRun{{Shop: shop, URL: listURL, Mode: config.ModeListing, OfferCount: 0}}, configured(), day(2), Options{})
	if !slices.Equal(types(evs), []Type{AdapterBroken}) || evs[0].Detail == "" {
		t.Fatalf("0 offers after ≥1: events = %+v", evs)
	}

	evs = Apply(st, []SourceRun{{Shop: shop, URL: listURL, Mode: config.ModeListing, Err: errors.New("GET x: HTTP 503")}}, configured(), day(3), Options{})
	if !slices.Equal(types(evs), []Type{AdapterBroken}) || evs[0].Detail != "GET x: HTTP 503" {
		t.Fatalf("http error: events = %+v", evs)
	}
	if s := st.Sources[key]; s.Failures != 2 || s.LastCount != 1 || s.LastError == "" {
		t.Errorf("source health after failures = %+v", s)
	}

	// A broken source must not age its offers toward "gone".
	if m := st.Offers[state.Key(shop, itemURL)].Missed; m != 0 {
		t.Errorf("missed = %d while source broken, want 0", m)
	}

	// Recovery resets the failure count.
	Apply(st, []SourceRun{listing(ob(itemURL, 15_000_000, nil))}, configured(), day(4), Options{})
	if s := st.Sources[key]; s.Failures != 0 || s.LastError != "" {
		t.Errorf("source health after recovery = %+v", s)
	}
}

// Sources filtered out (--only) are untouched; sources removed from the
// config age out their offers and lose their health record.
func TestUnrunAndRemovedSources(t *testing.T) {
	st := state.New()
	Apply(st, []SourceRun{listing(ob(itemURL, 15_000_000, nil))}, configured(), t0, Options{})

	for i := 1; i <= 5; i++ {
		Apply(st, nil, configured(), day(i), Options{}) // configured but not run
	}
	if o := st.Offers[state.Key(shop, itemURL)]; o == nil || o.Missed != 0 {
		t.Fatalf("offer of a skipped source changed: %+v", o)
	}

	var evs []Event
	for i := 6; i <= 8; i++ {
		evs = Apply(st, nil, map[string]bool{}, day(i), Options{}) // source removed
	}
	if !slices.Equal(types(evs), []Type{Gone}) {
		t.Errorf("events = %v, want gone after source removal", types(evs))
	}
	if len(st.Sources) != 0 {
		t.Errorf("sources = %v, want health of removed source dropped", st.Sources)
	}
}

// The same listing on two pages (or twice on one) is one offer.
func TestDuplicateObservations(t *testing.T) {
	st := state.New()
	other := SourceRun{Shop: shop, URL: "https://vjshop.vn/may-anh-fujifilm", Mode: config.ModeListing, OfferCount: 1,
		Observations: []Observation{ob(itemURL, 15_000_000, nil)}}
	cfg := configured()
	cfg[other.Key()] = true
	evs := Apply(st, []SourceRun{listing(ob(itemURL, 15_000_000, nil), ob(itemURL, 15_000_000, nil)), other}, cfg, t0, Options{})
	if !slices.Equal(types(evs), []Type{NewListing}) || len(st.Offers) != 1 {
		t.Errorf("events = %v, offers = %d", types(evs), len(st.Offers))
	}
}

func TestOptionsDropCounts(t *testing.T) {
	if !(Options{}).dropCounts(100, 99) {
		t.Error("no threshold: every drop counts")
	}
	o := Options{MinDropPct: 2}
	if !o.dropCounts(10_000_000, 9_800_000) || o.dropCounts(10_000_000, 9_800_001) {
		t.Error("2% boundary wrong")
	}
	o = Options{MinDropVND: 300_000}
	if !o.dropCounts(10_000_000, 9_700_000) || o.dropCounts(10_000_000, 9_700_001) {
		t.Error("300k boundary wrong")
	}
}

// Offers first recorded in their source's first successful run are a
// baseline; later new offers, and changes to known offers, are not. A
// source added later starts its own baseline.
func TestBaseline(t *testing.T) {
	st := state.New()
	evs := Apply(st, []SourceRun{listing(ob(itemURL, 15_000_000, nil))}, configured(), t0, Options{})
	if len(evs) != 1 || evs[0].Type != NewListing || !evs[0].Baseline {
		t.Fatalf("first run: %+v, want one baseline new_listing", evs)
	}

	second := itemURL + "-2"
	evs = Apply(st, []SourceRun{listing(ob(itemURL, 14_000_000, nil), ob(second, 15_000_000, nil))}, configured(), day(1), Options{})
	if len(evs) != 2 || slices.ContainsFunc(evs, func(e Event) bool { return e.Baseline }) {
		t.Errorf("second run: %+v, want price_drop and new_listing, not baseline", evs)
	}

	other := SourceRun{Shop: shop, URL: listURL + "?page=other", Mode: config.ModeListing, OfferCount: 2,
		Observations: []Observation{ob(itemURL, 14_000_000, nil), ob(itemURL+"-3", 9_000_000, nil)}}
	cfgd := configured()
	cfgd[other.Key()] = true
	evs = Apply(st, []SourceRun{listing(ob(itemURL, 14_000_000, nil), ob(second, 15_000_000, nil)), other}, cfgd, day(2), Options{})
	if len(evs) != 1 || evs[0].URL != itemURL+"-3" || !evs[0].Baseline {
		t.Errorf("new source: %+v, want one baseline new_listing for its new offer", evs)
	}
}

// A listing that states it has no items (the last used lens sold) is
// healthy: no adapter_broken, and its offers age out toward "gone".
func TestEmptyListingIsNotBroken(t *testing.T) {
	st := state.New()
	Apply(st, []SourceRun{listing(ob(itemURL, 15_000_000, nil))}, configured(), t0, Options{})

	empty := SourceRun{Shop: shop, URL: listURL, Mode: config.ModeListing, Empty: true}
	evs := Apply(st, []SourceRun{empty}, configured(), day(1), Options{})
	if len(evs) != 0 {
		t.Fatalf("empty listing: events = %v, want none", types(evs))
	}
	if s := st.Sources[state.Key(shop, listURL)]; s.Failures != 0 || s.LastCount != 0 {
		t.Errorf("source health = %+v", s)
	}
	if m := st.Offers[state.Key(shop, itemURL)].Missed; m != 1 {
		t.Errorf("missed = %d, want 1", m)
	}
}
