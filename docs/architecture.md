# Architecture

How fujiwatch works inside. For configuration and running it, see [usage.md](usage.md); for each shop's quirks, see [shops.md](shops.md).

- [Pipeline](#pipeline)
- [Repository layout](#repository-layout)
- [Shop adapters](#shop-adapters)
- [Matching and normalisation](#matching-and-normalisation)
- [State, history and events](#state-history-and-events)
- [Notifiers](#notifiers)
- [Price-history site](#price-history-site)
- [Being a polite scraper](#being-a-polite-scraper)

---

## Pipeline

```
GitHub Actions (cron, daily)
        │
        ▼
  fujiwatch run (Go binary)
   ├─ load config/watchlist.yaml + config/sources.yaml
   ├─ load data/state.json
   ├─ for each source: fetch (every page of a listing) → shop adapter → []Offer
   ├─ matcher: Offer.Title → product ID (or reject)
   ├─ diff against state → []Event
   ├─ append data/history/<product-id>.jsonl
   ├─ notify (WhatsApp instant events, email daily digest)
   └─ write data/state.json
        │
        ▼
  git commit data/ (by the workflow)
        │
        ▼
  build site/ (Svelte + Vite) reading data/history → GitHub Pages
```

**Two watch modes**

- `product`: a fixed product URL. Track price and stock. Events: `price_drop`, `price_rise`, `back_in_stock`, `out_of_stock`, `below_target`. The URL from `sources.yaml` is the offer's identity, whatever canonical URL the page reports; a title that doesn't match the configured product is reported as a `mismatch`.
- `listing`: a category, search or used-goods page with many items. Every item title goes through the matcher. Event: `new_listing` (the item URL is the identity key), plus the product-mode events for listings already seen.

A failure in one source never aborts the run; it becomes an `adapter_broken` event. Only a cancelled context does.

---

## Repository layout

```
fujiwatch/
├── cmd/fujiwatch/               # CLI: run, snapshot, extract, match, sitedata; .env loading
├── internal/
│   ├── config/                  # YAML loading + validation
│   ├── fetch/                   # HTTP client: UA, timeouts, retries, per-host delay; robots.txt
│   ├── adapters/                # page → []Offer
│   │   ├── adapter.go           # Offer, interfaces, registry (incl. CSS layouts per shop)
│   │   ├── generic.go           # JSON-LD / Open Graph / microdata extraction
│   │   ├── css.go               # selector-driven adapter (Layout)
│   │   ├── haravan.go           # xspace
│   │   ├── vjshop.go
│   │   └── woocommerce.go       # Store API shops
│   ├── match/                   # title normalisation, product matching, condition detection
│   ├── price/                   # VND parsing
│   ├── runner/                  # one cycle: sources → offers → matches → diff; text report
│   ├── diff/                    # state × observations → events
│   ├── state/                   # state.json + history jsonl
│   ├── notify/                  # email, CallMeBot WhatsApp, weekly summary
│   ├── snapshot/                # saving pages to testdata/
│   └── sitedata/                # data/ → JSON for the history site
├── config/
│   ├── watchlist.yaml
│   └── sources.yaml
├── data/                        # written by CI; committed
│   ├── state.json
│   └── history/*.jsonl
├── testdata/<shop>/             # saved page snapshots, .meta.json, .golden.json
├── site/                        # Svelte + Vite static history site
├── docs/                        # usage, architecture, shop notes
└── .github/workflows/           # watch.yml (daily run + site), ci.yml
```

Dependencies: `github.com/PuerkitoBio/goquery` (HTML), `gopkg.in/yaml.v3` (config), `golang.org/x/text` (diacritic stripping), the standard library for everything else. No headless browser: every shop so far serves its data without JavaScript.

---

## Shop adapters

```go
type Offer struct {
    Shop      string
    URL       string
    Title     string
    PriceVND  int64  // 0 = price not shown ("Liên hệ")
    InStock   *bool  // nil = unknown
    Condition string // "new" | "used" | "" (unknown; the source decides)
}

type Adapter interface {
    Name() string
    Extract(pageURL string, doc *goquery.Document, raw []byte) ([]Offer, error)
}
```

Optional interfaces an adapter can implement:

| Interface | Purpose | Used by |
|---|---|---|
| `URLRewriter` | `FetchURL(pageURL, mode)`: fetch something other than the configured page, e.g. a JSON endpoint behind a product page. | xspace (`.js`), vjshop (product API), WooCommerce (Store API) |
| `Paginator` | `NextPage(fetchURL, raw)`: the URL of the next listing page, or `""`. The runner follows it up to 20 pages. | vjshop |
| `ListingCounter` | `ListingTotal(raw)`: the item count the page states, so an empty listing (the last used lens sold) is not mistaken for a broken page. | vjshop |
| `clockSetter` | Use the run's time instead of the wall clock (sale windows). | vjshop |

**Kinds of adapters** (registry in `internal/adapters/adapter.go`):

- **Platform adapters** read an API or embedded JSON: Haravan, vjshop, WooCommerce.
- **CSS layouts** for shops without structured data: `Layout{Name, Marker, Card, Title, Link, Price, Stock}`. The first layout whose marker matches and that yields offers wins; a comma-separated selector inside a card is a priority order. If no layout matches, the generic extractor gets a try.
- **Generic** (`generic.go`), for shops without an adapter and as a fallback: JSON-LD (`Product` / `ItemList` → `offers.price`, `offers.availability`), then Open Graph (`og:price:amount`, `product:availability`), then microdata (`itemprop="price"`).

JSON bodies are never parsed as HTML: API responses embed product descriptions whose markup can overflow the HTML parser.

**Multi-page listings:** all or nothing. If any page fails, the whole source fails, so offers on a missing page are never reported as gone. An offer that shifts onto the next page while the listing is being read is counted once.

**Tests:** every snapshot in `testdata/<shop>/` has a `.golden.json` with the extracted offers (`go test ./internal/adapters -update` rewrites them); spec tests assert the important fields per shop.

---

## Matching and normalisation

Every title is normalised before matching:

1. Lowercase.
2. Map `đ` → `d` explicitly. Unicode NFD does **not** decompose `đ`.
3. NFD-normalise, then remove combining marks (`runes.Remove(runes.In(unicode.Mn))`). This turns "ống kính" into "ong kinh".
4. Unify variants: `f/2.8` and `f2.8` → `f2.8`; en-dash and em-dash → `-`; collapse whitespace; `x t5` / `xt5` / `x-t5` → `x-t5`.

Matching rules per product: all `patterns` must match, all `require_all` must match, at least one `require_any` must match (if set), and none of `exclude` may match. An offer that matches **zero or more than one** product is logged as unmatched or ambiguous and not alerted on; the daily email lists those so the rules can be tuned.

**Condition.** The first that applies wins:

1. the adapter's own verdict (e.g. vjshop items in a `*-cu` category or with a condition grade are used);
2. the source's `condition: new | used`;
3. detection (`condition: auto`): used if the normalised title or URL contains `cu` (whole word), `used`, `like new`, a grade of 80–99% ("98%"; "giảm 10%" is a discount, not a grade), `qua su dung` or `2nd`.

**Price parsing (`internal/price`)** handles `27,490,000₫`, `27.490.000 đ`, `27.490.000đ`, `27490000`, and "Liên hệ" (call for price → 0): it strips everything except digits, then sanity-checks the range (500,000–200,000,000 VND).

---

## State, history and events

`data/state.json` holds the last known state of every tracked offer, keyed by `shop|url`, plus the health of every source page (used for `adapter_broken`):

```json
{
  "offers": {
    "xspace|https://fujifilm-xspace.vn/products/...": {
      "product": "xf10-24-ii",
      "shop": "xspace",
      "url": "https://fujifilm-xspace.vn/products/...",
      "title": "Ống kính Fujinon XF10-24mm F4 R OIS WR II",
      "price_vnd": 27490000,
      "in_stock": true,
      "condition": "new",
      "source": "xspace|https://fujifilm-xspace.vn/products/...",
      "first_seen": "2026-11-07T23:17:00Z",
      "last_seen": "2026-11-07T23:17:00Z"
    }
  },
  "sources": {
    "xspace|https://fujifilm-xspace.vn/products/...": { "last_count": 1, "last_ok": "2026-11-07T23:17:00Z" }
  }
}
```

Only matched offers are tracked. If a page hides the price ("Liên hệ") or the stock, the last known value is kept, so the next visible price is compared against it. The file is written atomically (temp file + rename).

`data/history/<product-id>.jsonl` gets one line per offer per run:

```json
{"ts":"2026-11-07T23:17:00Z","shop":"xspace","url":"...","price_vnd":27490000,"in_stock":true,"condition":"new"}
```

**Events (`internal/diff`)**

- `price_drop` / `price_rise`: only drops are alerted on. Optional threshold in `sources.yaml` (`min_drop_pct`, `min_drop_vnd`: a drop counts if it meets either) to ignore noise. The stored price is updated either way.
- `back_in_stock` / `out_of_stock`
- `new_listing`: the URL has not been seen before. Only raised in listing mode; the first sighting of a product-mode URL is silent. The same URL found by two sources in one run counts once (the first source wins).
- `gone`: not seen for 3 consecutive runs. Removed from state and logged (useful for used items: "sold"). Only runs in which the offer's own source worked count, so a broken shop never marks its items as sold; offers of a source removed from `sources.yaml` age out the same way.
- `adapter_broken`: the fetch, robots.txt check or extraction failed, the HTTP status is not 200, a page of a multi-page listing failed, or a source that previously returned ≥ 1 offer now returns 0 (unless the page itself states the listing is empty, e.g. vjshop's `meta.total` of 0). Always included in the email, so silent failures are visible.
- `below_target`: an offer's price reached the product's `target_vnd` (new) or `target_used_vnd` (used). Raised once per crossing: when the price goes from above the target (or unknown) to at or below it, including a first sighting already below it.

---

## Notifiers

```go
type Notifier interface {
    Name() string
    Send(ctx context.Context, msg *notify.Message) error
}
```

`Message` carries the subject, the events, the best-price table, source health and unmatched offers, built from the run report and the updated state. Notifiers are configured from environment variables ([usage.md: notifications](usage.md#notifications)); a send failure is a warning, never a run failure, so the data is still committed.

---

## Price-history site

- `site/`: Svelte 5 + Vite, built to static files in `site/dist`.
- At build time, `fujiwatch sitedata` reads `data/history/*.jsonl` and `data/state.json` and writes `site/public/data/`:
  - `index.json`: every watched product (watchlist order) with its current offers (cheapest first) and its best new and best used price (in stock or unknown stock, price shown);
  - `<product-id>.json`: one series per shop and condition (new and used separate), one point per **Vietnam** day at that day's lowest price, with the listing URL. Days where the price was hidden are left out.
- One page:
  - a product selector (the selection is kept in the URL hash, e.g. `#x-t5`)
  - a line chart per product, one line per shop; used listings are dashed with open markers; hover shows the prices of a day; legend entries toggle series; target prices are drawn as lines
  - a new / used / all filter
  - a table of current offers with links to the shop pages
- Charting: hand-rolled SVG, no chart library (≈ 19 kB gzipped JS in total). Light and dark mode; works at phone width.
- No backend and no API calls: the page only loads the JSON files that were generated next to it at build time.

---

## Being a polite scraper

- One request at a time per host, with a 3 s delay (`delay_ms`); about 40 page requests per day in total, plus one `robots.txt` per host.
- A descriptive User-Agent: `fujiwatch/0.1 (personal price watcher; +https://github.com/rbscholtus/fujiwatch)`.
- `robots.txt` is respected: a disallowed URL is never fetched, and the source reports `adapter_broken`.
- Timeouts (20 s) and at most 2 retries with backoff. A failure in one shop never aborts the run.
