# fujiwatch

A small price watcher for Fujifilm X-mount cameras and lenses in Vietnamese camera shops.

It runs once a day on GitHub Actions. Each run checks a list of shop pages, records prices, and sends an alert (WhatsApp and/or email) when:

- a watched product drops in price or comes back in stock (**new gear**), or
- a new matching listing appears on a shop's used or category page (**used gear**).

A static price-history site, built with Svelte and published on GitHub Pages, shows a chart per product over time.

---

## 1. Goals and scope

**Watched items (initial)**

| ID | Item | Notes |
|---|---|---|
| `x-t3` | Fujifilm X-T3 body | new + used; must not match X-T30 / X-T30 II / X-T30 III |
| `x-t4` | Fujifilm X-T4 body | new + used |
| `x-t5` | Fujifilm X-T5 body | new + used; must not match X-T50 |
| `xf10-24-i` | XF10-24mm F4 R OIS (Mark I) | tracked separately from Mark II (different prices) |
| `xf10-24-ii` | XF10-24mm F4 R OIS WR (Mark II) | Mark II is the WR version |
| `tamron-16-50` | Tamron 16-50mm F2.8 Di III VXD | **X-mount only**; reject Sony E / Nikon Z / Canon RF variants |

More lenses will be added later. Adding an item must only require editing `config/watchlist.yaml`.

**Shops (initial)**

| Shop | Site | Platform (verified) | How it is read |
|---|---|---|---|
| xspace | fujifilm-xspace.vn | Haravan | `/products/<handle>.js` JSON |
| vjshop | vjshop.vn | Next.js | `__NEXT_DATA__` (listings), `api.vjshop.vn` (products) |
| kyma | kyma.vn | Custom | CSS selectors on category pages |
| zshop | zshop.vn | CS-Cart | CSS selectors (category and product pages) |
| mayanhhoangto | mayanhhoangto.com | WooCommerce | Store API |
| bncamera | bncamera.com | WooCommerce | Store API |
| mayanh24h | mayanh24h.com | Custom (AngularJS front, server-rendered lists) | CSS selectors (category and product pages) |
| x-camera | x-camera.vn | WooCommerce | Store API |
| giangduydat | giangduydat.vn | Custom | CSS selectors; product pages list each unit as a schema.org Offer |

**Out of scope:** Shopee, Lazada, Tiki, Chợ Tốt (scam risk), Facebook Marketplace and groups.

---

## 2. Architecture

```
GitHub Actions (cron, daily)
        │
        ▼
  fujiwatch (Go binary)
   ├─ load config/watchlist.yaml + config/sources.yaml
   ├─ load data/state.json
   ├─ for each source: fetch → shop adapter → []Offer
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

- `product`: a fixed product URL. Track price and stock. Events: `price_drop`, `price_rise`, `back_in_stock`, `out_of_stock`.
- `listing`: a category, search or used-goods page with many items. Every item title goes through the matcher. Event: `new_listing` (the item URL is the identity key), plus `price_drop` for listings already seen.

---

## 3. Repository layout

```
fujiwatch/
├── cmd/fujiwatch/main.go        # CLI entrypoint
├── internal/
│   ├── config/                  # YAML loading + validation
│   ├── fetch/                   # HTTP client: UA, timeouts, retries, polite delay
│   ├── adapters/                # one file per shop + generic.go
│   │   ├── adapter.go           # interface + registry
│   │   ├── generic.go           # JSON-LD / og:price / microdata extraction
│   │   ├── xspace.go
│   │   ├── vjshop.go
│   │   └── ...
│   ├── match/                   # title normalisation + product matching
│   ├── price/                   # VND parsing
│   ├── state/                   # state.json + history jsonl
│   ├── diff/                    # state × offers → events
│   └── notify/                  # notifier interface, email, callmebot
├── config/
│   ├── watchlist.yaml
│   └── sources.yaml
├── data/                        # written by CI; committed
│   ├── state.json
│   └── history/*.jsonl
├── testdata/                    # saved HTML snapshots per shop (golden tests)
├── site/                        # Svelte + Vite static history site
├── .github/workflows/watch.yml
├── go.mod
└── README.md
```

Suggested Go dependencies:

- `github.com/PuerkitoBio/goquery`: HTML parsing
- `gopkg.in/yaml.v3`: config
- `golang.org/x/text/unicode/norm` + `golang.org/x/text/runes`: diacritic stripping
- Standard library for everything else (`net/http`, `net/smtp`, `encoding/json`)
- Optional, only if a shop truly needs JavaScript: `github.com/chromedp/chromedp`. Avoid it if possible; it makes CI slower and heavier.

---

## 4. Configuration

### `config/watchlist.yaml`

```yaml
products:
  - id: x-t3
    name: "Fujifilm X-T3"
    kind: body
    # A token pattern is matched against the *normalised* title (see §6).
    # \b-style boundaries are essential: "xt3" must not match "xt30".
    # "x-t3"/"xt3" followed by a non-digit or end of title → never matches X-T30 / X-T30 II
    patterns: ['\bx-?t3(\D|$)']

  - id: x-t4
    name: "Fujifilm X-T4"
    kind: body
    patterns: ['\bx-?t4(\D|$)']

  - id: x-t5
    name: "Fujifilm X-T5"
    kind: body
    patterns: ['\bx-?t5(\D|$)']   # never matches X-T50

  - id: xf10-24-ii
    name: "XF10-24mm F4 R OIS WR (II)"
    kind: lens
    patterns: ['10-24']
    require_all: ['\bwr\b|\bii\b|mark ?ii']

  - id: xf10-24-i
    name: "XF10-24mm F4 R OIS (I)"
    kind: lens
    patterns: ['10-24']
    exclude: ['\bwr\b', '\bii\b', 'mark ?ii']

  - id: tamron-16-50
    name: "Tamron 16-50mm F2.8 Di III VXD (X)"
    kind: lens
    patterns: ['tamron', '16-50']          # all patterns must match
    require_any: ['fuji', 'fujifilm', 'x-?mount', 'ngam x', '\bfx\b']
    exclude: ['sony', '\bfe\b', 'e-?mount', 'nikon', '\bz\b', 'canon', '\brf\b']
```

Optional per product: `target_vnd` and `target_used_vnd` (see §7, `below_target`).

> Note: Go's `regexp` (RE2) does **not** support lookahead `(?!…)`, so model-number boundaries use a negated class instead: `(\D|$)` means "followed by a non-digit or end of string". Use `\D`, not `[^0]`, so no other trailing digit slips through either. Prefer this over `exclude: ['x-?t30']`: an exclude rejects the whole title, which would wrongly drop a listing such as "X-T3 kèm grip, dùng chung pin với X-T30". Keep `exclude` for real confusions (other mounts or brands). Normalisation (§6) must turn "X T3" / "XT3" / "X-T3" into the same form *before* these patterns run.

### `config/sources.yaml`

```yaml
defaults:
  delay_ms: 3000        # between requests to the same host
  timeout_s: 20

sources:
  - shop: xspace
    mode: product
    product: xf10-24-ii
    url: https://fujifilm-xspace.vn/products/fujinon-xf10-24mmf4-r-ois-wr-ii

  - shop: vjshop
    mode: listing
    condition: used      # used | new | auto (detect from title)
    url: https://vjshop.vn/do-cu-chat        # refine to the camera/lens subcategory

  - shop: mayanhhoangto
    mode: listing
    condition: auto
    url: https://mayanhhoangto.com/ong-kinh/ong-kinh-fuji/
  # ... one entry per page to watch
```

The actual product and listing URLs still need to be collected. The URLs used during design were only examples of each shop's URL format.

---

## 5. Shop adapters

```go
type Offer struct {
    Shop      string
    URL       string
    Title     string
    PriceVND  int64  // 0 = price not shown ("Liên hệ")
    InStock   *bool  // nil = unknown
    Condition string // "new" | "used" | ""
}

type Adapter interface {
    Name() string
    Extract(pageURL string, doc *goquery.Document, raw []byte) ([]Offer, error)
}
```

**Extraction order (generic.go, used by all adapters first)**

1. JSON-LD `<script type="application/ld+json">` with `@type: Product` or `ItemList` → `offers.price`, `offers.availability`.
2. Open Graph meta tags: `og:price:amount`, `og:price:currency`, `product:availability`.
3. Microdata (`itemprop="price"`).
4. Shop-specific CSS selectors (in the shop's adapter file).

**Per-shop notes**

- **fujifilm-xspace (Haravan):** product pages expose `og:price:amount` (e.g. `27490000`) and `og:price:currency` = `VND`. Stock text is in `.checkProduct`: "Còn hàng" (in stock) or "Hết hàng" (out of stock); related-product cards elsewhere on the page also say "Hết hàng", so don't search the whole page. Product JSON at `/products/<handle>.js` works (verified) and is preferred: prices are in VND × 100, with `available` per variant. `/search` is disallowed in `robots.txt`.
- **vjshop (Next.js):** the price is not in the visible server-rendered text. Listing pages embed the product list in `<script id="__NEXT_DATA__" type="application/json">` at `props.pageProps.products` (`core.name`, `core.slug`, `real_price`, `core.status.raw`, `core.product_category.slug`). Product pages are rendered client-side and carry **no** product data, so product mode fetches `https://api.vjshop.vn/api/products/<slug>` instead; that response has no `real_price`, only `prices.{default,sale,flashsale}` with priorities and optional `from`/`to` windows (Vietnam time), so the adapter picks the highest-priority price active now. The site has a used-goods section ("Đồ cũ chất", `/do-cu-chat`); `robots.txt` disallows `/may-anh-cu` and `/ong-kinh-cu`, so don't watch those. Product URLs carry the variant (e.g. `...-silver-chinh-hang`). Unknown slugs return HTTP 200 with an empty page, so a wrong URL shows up as `adapter_broken`, not as a 404.
- **kyma:** the search page is server-rendered, but its search is picky; a query like `XF 18-55mm f/2.8-4` returned 0 results. Watch category pages (e.g. `/ong-kinh-fujifilm`, `/may-anh-fujifilm`) or exact product URLs instead of search.
- **WooCommerce shops (bncamera, x-camera, probably mayanhhoangto):** try the Store API first: `GET /wp-json/wc/store/v1/products?search=<term>` returns clean JSON with prices (in minor units; check `currency_minor_unit`). It is often enabled by default. Fall back to HTML: `.product .price .woocommerce-Price-amount`.
- **WooCommerce details:** names contain HTML entities (`&#8211;`); prices are strings in minor units; `is_on_backorder` ("Cho phép đặt hàng trước", pre-order allowed) counts as **not** in stock even when `is_in_stock` is true. The adapter rewrites shop URLs: product page → `?slug=<slug>`, search page `?s=<term>` → `?search=<term>`, category page → `?category=<last path segment>` (an unknown slug returns `[]`). Responses embed full HTML descriptions, so JSON bodies are never parsed as HTML. Snapshots in `testdata/` are trimmed to the fields the adapter reads.
- **kyma:** cards are `li.li-product`; the first `span` in `.item__price` is the current price (the second is the struck-through one). No stock labels on category pages.
- **zshop (CS-Cart, not Magento):** cards `.ty-grid-list__item`, price `.ty-price .ty-price-num`; product pages `h1.ty-product-block-title`. Upcoming products have no price (→ 0).
- **mayanh24h:** `robots.txt` disallows every URL with a query string. Cards `.product-item` with a `.stock-status` label; used items are graded in the title ("Mới 98%" = 98% condition). Product pages: `h1.text_1`, price `h2.text-primary`, stock after "Tình trạng:".
- **giangduydat:** category cards `li.idx-prod` ("Từ 4.490.000 đ" = from). A product page lists every unit (new, used 85%, 90%, …) as its own `Offer` with a name and price, so watch product pages in **listing** mode; each unit gets the page URL plus a `#<slug of its name>` fragment as identity.
- Shops without structured data are configured as **layouts** in `internal/adapters/adapter.go` (`Layout{Marker, Card, Title, Link, Price, Stock}`); a comma-separated selector inside a card is a priority order.

**Workflow for adding a shop**

1. `go run ./cmd/fujiwatch snapshot <url>` saves `testdata/<shop>/<slug>.html` (or `.json` when the adapter fetches a JSON endpoint) plus `<slug>.meta.json` (page URL, fetch URL, mode, fetch time).
2. Write the adapter. `go test ./internal/adapters -update` writes `<slug>.golden.json` for every snapshot; review it, commit it, and add explicit assertions for the important fields.
3. Add the entry to `sources.yaml`.

---

## 6. Matching and normalisation

Normalise every title before matching:

1. Lowercase.
2. Map `đ` → `d` explicitly. Unicode NFD does **not** decompose `đ`.
3. NFD-normalise, then remove combining marks (`runes.Remove(runes.In(unicode.Mn))`). This turns "ống kính" into "ong kinh".
4. Unify variants: `f/2.8` and `f2.8` → `f2.8`; en-dash and em-dash → `-`; collapse whitespace; `x t5` / `xt5` / `x-t5` → `x-t5`.
5. Strip marketing noise if it interferes: `chinh hang` (official), `gia tot` (good price).

Matching rules per product: all `patterns` must match, all `require_all` must match, at least one `require_any` must match (if set), and none of `exclude` may match. An offer that matches **zero or more than one** product is logged as unmatched or ambiguous and not alerted on; the daily email lists those so the rules can be tuned.

**Condition detection (`condition: auto`)** marks an offer as used if its normalised title or URL contains any of: `cu`, `used`, `like new`, `likenew`, `\d{2}%` (e.g. "98%"), `qua su dung`, `2nd`. Bare `cu` is noisy, so match it as a whole word only.

**Price parsing (`internal/price`)** must handle `27,490,000₫`, `27.490.000 đ`, `27.490.000đ`, `27490000`, and "Liên hệ" (call for price → 0). Strip everything except digits, then sanity-check the range (500,000–200,000,000 VND).

---

## 7. State, history and events

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

- `price_drop` / `price_rise`: only alert on drops by default. Optional threshold in `sources.yaml` (`min_drop_pct`, `min_drop_vnd`: a drop counts if it meets either) to ignore noise. The stored price is updated either way.
- `back_in_stock` / `out_of_stock`
- `new_listing`: the URL has not been seen before (listing mode)
- `gone`: not seen for 3 consecutive runs. Remove it from state and log it (useful for used items: "sold"). Only runs in which the offer's own source page worked count, so a broken shop never marks its items as sold; offers of a source removed from `sources.yaml` age out the same way.
- `adapter_broken`: a source that previously returned ≥ 1 offer now returns 0, or the HTTP status is not 200, or the fetch, robots.txt check or extraction failed. Always included in the email, so silent failures are visible.
- `below_target`: an offer's price reached the product's `target_vnd` (new) or `target_used_vnd` (used). Raised once per crossing: when the price goes from above the target (or unknown) to at or below it, including a first sighting already below it.
- `new_listing` is only raised in listing mode; the first sighting of a product-mode URL is silent. The same URL found on two source pages in one run counts once.

---

## 8. Notifications

Notifier interface:

```go
type Notifier interface {
    Name() string
    Send(ctx context.Context, msg *notify.Message) error
}
```

`Message` carries the subject, the events, the best-price table, source health and unmatched offers, built from the run report and the updated state. Notifiers are enabled by their environment variables (repository secrets, or a local `.env`; see `.env.example`). A notifier with only some of its variables set is skipped with a warning. Notification failures never fail the run (the data must still be committed); they are printed as warnings, and as `::warning::` annotations on GitHub Actions.

**WhatsApp via CallMeBot (instant, important events only: `below_target`, `price_drop`, `back_in_stock`, `new_listing`)**

When one listing has several of these in a run (e.g. `new_listing` and `below_target`), only the most important one is sent, in that order.

- One-time setup: add CallMeBot's number to your phone contacts and send it "I allow callmebot to send me messages". It replies with your personal API key. The current number and steps are at https://www.callmebot.com/blog/free-api-whatsapp-messages/
- Send with `GET https://api.callmebot.com/whatsapp.php?phone=<+84…>&text=<urlencoded>&apikey=<key>`
- Text only, keep messages short, at most one message per run (batch the events). It is a free shared service, so treat failures as non-fatal.

**Email via SMTP (daily digest, every run)**

- Gmail SMTP: `smtp.gmail.com:587` with STARTTLS. Requires 2-Step Verification and an **app password** (Google Account → Security → App passwords).
- Contents: events at the top, then a table of the current best price per product (new vs used), unmatched/ambiguous offers, and adapter health.
- Option (`notify:` in `sources.yaml`): `email: changes` sends the email only when there are events or failures, plus a weekly "all OK" heartbeat on `heartbeat: <weekday>` (Vietnam time). Default `email: always`.
- Weekly summary (`notify: weekly: <weekday>`): on that day the email always goes out, with the subject suffix "· weekly summary" and a table of each product's lowest new and used price over the last 7 days against the 7 days before, from `data/history`.
- Optional variables: `MAIL_FROM` (default `SMTP_USER`), `SMTP_ADDR` (default `smtp.gmail.com:587`). `MAIL_TO` may list several addresses, comma-separated. The password is only sent after STARTTLS.

**Instagram:** not supported. There is no practical API for a bot to DM a personal account.

---

## 9. CLI

```
fujiwatch run [--dry-run] [--only <shop>] [--no-notify]
fujiwatch snapshot <url>                 # save HTML to testdata/
fujiwatch extract <shop> <file.html>     # run one adapter on a saved file, print offers
fujiwatch match "<title>"                # show which product a title matches, and why
fujiwatch sitedata                       # build the history site's JSON from data/
```

`--dry-run` fetches and diffs but does not write state or send notifications. Use it locally all the time.

---

## 10. GitHub Actions

Two workflows:

- [`.github/workflows/watch.yml`](.github/workflows/watch.yml): the daily run. `cron: "17 23 * * *"` is 06:17 Asia/Saigon (cron is UTC), off the hour to avoid queue delays, plus a manual **Run workflow** button with optional `dry_run` and `only` inputs. The `scrape` job runs `go test ./...`, then `fujiwatch run`, then commits `data/` as `data: <date> [skip ci]` (only when it changed). The `site` job builds and deploys the history site (§11).
- [`.github/workflows/ci.yml`](.github/workflows/ci.yml): `go vet`, `go test -race` and `golangci-lint` on pushes and pull requests (data-only commits are ignored).

Actions are pinned to their current major versions (`actions/checkout@v7`, `actions/setup-go@v7`, …); bump them when GitHub releases new majors. `cmd/fujiwatch/workflow_test.go` checks the workflows against this section and executes their shell steps.

**Secrets** (repository → Settings → Secrets and variables → Actions): `CALLMEBOT_PHONE`, `CALLMEBOT_APIKEY`, `SMTP_USER`, `SMTP_PASS`, `MAIL_TO`.

**Things to know about GitHub Actions**

- Scheduled runs can start late (minutes, sometimes longer) at busy times. Fine for a daily job.
- In public repositories, scheduled workflows are disabled after 60 days without repository activity. The daily data commit should count as activity, but if the schedule ever stops, re-enable it in the Actions tab.
- **Public vs private repo:** GitHub Pages on a private repository requires a paid plan. A public repo is simplest: the watchlist and shop prices are not sensitive, and secrets stay secret either way. If you want the repo private on a free plan, drop the Pages job and rely on the email digest, or render the charts into the email.
- Runners are US datacenter machines. If a shop blocks datacenter traffic (e.g. Cloudflare challenges), that shop's adapter will report `adapter_broken`, and we decide case by case.

---

## 11. Price-history site (`site/`)

- Svelte 5 + Vite, built to static files in `site/dist`.
- At build time, `fujiwatch sitedata` (Go) reads `data/history/*.jsonl` and `data/state.json` and writes `site/public/data/`:
  - `index.json`: every watched product (watchlist order) with its current offers (cheapest first) and its best new and best used price (in stock or unknown stock, price shown);
  - `<product-id>.json`: one series per shop and condition (new and used separate), one point per **Vietnam** day at that day's lowest price, with the listing URL. Days where the price was hidden are left out.
- One page:
  - a product selector (the selection is kept in the URL hash, e.g. `#x-t5`)
  - a line chart per product, one line per shop; used listings are dashed with open markers; hover shows the prices of a day; legend entries toggle series
  - a new / used / all filter
  - a table of current offers with links to the shop pages
- Charting: hand-rolled SVG, no chart library (≈ 19 kB gzipped JS in total). Light and dark mode; works at phone width.
- No backend and no API calls: the page only loads the JSON files that were generated next to it at build time.
- Local preview: `go run ./cmd/fujiwatch sitedata && cd site && npm install && npm run dev`.

---

## 12. Being a polite scraper

- One request at a time per host, with a 2–5 s delay; about 20–40 requests per day in total.
- A descriptive User-Agent, e.g. `fujiwatch/0.1 (personal price watcher; contact: <email>)`.
- Respect `robots.txt` where it disallows the paths you fetch.
- Timeouts (20 s) and at most 2 retries with backoff. A failure in one shop must not abort the run.

---

## 13. Local development

```bash
go mod init github.com/<you>/fujiwatch
go get github.com/PuerkitoBio/goquery gopkg.in/yaml.v3 golang.org/x/text

# iterate on one shop
go run ./cmd/fujiwatch snapshot "https://fujifilm-xspace.vn/products/fujinon-xf10-24mmf4-r-ois-wr-ii"
go run ./cmd/fujiwatch extract xspace testdata/xspace/fujinon-xf10-24mmf4-r-ois-wr-ii.html
go run ./cmd/fujiwatch match "Ống kính Fujinon XF10-24mm F4 R OIS WR II"

# full run without side effects
go run ./cmd/fujiwatch run --dry-run

# site
go run ./cmd/fujiwatch sitedata          # data/ → site/public/data/
cd site && npm install && npm run dev
```

For local secrets, use a `.env` file (git-ignored) that is loaded only when present.

---

## 14. Milestones

All done; each was committed with spec tests that pass (`go test ./...`).

1. ✅ **Skeleton:** config loading, fetch client, VND price parser, normaliser and matcher with table-driven tests (X-T3 vs X-T30, X-T5 vs X-T50, XF10-24 Mark I vs Mark II, Tamron X vs E-mount).
2. ✅ **First two shops:** fujifilm-xspace (product mode) and vjshop (listing mode, used section), with golden tests from snapshots.
3. ✅ **State, history and diff:** events, `--dry-run` output.
4. ✅ **Notifications:** email digest, then CallMeBot WhatsApp.
5. ✅ **GitHub Actions:** schedule, data commit, secrets.
6. ✅ **Remaining shops:** kyma, then the WooCommerce shops (Store API), then zshop, mayanh24h, giangduydat.
7. ✅ **History site:** Svelte page on GitHub Pages.
8. ✅ **Nice-to-haves:** a per-product target price ("alert below X VND"), a weekly summary. More lenses: add them to `config/watchlist.yaml` (no code changes needed).

## 15. Open items

- [ ] Collect the real product, category and used-section URLs per shop for the watched items.
- [x] Repo visibility: **public** (GitHub Pages works on the free plan).
- [x] Verify the platform per shop (§1).
- [x] Body-only products exclude kits ("X-T5 + Lens XF 16-50mm"), so kit prices don't mix into the body's history (`kit_excludes` in `config/watchlist.yaml`).
- [x] Price-drop threshold: alert on every drop (`min_drop_pct: 0`, `min_drop_vnd: 0`).
