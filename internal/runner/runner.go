// Package runner runs one watch cycle: fetch every source, extract offers,
// match them, diff against state and collect history records.
package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/rbscholtus/fujiwatch/internal/adapters"
	"github.com/rbscholtus/fujiwatch/internal/config"
	"github.com/rbscholtus/fujiwatch/internal/diff"
	"github.com/rbscholtus/fujiwatch/internal/fetch"
	"github.com/rbscholtus/fujiwatch/internal/match"
	"github.com/rbscholtus/fujiwatch/internal/state"
)

// ErrDisallowed is returned for URLs that robots.txt disallows.
var ErrDisallowed = errors.New("disallowed by robots.txt")

// Fetcher is the subset of *fetch.Client the runner needs.
type Fetcher interface {
	Get(ctx context.Context, rawURL string) (*fetch.Response, error)
	Allowed(ctx context.Context, rawURL string) (bool, error)
}

// Options configures a run.
type Options struct {
	Only string           // only run sources of this shop ("" = all)
	Now  func() time.Time // default time.Now
	Log  io.Writer        // progress output; nil = silent
}

// SourceReport summarises one source in this run.
type SourceReport struct {
	Shop    string `json:"shop"`
	Mode    string `json:"mode"`
	URL     string `json:"url"`
	Offers  int    `json:"offers"`  // extracted
	Matched int    `json:"matched"` // that matched a watched product
	Err     string `json:"error,omitempty"`
}

// Unmatched is an offer from a listing that matched zero or several
// products, or a product page whose title doesn't match its product.
type Unmatched struct {
	Shop       string   `json:"shop"`
	URL        string   `json:"url"`
	Title      string   `json:"title"`
	Status     string   `json:"status"` // unmatched | ambiguous | mismatch
	Candidates []string `json:"candidates,omitempty"`
	NearMiss   []string `json:"near_miss,omitempty"` // unmatched, but close to these products
}

// Report is the outcome of a run.
type Report struct {
	Time      time.Time          `json:"time"`
	Sources   []SourceReport     `json:"sources"`
	Events    []diff.Event       `json:"events"`
	Unmatched []Unmatched        `json:"unmatched"`
	Seen      []diff.Observation `json:"-"` // every matched offer this run
}

// Run executes one cycle. It updates st in place and returns the report and
// the history records to append, keyed by product ID. Failures of single
// sources are reported, never returned; only a cancelled ctx aborts the run.
func Run(ctx context.Context, cfg *config.Config, st *state.State, f Fetcher, opts Options) (*Report, map[string][]state.Record, error) {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	now := opts.Now().UTC().Truncate(time.Second)

	m, err := match.New(cfg.Watchlist.Products)
	if err != nil {
		return nil, nil, fmt.Errorf("compile watchlist: %w", err)
	}

	rep := &Report{Time: now}
	configured := map[string]bool{}
	unmatchedSeen := map[string]bool{} // shop|url: one offer found by overlapping sources
	var runs []diff.SourceRun

	for _, src := range cfg.Sources.Sources {
		configured[state.Key(src.Shop, src.URL)] = true
		if opts.Only != "" && src.Shop != opts.Only {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, nil, fmt.Errorf("run cancelled: %w", err)
		}
		logf(opts.Log, "%-14s %-7s %s\n", src.Shop, src.Mode, src.URL)

		run, unmatched := runSource(ctx, src, m, f, now)
		runs = append(runs, run)
		for _, u := range unmatched {
			if k := state.Key(u.Shop, u.URL); !unmatchedSeen[k] {
				unmatchedSeen[k] = true
				rep.Unmatched = append(rep.Unmatched, u)
			}
		}
		rep.Seen = append(rep.Seen, run.Observations...)

		sr := SourceReport{Shop: src.Shop, Mode: src.Mode, URL: src.URL, Offers: run.OfferCount, Matched: len(run.Observations)}
		if run.Err != nil {
			sr.Err = run.Err.Error()
			logf(opts.Log, "%-14s error: %v\n", "", run.Err)
		}
		rep.Sources = append(rep.Sources, sr)
	}

	rep.Events = diff.Apply(st, runs, configured, now, diff.Options{
		MinDropPct: cfg.Sources.Defaults.MinDropPct,
		MinDropVND: cfg.Sources.Defaults.MinDropVND,
		Targets: func(product, condition string) int64 {
			p, _ := cfg.ProductByID(product)
			return p.Target(condition)
		},
	})

	return rep, history(runs, now), nil
}

func runSource(ctx context.Context, src config.Source, m *match.Matcher, f Fetcher, now time.Time) (diff.SourceRun, []Unmatched) {
	run := diff.SourceRun{Shop: src.Shop, URL: src.URL, Mode: src.Mode}

	a := adapters.WithClock(adapters.ForShop(src.Shop), func() time.Time { return now })
	fetchURL, err := adapters.FetchURL(a, src.URL, src.Mode)
	if err != nil {
		run.Err = err
		return run, nil
	}

	offers, empty, err := fetchOffers(ctx, a, src, fetchURL, f)
	if err != nil {
		run.Err = err
		return run, nil
	}
	run.OfferCount = len(offers)
	run.Empty = empty

	var unmatched []Unmatched
	if src.Mode == config.ModeProduct {
		if len(offers) == 0 {
			return run, nil
		}
		// A product page is one product; the URL from sources.yaml is its
		// identity, whatever canonical URL the page reports.
		o := offers[0]
		o.URL = src.URL
		if res := m.Match(o.Title); !slices.Contains(res.Candidates, src.Product) {
			unmatched = append(unmatched, Unmatched{
				Shop: src.Shop, URL: src.URL, Title: o.Title, Status: "mismatch", Candidates: res.Candidates,
			})
		}
		run.Observations = append(run.Observations, observation(o, src.Product, src))
		return run, unmatched
	}

	for _, o := range offers {
		res := m.Match(o.Title)
		if res.Status != match.Matched {
			unmatched = append(unmatched, Unmatched{
				Shop: src.Shop, URL: o.URL, Title: o.Title, Status: res.Status.String(), Candidates: res.Candidates, NearMiss: res.NearMiss,
			})
			continue
		}
		run.Observations = append(run.Observations, observation(o, res.ProductID, src))
	}
	return run, unmatched
}

// maxPages bounds how many pages one listing source may span.
const maxPages = 20

// fetchOffers fetches a source and, for listings, every following page the
// adapter reports. It is all or nothing: if any page fails, the source fails,
// so offers on a missing page are never reported as gone. empty is true when
// the first page yields no offers and states that the listing has no items.
func fetchOffers(ctx context.Context, a adapters.Adapter, src config.Source, fetchURL string, f Fetcher) (offers []adapters.Offer, empty bool, err error) {
	seen := map[string]bool{} // offers from earlier pages, by URL and title
	for page := 1; fetchURL != ""; page++ {
		if page > maxPages {
			return nil, false, fmt.Errorf("listing has more than %d pages", maxPages)
		}
		allowed, err := f.Allowed(ctx, fetchURL)
		switch {
		case err != nil:
			return nil, false, err
		case !allowed:
			return nil, false, fmt.Errorf("%s: %w", fetchURL, ErrDisallowed)
		}
		res, err := f.Get(ctx, fetchURL)
		if err != nil {
			return nil, false, err
		}
		pageOffers, err := adapters.Extract(a, src.URL, res.Body)
		if err != nil {
			return nil, false, fmt.Errorf("page %d: %w", page, err)
		}
		if page == 1 && len(pageOffers) == 0 {
			empty = adapters.ReportsEmpty(a, res.Body)
		}
		// A listing can shift while it is paged through; skip an offer
		// already seen on an earlier page.
		for _, o := range pageOffers {
			if !seen[o.URL+"\x00"+o.Title] {
				offers = append(offers, o)
			}
		}
		for _, o := range pageOffers {
			seen[o.URL+"\x00"+o.Title] = true
		}

		if src.Mode != config.ModeListing {
			break
		}
		next, err := adapters.NextPage(a, fetchURL, res.Body)
		if err != nil {
			return nil, false, err
		}
		if next == fetchURL {
			break
		}
		fetchURL = next
	}
	return offers, empty, nil
}

func observation(o adapters.Offer, product string, src config.Source) diff.Observation {
	return diff.Observation{
		Shop:      src.Shop,
		URL:       o.URL,
		Title:     o.Title,
		Product:   product,
		Condition: resolveCondition(o, src),
		PriceVND:  o.PriceVND,
		InStock:   o.InStock,
	}
}

// resolveCondition: what the shop says, else what the source says, else a
// guess from the title and URL (condition: auto).
func resolveCondition(o adapters.Offer, src config.Source) string {
	switch {
	case o.Condition != "":
		return o.Condition
	case src.Condition == config.ConditionNew, src.Condition == config.ConditionUsed:
		return src.Condition
	default:
		return match.DetectCondition(o.Title, o.URL)
	}
}

// history builds one record per offer per run, de-duplicated by shop|url.
func history(runs []diff.SourceRun, now time.Time) map[string][]state.Record {
	out := map[string][]state.Record{}
	seen := map[string]bool{}
	for _, run := range runs {
		if run.Err != nil {
			continue
		}
		for _, ob := range run.Observations {
			key := state.Key(ob.Shop, ob.URL)
			if seen[key] {
				continue
			}
			seen[key] = true
			out[ob.Product] = append(out[ob.Product], state.Record{
				TS:        now,
				Shop:      ob.Shop,
				URL:       ob.URL,
				PriceVND:  ob.PriceVND,
				InStock:   ob.InStock,
				Condition: ob.Condition,
			})
		}
	}
	return out
}

func logf(w io.Writer, format string, args ...any) {
	if w != nil {
		_, _ = fmt.Fprintf(w, format, args...)
	}
}
