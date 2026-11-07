package diff

// Spec tests for README milestone 8: per-product target price
// ("alert below X VND").

import (
	"slices"
	"testing"

	"github.com/rbscholtus/fujiwatch/internal/config"
	"github.com/rbscholtus/fujiwatch/internal/state"
)

func targets(newT, usedT int64) Options {
	return Options{Targets: func(product, condition string) int64 {
		if product != "x-t3" {
			return 0
		}
		return config.Product{TargetVND: newT, TargetUsedVND: usedT}.Target(condition)
	}}
}

func TestBelowTargetCrossing(t *testing.T) {
	opts := targets(0, 15_000_000) // used X-T3 at or below 15M
	st := state.New()

	// First sighting above target: only new_listing.
	evs := Apply(st, []SourceRun{listing(ob(itemURL, 16_000_000, nil))}, configured(), t0, opts)
	if !slices.Equal(types(evs), []Type{NewListing}) {
		t.Fatalf("run 1 = %v", types(evs))
	}

	// Drops to exactly the target: price_drop + below_target.
	evs = Apply(st, []SourceRun{listing(ob(itemURL, 15_000_000, nil))}, configured(), day(1), opts)
	if !slices.Equal(types(evs), []Type{PriceDrop, BelowTarget}) {
		t.Fatalf("run 2 = %v", types(evs))
	}
	if e := evs[1]; e.TargetVND != 15_000_000 || e.PriceVND != 15_000_000 || e.OldPrice != 16_000_000 {
		t.Errorf("below_target = %+v", e)
	}

	// Stays below (even dropping further): no repeat below_target.
	evs = Apply(st, []SourceRun{listing(ob(itemURL, 14_500_000, nil))}, configured(), day(2), opts)
	if !slices.Equal(types(evs), []Type{PriceDrop}) {
		t.Errorf("run 3 = %v, want only price_drop", types(evs))
	}

	// Price hidden, then shown again below target: alert again (we could
	// not know it stayed below).
	Apply(st, []SourceRun{listing(ob(itemURL, 0, nil))}, configured(), day(3), opts)
	// last known price is kept (14.5M), so this is not a crossing
	evs = Apply(st, []SourceRun{listing(ob(itemURL, 14_500_000, nil))}, configured(), day(4), opts)
	if len(evs) != 0 {
		t.Errorf("run 5 = %v, want none (last known price already below target)", types(evs))
	}

	// Rises above, then falls below again: a new crossing.
	Apply(st, []SourceRun{listing(ob(itemURL, 17_000_000, nil))}, configured(), day(5), opts)
	evs = Apply(st, []SourceRun{listing(ob(itemURL, 14_900_000, nil))}, configured(), day(6), opts)
	if !slices.Equal(types(evs), []Type{PriceDrop, BelowTarget}) {
		t.Errorf("run 7 = %v", types(evs))
	}
}

func TestBelowTargetFirstSighting(t *testing.T) {
	// A new used listing already below target: both events.
	evs := Apply(state.New(), []SourceRun{listing(ob(itemURL, 14_000_000, nil))}, configured(), t0, targets(0, 15_000_000))
	if !slices.Equal(types(evs), []Type{NewListing, BelowTarget}) {
		t.Errorf("events = %v", types(evs))
	}

	// Product mode first sighting below target: both events too.
	run := SourceRun{Shop: shop, URL: itemURL, Mode: config.ModeProduct, OfferCount: 1,
		Observations: []Observation{ob(itemURL, 14_000_000, nil)}}
	evs = Apply(state.New(), []SourceRun{run}, map[string]bool{run.Key(): true}, t0, targets(0, 15_000_000))
	if !slices.Equal(types(evs), []Type{NewListing, BelowTarget}) {
		t.Errorf("product mode events = %v", types(evs))
	}
}

func TestBelowTargetIgnores(t *testing.T) {
	tests := []struct {
		name string
		opts Options
		p    int64
	}{
		{"no targets configured", Options{}, 1_000_000},
		{"target for the other condition only", targets(20_000_000, 0), 14_000_000}, // offer is used
		{"hidden price", targets(0, 15_000_000), 0},
		{"above target", targets(0, 15_000_000), 15_000_001},
	}
	for _, tt := range tests {
		evs := Apply(state.New(), []SourceRun{listing(ob(itemURL, tt.p, nil))}, configured(), t0, tt.opts)
		if slices.Contains(types(evs), BelowTarget) {
			t.Errorf("%s: got below_target", tt.name)
		}
	}
}
