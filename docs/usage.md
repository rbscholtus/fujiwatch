# Usage

How to configure, run and extend fujiwatch. For how it works inside, see [architecture.md](architecture.md); for each shop's quirks, see [shops.md](shops.md).

- [Configuration](#configuration)
- [CLI](#cli)
- [Notifications](#notifications)
- [GitHub Actions](#github-actions)
- [Local development](#local-development)
- [Adding a product](#adding-a-product)
- [Adding a page to watch](#adding-a-page-to-watch)
- [Adding a shop](#adding-a-shop)

---

## Configuration

Two files in `config/`. Both are validated on load; an error names the entry (`sources[3]: unknown product "x-t6"`).

### `config/watchlist.yaml`

The products to watch, and the rules that decide whether a shop's title is that product.

```yaml
# Shared lists, referenced with YAML anchors.
kit_excludes: &kit ['\bkit\b', '\d+(-\d+)? ?mm\b', '\+ ?(lens|ong kinh)\b']
body_excludes: &body ['\bx-(t|s|h|e|m|a|pro)\d']
other_mounts: &mounts ['sony', '\be-?mount\b', 'canon', '\brf(-s)?\b', 'nikon', '\bl-?mount\b']   # shortened

products:
  - id: x-t5                      # stable ID: state, history files, site URLs
    name: "Fujifilm X-T5"         # shown in emails and on the site
    kind: body                    # body | lens
    patterns: ['\bx-t5(\D|$)']    # never matches X-T50
    exclude: *kit                 # body only: no kits

  - id: xf10-24-ii
    name: "XF10-24mm F4 R OIS WR (II)"
    kind: lens
    patterns: ['(^|\D)10-24(\D|$)']
    require_all: ['\bwr\b|\bii\b|mark ?ii']
    exclude: *body                # lens only: no "X-T5 + XF10-24" kits

  - id: tamron-16-50
    name: "Tamron 16-50mm F2.8 Di III VXD (X)"
    kind: lens
    patterns: ['tamron', '(^|\D)16-50(\D|$)']
    require_any: ['fuji', 'fujifilm', 'x-?mount', 'ngam x', '\bfx\b']
    exclude: ['sony', '\bfe\b', '\be-?mount', 'nikon', '\bz\b', 'canon', '\brf\b']
    other_mounts: *mounts         # optional: see below
    target_vnd: 15000000          # optional: below_target alert for new offers
    target_used_vnd: 11000000     # optional: the same for used offers
```

Rules are RE2 regular expressions matched against the **normalised** title (lowercase, no diacritics, `XT5` / `X T5` → `x-t5`, `f/2.8` → `f2.8`; see [architecture.md: matching](architecture.md#matching-and-normalisation)):

- all `patterns` and all `require_all` must match,
- at least one `require_any` must match (if set),
- no `exclude` may match.

A title that matches zero or several products is never alerted on. Ambiguous titles, and near misses (all `patterns` match, no `exclude` does, but a `require_all` / `require_any` fails), are listed in the email so the rules can be tuned.

`other_mounts` is for third-party lenses made for several mounts (Sigma, Tamron). It never stops a match: a dual-mount listing ("for Sony E/Fujifilm") still matches through `require_any`. It only decides that a title naming another mount and not Fujifilm ("Sigma 18-50mm … For Sony E") is that mount's version, so it is not listed as a near miss. Use `exclude` instead when even dual-mount titles must not match. `fujiwatch match` shows near misses.

> Go's `regexp` (RE2) has **no** lookahead `(?!…)`, so model boundaries use a negated class: `(\D|$)` means "followed by a non-digit or the end". Focal ranges use `(^|\D)…(\D|$)` instead of `\b`, because there is no word boundary inside "xf10-24", and "110-240" must not match "10-24". Prefer this over `exclude: ['x-?t30']`: an exclude rejects the whole title, which would wrongly drop "X-T3 kèm grip, dùng chung pin với X-T30". Keep `exclude` for real confusions (other mounts, brands, kits, a newer Mark).

### `config/sources.yaml`

The pages to fetch, plus run-wide settings.

```yaml
defaults:
  delay_ms: 3000        # between requests to the same host
  timeout_s: 20
  # Alert on a price drop of at least min_drop_pct percent OR min_drop_vnd.
  # Both 0: alert on every drop.
  min_drop_pct: 0
  min_drop_vnd: 0

notify:
  email: always         # always | changes (see Notifications)
  heartbeat: sunday     # with email: changes, an "all OK" email on this weekday
  weekly: sunday        # weekly summary in the email on this weekday; remove to disable

sources:
  - shop: xspace
    mode: product                 # one fixed product page
    product: xf10-24-ii           # must be a watchlist ID
    url: https://fujifilm-xspace.vn/products/fujinon-xf10-24mmf4-r-ois-wr-ii

  - shop: vjshop
    mode: listing                 # category / search / used page; every title goes through the matcher
    condition: used               # new | used | auto
    url: https://vjshop.vn/may-anh-ong-kinh-cu-fujifilm
```

- `shop` selects the adapter (the names in the [README shops table](../README.md#goals-and-scope)).
- `mode: product` tracks one product's price and stock. `mode: listing` matches every item on the page; for shops whose adapter supports it (vjshop), every page of the listing is read.
- `condition` defaults to `new` for product mode and `auto` for listing mode. `auto` detects used items from the title, URL or the shop's own data (see [architecture.md: condition detection](architecture.md#matching-and-normalisation)).
- Weekdays (`heartbeat`, `weekly`) are in Vietnam time.
- Removing a source is safe: its offers age out and are reported `gone` after 3 runs.

---

## CLI

```
fujiwatch run      [--config <dir>] [--data <dir>] [--dry-run] [--only <shop>] [--no-notify]
fujiwatch snapshot [--config <dir>] [--out <dir>] [--shop <shop>] [--mode product|listing] [--no-rewrite] <url>
fujiwatch extract  [--config <dir>] [--url <page-url>] <shop> <file>
fujiwatch match    [--config <dir>] "<title>" [url]
fujiwatch sitedata [--config <dir>] [--data <dir>] [--out <dir>]
```

| Command | What it does |
|---|---|
| `run` | One watch cycle: fetch every source, match, diff against `data/state.json`, append history, notify, save state. |
| `snapshot` | Fetch a page and save it under `testdata/<shop>/<slug>.html` (or `.json`) plus `<slug>.meta.json`, for adapter tests. |
| `extract` | Run one shop's adapter on a saved file and print the offers. |
| `match` | Show which product a title matches, rule by rule, whether it is a near miss, and the detected condition. |
| `sitedata` | Build the history site's JSON from `data/` into `site/public/data/`. |

Flags:

- `--config <dir>`: directory with `watchlist.yaml` and `sources.yaml` (default `config`).
- `--data <dir>`: directory with `state.json` and `history/` (default `data`).
- `--dry-run` (run): fetch and diff, but write no state or history and send no notifications. **Use it for every local run.**
- `--no-notify` (run): don't send notifications.
- `--only <shop>` (run): only run that shop's sources. Other sources' state is left untouched.
- `--out <dir>`: snapshot directory (default `testdata`), or the site data directory for `sitedata` (default `site/public/data`).
- `--shop <shop>`, `--mode product|listing` (snapshot): override what is looked up from `sources.yaml` by URL or host.
- `--no-rewrite` (snapshot): save the page itself, not the endpoint the adapter would fetch (such as a JSON API).
- `--url <page-url>` (extract): the page URL the file was fetched for (default: from its `.meta.json`).

---

## Notifications

Notifiers are enabled by their environment variables: repository secrets on GitHub Actions, or a local `.env` file (see [`.env.example`](../.env.example)). A notifier with only some of its variables set is skipped with a warning. Notification failures never fail the run (the data must still be committed); they are printed as warnings, and as `::warning::` annotations on GitHub Actions.

**WhatsApp via CallMeBot (instant, important events only: `below_target`, `price_drop`, `back_in_stock`, `new_listing`)**

When one listing has several of these in a run (e.g. `new_listing` and `below_target`), only the most important one is sent, in that order. At most one message per run, with the events batched.

Events from a page's **first** successful run (the first run after launch, or a page just added to `sources.yaml`) are a baseline: they are in the email, with a note, but never sent to WhatsApp.

- One-time setup: add CallMeBot's number to your phone contacts and send it "I allow callmebot to send me messages". It replies with your personal API key. The current number and steps are at https://www.callmebot.com/blog/free-api-whatsapp-messages/
- Variables: `CALLMEBOT_PHONE` (`+84…`), `CALLMEBOT_APIKEY`.
- It is a free shared service, so failures are non-fatal.

**Email via SMTP (daily digest)**

- Gmail SMTP: `smtp.gmail.com:587` with STARTTLS. Requires 2-Step Verification and an **app password** (Google Account → Security → App passwords).
- Variables: `SMTP_USER`, `SMTP_PASS`, `MAIL_TO` (several addresses comma-separated). Optional: `MAIL_FROM` (default `SMTP_USER`), `SMTP_ADDR` (default `smtp.gmail.com:587`). The password is only sent after STARTTLS.
- Contents: events at the top, then a table of the current best price per product (new vs used), source health, and offers that need a look:
  - **mismatch**: a product page whose title doesn't match its configured product;
  - **ambiguous**: a title that matches several products;
  - **near miss**: a title that names a watched product but fails its `require_all` / `require_any` rules (mount or version not confirmed), e.g. "Tamron 16-50mm F2.8 Di III VXD" without a mount;
  - every other unmatched offer is only counted, per shop ("Other unmatched offers: 280 (vjshop 250, kyma 30)"). `fujiwatch run --dry-run` lists them all.
- `notify: email: changes` sends the email only when there are events or failures, plus a weekly "all OK" heartbeat on `heartbeat: <weekday>`. Default `email: always`.
- `notify: weekly: <weekday>`: on that day the email always goes out, with the subject suffix "· weekly summary" and a table of each product's lowest new and used price over the last 7 days against the 7 days before.

**Instagram:** not supported. There is no practical API for a bot to DM a personal account.

---

## GitHub Actions

Two workflows:

- [`.github/workflows/watch.yml`](../.github/workflows/watch.yml): the daily run. `cron: "17 23 * * *"` is 06:17 Asia/Saigon (cron is UTC), off the hour to avoid queue delays, plus a manual **Run workflow** button with optional `dry_run` and `only` inputs. The `scrape` job runs `go test ./...`, then `fujiwatch run`, then commits `data/` as `data: <date> [skip ci]` (only when it changed). The `site` job builds and deploys the history site.
- [`.github/workflows/ci.yml`](../.github/workflows/ci.yml): `go vet`, `go test -race` and `golangci-lint` on pushes and pull requests (data-only commits are ignored).

Actions are pinned to their current major versions (`actions/checkout@v7`, `actions/setup-go@v7`, …); bump them when GitHub releases new majors. `cmd/fujiwatch/workflow_test.go` checks the workflows against this section and executes their shell steps.

**Secrets** (repository → Settings → Secrets and variables → Actions): `CALLMEBOT_PHONE`, `CALLMEBOT_APIKEY`, `SMTP_USER`, `SMTP_PASS`, `MAIL_TO`.

**Things to know about GitHub Actions**

- Scheduled runs can start late (minutes, sometimes longer) at busy times. Fine for a daily job.
- In public repositories, scheduled workflows are disabled after 60 days without repository activity. The daily data commit should count as activity, but if the schedule ever stops, re-enable it in the Actions tab.
- **Public vs private repo:** GitHub Pages on a private repository requires a paid plan. The repo is public: the watchlist and shop prices are not sensitive, and secrets stay secret either way.
- Runners are US datacenter machines. If a shop blocks datacenter traffic (e.g. Cloudflare challenges), that shop's sources report `adapter_broken`, and we decide case by case.

---

## Local development

```bash
go test ./...

# iterate on one shop
go run ./cmd/fujiwatch snapshot "https://fujifilm-xspace.vn/products/fujinon-xf10-24mmf4-r-ois-wr-ii"
go run ./cmd/fujiwatch extract xspace testdata/xspace/fujinon-xf10-24mmf4-r-ois-wr-ii.json
go run ./cmd/fujiwatch match "Ống kính Fujinon XF10-24mm F4 R OIS WR II"

# full run without side effects
go run ./cmd/fujiwatch run --dry-run --no-notify
go run ./cmd/fujiwatch run --dry-run --no-notify --only vjshop

# site
go run ./cmd/fujiwatch sitedata          # data/ → site/public/data/
cd site && npm install && npm run dev
```

For local secrets, use a `.env` file (git-ignored). It is loaded only when present, and variables already set in the environment win.

---

## Adding a product

1. Add an entry to `config/watchlist.yaml`. Use `exclude: *kit` for a body and `exclude: *body` for a lens. If an older or newer Mark of the same lens exists, add both and keep them apart (`require_all` on one, `exclude` on the other), as for the XF10-24 and XF16-55.
2. Check the rules against real shop titles, including kits and other mounts, which must **not** match:
   ```bash
   go run ./cmd/fujiwatch match "Ống kính Fujifilm XF 16-55mm f/2.8 R LM WR | Chính hãng"
   ```
3. Run `go run ./cmd/fujiwatch run --dry-run --no-notify` and look at the new matches and the unmatched list.
4. Add the product to the table in the [README](../README.md#goals-and-scope) (a test checks this).
5. `go test ./...`. Tests over saved snapshots count matches, so a new product can change expected counts there.

No code changes are needed.

## Adding a page to watch

1. Check the shop's `robots.txt`: the path must not be disallowed (the run refuses, and reports `adapter_broken`). Per-shop rules are in [shops.md](shops.md).
2. Choose the mode: `listing` for categories, searches and used sections; `product` for one product page (with `product: <watchlist ID>`).
3. Use the URL in the form the shop's listings report, so one product seen by a listing and by its product page is one offer (vjshop: `https://vjshop.vn/<slug>`, without the category path).
4. Add it to `config/sources.yaml` and run `go run ./cmd/fujiwatch run --dry-run --no-notify --only <shop>`. Check the offer count: if it equals a page size (16, 30, 36, 40, 100), the listing may have more pages than are being read.
5. For a product page, the dry-run report shows no price; to see what is recorded, run with `--data <scratch dir> --no-notify` and read `<scratch dir>/state.json`. Never point `--data` at `data/`.
6. When one URL is found by two sources in a run, the first source in `sources.yaml` wins: list the more reliable source (a product page) first. A product page behind a listing that already finds its URL adds nothing.
7. Add a product page only where the listing falls short: robots.txt limits it (mayanh24h), its prices are unreliable (giangduydat), it shows no stock or drops sold-out products (kyma), or there is no listing (xspace). A product page the shop deletes fails every run (`adapter_broken`) until it is removed.

## Adding a shop

1. Find out how the shop serves prices: a JSON API, embedded JSON, structured data (JSON-LD, Open Graph, microdata) or only HTML. The platform often decides it (Haravan, WooCommerce, …); see [architecture.md: shop adapters](architecture.md#shop-adapters).
2. `go run ./cmd/fujiwatch snapshot --shop <shop> <url>` saves `testdata/<shop>/<slug>.html` (or `.json` when the adapter fetches a JSON endpoint) plus `<slug>.meta.json` (page URL, fetch URL, mode, fetch time). Snapshot a category page and a product page.
3. Write the adapter (or a `Layout` for a CSS-selector shop) and register it in `internal/adapters/adapter.go`. `go test ./internal/adapters -update` writes `<slug>.golden.json` for every snapshot; review it, commit it, and add explicit assertions for the important fields.
4. Add its pages to `config/sources.yaml`, and the shop to the [README shops table](../README.md#goals-and-scope) and to [shops.md](shops.md) (tests check both).
