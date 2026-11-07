// Package runner runs one watch cycle: fetch every source, extract offers,
// match them, diff against state and collect history records.
package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"slices"
	"sync"
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
	PostForm(ctx context.Context, rawURL string, form url.Values) (*fetch.Response, error)
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
	Shop    string  `json:"shop"`
	Mode    string  `json:"mode"`
	URL     string  `json:"url"`
	Offers  int     `json:"offers"`  // extracted
	Matched int     `json:"matched"` // that matched a watched product
	Pages   int     `json:"pages"`   // requests made (listing pages)
	Seconds float64 `json:"seconds"` // time taken, including polite delays
	Err     string  `json:"error,omitempty"`
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

	var selected []config.Source
	for _, src := range cfg.Sources.Sources {
		configured[state.Key(src.Shop, src.URL)] = true
		if opts.Only == "" || src.Shop == opts.Only {
			selected = append(selected, src)
		}
	}
	results := runShops(ctx, selected, m, f, now, opts.Log)
	if err := ctx.Err(); err != nil {
		return nil, nil, fmt.Errorf("run cancelled: %w", err)
	}

	// Results in config order: when two sources find one URL, the first wins.
	for i, src := range selected {
		run, unmatched := results[i].run, results[i].unmatched
		runs = append(runs, run)
		for _, u := range unmatched {
			if k := state.Key(u.Shop, u.URL); !unmatchedSeen[k] {
				unmatchedSeen[k] = true
				rep.Unmatched = append(rep.Unmatched, u)
			}
		}
		rep.Seen = append(rep.Seen, run.Observations...)

		sr := SourceReport{
			Shop: src.Shop, Mode: src.Mode, URL: src.URL, Offers: run.OfferCount, Matched: len(run.Observations),
			Pages: results[i].pages, Seconds: results[i].elapsed.Round(100 * time.Millisecond).Seconds(),
		}
		if run.Err != nil {
			sr.Err = run.Err.Error()
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

type sourceResult struct {
	run       diff.SourceRun
	unmatched []Unmatched
	pages     int
	elapsed   time.Duration
}

// runShops runs the sources of each shop in order, and the shops
// concurrently: the fetcher keeps one request at a time per host with its
// polite delay, so a run takes as long as its slowest shop rather than the
// sum of all of them. Results are indexed like sources.
func runShops(ctx context.Context, sources []config.Source, m *match.Matcher, f Fetcher, now time.Time, log io.Writer) []sourceResult {
	results := make([]sourceResult, len(sources))
	byShop := map[string][]int{}
	var shops []string
	for i, s := range sources {
		if byShop[s.Shop] == nil {
			shops = append(shops, s.Shop)
		}
		byShop[s.Shop] = append(byShop[s.Shop], i)
	}

	var logMu sync.Mutex
	var wg sync.WaitGroup
	for _, shop := range shops {
		wg.Go(func() {
			for _, i := range byShop[shop] {
				if ctx.Err() != nil {
					return
				}
				src := sources[i]
				start := time.Now()
				run, unmatched, pages := runSource(ctx, src, m, f, now)
				results[i] = sourceResult{run: run, unmatched: unmatched, pages: pages, elapsed: time.Since(start)}

				logMu.Lock()
				logf(log, "%-14s %-7s %2d page(s) %5.1fs  %s\n", src.Shop, src.Mode, pages, results[i].elapsed.Seconds(), src.URL)
				if run.Err != nil {
					logf(log, "%-14s error: %v\n", "", run.Err)
				}
				logMu.Unlock()
			}
		})
	}
	wg.Wait()
	return results
}

func runSource(ctx context.Context, src config.Source, m *match.Matcher, f Fetcher, now time.Time) (run diff.SourceRun, unmatched []Unmatched, pages int) {
	run = diff.SourceRun{Shop: src.Shop, URL: src.URL, Mode: src.Mode}

	a := adapters.WithClock(adapters.ForShop(src.Shop), func() time.Time { return now })
	fetchURL, err := adapters.FetchURL(a, src.URL, src.Mode)
	if err != nil {
		run.Err = err
		return run, nil, pages
	}

	offers, empty, pages, err := fetchOffers(ctx, a, src, fetchURL, f)
	if err != nil {
		run.Err = err
		return run, nil, pages
	}
	run.OfferCount = len(offers)
	run.Empty = empty

	if src.Mode == config.ModeProduct {
		if len(offers) == 0 {
			return run, nil, pages
		}
		// A product page is one product; the URL from sources.yaml is its
		// identity, whatever canonical URL the page reports. A page with
		// variants (body, kits, used units) yields an offer per variant: use
		// the ones that match the product, preferring the source's
		// condition, then in stock, then the lowest price.
		o, ok := pickVariant(offers,
			func(t string) bool { return slices.Contains(m.Match(t).Candidates, src.Product) },
			func(o adapters.Offer) bool { return resolveCondition(o, src) == src.Condition })
		if !ok {
			unmatched = append(unmatched, Unmatched{
				Shop: src.Shop, URL: src.URL, Title: o.Title, Status: "mismatch", Candidates: m.Match(o.Title).Candidates,
			})
		}
		o.URL = src.URL
		run.Observations = append(run.Observations, observation(o, src.Product, src))
		return run, unmatched, pages
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
	return run, unmatched, pages
}

// maxPages bounds how many pages one listing source may span.
const maxPages = 20

// fetchOffers fetches a source and, for listings, every following page the
// adapter reports. It is all or nothing: if any page fails, the source fails,
// so offers on a missing page are never reported as gone. empty is true when
// the first page yields no offers and states that the listing has no items.
func fetchOffers(ctx context.Context, a adapters.Adapter, src config.Source, fetchURL string, f Fetcher) (offers []adapters.Offer, empty bool, pages int, err error) {
	seen := map[string]bool{} // offers from earlier pages, by URL and title
	cur := adapters.Page{URL: fetchURL}
	for page := 1; !cur.IsZero(); page++ {
		if page > maxPages {
			return nil, false, pages, fmt.Errorf("listing has more than %d pages", maxPages)
		}
		allowed, err := f.Allowed(ctx, cur.URL)
		switch {
		case err != nil:
			return nil, false, pages, err
		case !allowed:
			return nil, false, pages, fmt.Errorf("%s: %w", cur.URL, ErrDisallowed)
		}
		pages = page
		var res *fetch.Response
		if cur.Form != nil {
			res, err = f.PostForm(ctx, cur.URL, cur.Form)
		} else {
			res, err = f.Get(ctx, cur.URL)
		}
		if err != nil {
			return nil, false, pages, err
		}
		pageOffers, err := adapters.Extract(a, src.URL, res.Body)
		if err != nil {
			return nil, false, pages, fmt.Errorf("page %d: %w", page, err)
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

		// Product pages follow too: a WooCommerce product's variations
		// are a second request.
		next, err := adapters.NextPage(a, cur, res.Body)
		if err != nil {
			return nil, false, pages, err
		}
		if next.Key() == cur.Key() {
			break
		}
		cur = next
	}
	return offers, empty, pages, nil
}

// pickVariant returns the best offer whose title matches: in the wanted
// condition first, then in stock before unknown before out of stock, then
// the lowest shown price. ok is false if none matches; the first offer is
// returned then.
func pickVariant(offers []adapters.Offer, matches func(title string) bool, wanted func(adapters.Offer) bool) (best adapters.Offer, ok bool) {
	rank := func(o adapters.Offer) int {
		switch {
		case o.InStock == nil:
			return 1
		case *o.InStock:
			return 0
		default:
			return 2
		}
	}
	better := func(a, b adapters.Offer) bool {
		if wa, wb := wanted(a), wanted(b); wa != wb {
			return wa
		}
		if ra, rb := rank(a), rank(b); ra != rb {
			return ra < rb
		}
		if (a.PriceVND > 0) != (b.PriceVND > 0) {
			return a.PriceVND > 0
		}
		return a.PriceVND < b.PriceVND
	}
	for _, o := range offers {
		if matches(o.Title) && (!ok || better(o, best)) {
			best, ok = o, true
		}
	}
	if !ok {
		return offers[0], false
	}
	return best, true
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
