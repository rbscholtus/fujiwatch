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

| Shop | Site | Platform (observed / presumed) |
|---|---|---|
| fujifilm-xspace | fujifilm-xspace.vn | Haravan (verified) |
| vjshop | vjshop.vn | Next.js (verified) |
| kyma | kyma.vn | Custom (verified search page is server-rendered) |
| zshop | zshop.vn | presumed Magento-like (`.html` product URLs) — verify |
| mayanhhoangto | mayanhhoangto.com | presumed WordPress/WooCommerce — verify |
| bncamera | bncamera.com | WooCommerce (`?s=…&post_type=product`) — verify |
| mayanh24h | mayanh24h.com | unknown — verify |
| x-camera | x-camera.vn | presumed WooCommerce (`/shop/…`) — verify |
| giangduydat | giangduydat.vn | unknown — verify |

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

- **fujifilm-xspace (Haravan):** product pages expose `og:price:amount` (e.g. `27490000`) and `og:price:currency` = `VND`. Stock text on the page is "Còn hàng" (in stock) or "Hết hàng" (out of stock). Haravan, like Shopify, usually also serves product JSON at `/products/<handle>.js`. Verify that and prefer it if it works.
- **vjshop (Next.js):** the price is not in the visible server-rendered text. Look for `<script id="__NEXT_DATA__" type="application/json">` and walk the JSON for the price and variant fields. The site has a used-goods section ("Đồ cũ chất", `/do-cu-chat`). Product URLs carry the variant (e.g. `...-silver-chinh-hang`), while the canonical URL drops it.
- **kyma:** the search page is server-rendered, but its search is picky; a query like `XF 18-55mm f/2.8-4` returned 0 results. Watch category pages (e.g. `/ong-kinh-fujifilm`, `/may-anh-fujifilm`) or exact product URLs instead of search.
- **WooCommerce shops (bncamera, x-camera, probably mayanhhoangto):** try the Store API first: `GET /wp-json/wc/store/v1/products?search=<term>` returns clean JSON with prices (in minor units; check `currency_minor_unit`). It is often enabled by default. Fall back to HTML: `.product .price .woocommerce-Price-amount`.
- **zshop, mayanh24h, giangduydat:** inspect them, save a snapshot into `testdata/`, then write the adapter.

**Workflow for adding a shop**

1. `go run ./cmd/fujiwatch snapshot <url>` saves `testdata/<shop>/<slug>.html`.
2. Write the adapter, plus a golden test asserting the extracted offers from the snapshot.
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

`data/state.json`, keyed by `shop|url`:

```json
{
  "xspace|https://fujifilm-xspace.vn/products/...": {
    "product": "xf10-24-ii",
    "title": "Ống kính Fujinon XF10-24mm F4 R OIS WR II",
    "price_vnd": 27490000,
    "in_stock": true,
    "condition": "new",
    "first_seen": "2026-11-07T23:17:00Z",
    "last_seen": "2026-11-07T23:17:00Z"
  }
}
```

`data/history/<product-id>.jsonl` gets one line per offer per run:

```json
{"ts":"2026-11-07T23:17:00Z","shop":"xspace","url":"...","price_vnd":27490000,"in_stock":true,"condition":"new"}
```

**Events (`internal/diff`)**

- `price_drop` / `price_rise`: only alert on drops by default. Optional threshold (e.g. ≥ 2% or ≥ 300,000 VND) to ignore noise.
- `back_in_stock` / `out_of_stock`
- `new_listing`: the URL has not been seen before (listing mode)
- `gone`: not seen for 3 consecutive runs. Remove it from state and log it (useful for used items: "sold").
- `adapter_broken`: a source that previously returned ≥ 1 offer now returns 0, or the HTTP status is not 200. Always included in the email, so silent failures are visible.

---

## 8. Notifications

Notifier interface:

```go
type Notifier interface {
    Send(ctx context.Context, subject string, events []diff.Event) error
}
```

**WhatsApp via CallMeBot (instant, important events only: `price_drop`, `back_in_stock`, `new_listing`)**

- One-time setup: add CallMeBot's number to your phone contacts and send it "I allow callmebot to send me messages". It replies with your personal API key. The current number and steps are at https://www.callmebot.com/blog/free-api-whatsapp-messages/
- Send with `GET https://api.callmebot.com/whatsapp.php?phone=<+84…>&text=<urlencoded>&apikey=<key>`
- Text only, keep messages short, at most one message per run (batch the events). It is a free shared service, so treat failures as non-fatal.

**Email via SMTP (daily digest, every run)**

- Gmail SMTP: `smtp.gmail.com:587` with STARTTLS. Requires 2-Step Verification and an **app password** (Google Account → Security → App passwords).
- Contents: events at the top, then a table of the current best price per product (new vs used), unmatched/ambiguous offers, and adapter health.
- Option: send the email only when there are events or failures, plus a weekly "all OK" heartbeat.

**Instagram:** not supported. There is no practical API for a bot to DM a personal account.

---

## 9. CLI

```
fujiwatch run [--dry-run] [--only <shop>] [--no-notify]
fujiwatch snapshot <url>                 # save HTML to testdata/
fujiwatch extract <shop> <file.html>     # run one adapter on a saved file, print offers
fujiwatch match "<title>"                # show which product a title matches, and why
```

`--dry-run` fetches and diffs but does not write state or send notifications. Use it locally all the time.

---

## 10. GitHub Actions

`.github/workflows/watch.yml`:

```yaml
name: watch
on:
  schedule:
    - cron: "17 23 * * *"     # 06:17 Asia/Saigon (cron is UTC); off-the-hour to avoid queue delays
  workflow_dispatch: {}        # manual "Run workflow" button

permissions:
  contents: write
  pages: write
  id-token: write

concurrency:
  group: watch
  cancel-in-progress: false

jobs:
  scrape:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version-file: go.mod }
      - run: go test ./...
      - run: go run ./cmd/fujiwatch run
        env:
          CALLMEBOT_PHONE:  ${{ secrets.CALLMEBOT_PHONE }}
          CALLMEBOT_APIKEY: ${{ secrets.CALLMEBOT_APIKEY }}
          SMTP_USER:        ${{ secrets.SMTP_USER }}
          SMTP_PASS:        ${{ secrets.SMTP_PASS }}
          MAIL_TO:          ${{ secrets.MAIL_TO }}
      - name: Commit data
        run: |
          git config user.name  "fujiwatch-bot"
          git config user.email "fujiwatch-bot@users.noreply.github.com"
          git add data/
          git diff --cached --quiet || git commit -m "data: $(date -u +%F) [skip ci]"
          git push

  site:
    needs: scrape
    runs-on: ubuntu-latest
    environment: { name: github-pages, url: "${{ steps.deploy.outputs.page_url }}" }
    steps:
      - uses: actions/checkout@v4
        with: { ref: main }    # pick up the data commit
      - uses: actions/setup-node@v4
        with: { node-version: 22 }
      - run: npm ci && npm run build
        working-directory: site
      - uses: actions/upload-pages-artifact@v3
        with: { path: site/dist }
      - id: deploy
        uses: actions/deploy-pages@v4
```

Check the action versions against the GitHub Marketplace when setting this up; they may have newer major versions.

**Secrets** (repository → Settings → Secrets and variables → Actions): `CALLMEBOT_PHONE`, `CALLMEBOT_APIKEY`, `SMTP_USER`, `SMTP_PASS`, `MAIL_TO`.

**Things to know about GitHub Actions**

- Scheduled runs can start late (minutes, sometimes longer) at busy times. Fine for a daily job.
- In public repositories, scheduled workflows are disabled after 60 days without repository activity. The daily data commit should count as activity, but if the schedule ever stops, re-enable it in the Actions tab.
- **Public vs private repo:** GitHub Pages on a private repository requires a paid plan. A public repo is simplest: the watchlist and shop prices are not sensitive, and secrets stay secret either way. If you want the repo private on a free plan, drop the Pages job and rely on the email digest, or render the charts into the email.
- Runners are US datacenter machines. If a shop blocks datacenter traffic (e.g. Cloudflare challenges), that shop's adapter will report `adapter_broken`, and we decide case by case.

---

## 11. Price-history site (`site/`)

- Svelte + Vite, built to static files.
- At build time, a small script (or a Vite plugin) reads `../data/history/*.jsonl` and emits `public/data/<product-id>.json`, aggregated per day and shop, with new and used as separate series.
- One page:
  - a product selector
  - a line chart per product, one line per shop, with markers for used listings
  - a table of current best prices with links to the shop pages
- Charting: a small library such as Chart.js or uPlot, or hand-rolled SVG. Keep the bundle small.
- No backend and no runtime API calls. Everything is baked in at build time.

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
cd site && npm install && npm run dev
```

For local secrets, use a `.env` file (git-ignored) that is loaded only when present.

---

## 14. Milestones

1. **Skeleton:** config loading, fetch client, VND price parser, normaliser and matcher with table-driven tests (X-T3 vs X-T30, X-T5 vs X-T50, XF10-24 Mark I vs Mark II, Tamron X vs E-mount).
2. **First two shops:** fujifilm-xspace (product mode) and vjshop (listing mode, used section), with golden tests from snapshots.
3. **State, history and diff:** events, `--dry-run` output.
4. **Notifications:** email digest, then CallMeBot WhatsApp.
5. **GitHub Actions:** schedule, data commit, secrets.
6. **Remaining shops:** kyma, then the WooCommerce shops (try the Store API), then zshop, mayanh24h, giangduydat.
7. **History site:** Svelte page on GitHub Pages.
8. **Nice-to-haves:** a per-product target price ("alert below X VND"), a weekly summary, more lenses.

---

## 15. Open items

- [ ] Collect the real product, category and used-section URLs per shop for the watched items.
- [x] Repo visibility: **public** (GitHub Pages works on the free plan).
- [ ] Decide the price-drop alert threshold.
- [ ] Verify the platform per shop (rows marked "verify" in §1).
