# fujiwatch

A small price watcher for Fujifilm X-mount cameras and lenses in Vietnamese camera shops.

It runs once a day on GitHub Actions. Each run checks a list of shop pages, records prices, and sends an alert (WhatsApp and/or email) when:

- a watched product drops in price, reaches a target price or comes back in stock (**new gear**), or
- a new matching listing appears on a shop's used or category page (**used gear**).

A static price-history site, built with Svelte and published on GitHub Pages, shows a chart per product over time.

**Documentation**

- [Usage](docs/usage.md): configuration files, CLI, notifications, GitHub Actions, local development, and step-by-step guides for adding a product, a page or a shop.
- [Architecture](docs/architecture.md): pipeline, repository layout, adapters, matching, state and events, the history site.
- [Shop notes](docs/shops.md): how each shop is read, and its quirks.

---

## Goals and scope

**Watched products** (`config/watchlist.yaml`)

| ID | Product | Notes |
|---|---|---|
| `x-t3` | Fujifilm X-T3 body | new + used; must not match X-T30 / X-T30 II / X-T30 III |
| `x-t4` | Fujifilm X-T4 body | new + used |
| `x-t5` | Fujifilm X-T5 body | new + used; must not match X-T50 |
| `x-t6` | Fujifilm X-T6 body | new + used; expected soon, not in shops yet |
| `x-e5` | Fujifilm X-E5 body | new + used |
| `xf10-24-i` | XF10-24mm F4 R OIS (Mark I) | tracked separately from Mark II (different prices) |
| `xf10-24-ii` | XF10-24mm F4 R OIS WR (Mark II) | Mark II is the WR version |
| `xf16-55` | XF16-55mm F2.8 R LM WR (Mark I) | tracked separately from the WR II |
| `xf16-55-ii` | XF16-55mm F2.8 R LM WR II | the 2025 model |
| `xf18-55` | XF18-55mm F2.8-4 R LM OIS | reject Canon / Nikon / Sony 18-55 kit zooms |
| `tamron-16-50` | Tamron 16-50mm F2.8 Di III VXD | **X-mount only**; reject Sony E / Nikon Z / Canon RF variants |
| `sigma-10-18` | Sigma 10-18mm F2.8 DC DN Contemporary | X-mount; dual-mount listings ("for Fujifilm/Sony") count |
| `sigma-18-50` | Sigma 18-50mm F2.8 DC DN Contemporary | X-mount; dual-mount listings count |

Body products never match kits (a title with "kit", a focal length or "+ lens"), and lens products never match a title that names a camera body, so kit prices stay out of both histories. Adding a product only requires editing `config/watchlist.yaml` (see [Usage: adding a product](docs/usage.md#adding-a-product)).

**Shops** (`config/sources.yaml`)

| Shop | Site | Platform | How it is read |
|---|---|---|---|
| xspace | fujifilm-xspace.vn | Haravan | `/products/<handle>.js` JSON |
| vjshop | vjshop.vn | Next.js | `__NEXT_DATA__` (listings, every page), `api.vjshop.vn` (products) |
| kyma | kyma.vn | Custom (ASP.NET) | CSS selectors on category pages, plus its POST "show more" endpoint |
| zshop | zshop.vn | CS-Cart | CSS selectors (category and product pages, every page) |
| mayanhhoangto | mayanhhoangto.com | WooCommerce | Store API (every page) |
| bncamera | bncamera.com | WooCommerce | Store API (every page) |
| mayanh24h | mayanh24h.com | Custom (AngularJS front, server-rendered lists) | CSS selectors; page 1 of used listings, product pages for new gear (robots.txt) |
| x-camera | x-camera.vn | WooCommerce | Store API (every page) |
| giangduydat | giangduydat.vn | Custom | CSS selectors; product pages list each unit as a schema.org Offer, categories for discovery only |

**Out of scope:** Shopee, Lazada, Tiki, Chợ Tốt (scam risk), Facebook Marketplace and groups.

---

## Quick start

```bash
go test ./...
go run ./cmd/fujiwatch run --dry-run --no-notify --only vjshop   # fetch and diff one shop, write nothing
go run ./cmd/fujiwatch match "Máy ảnh Fujifilm X-T5 | Chính hãng" # which product does a title match?
```

The real run happens on GitHub Actions; see [Usage: GitHub Actions](docs/usage.md#github-actions).

---

## Milestones

All done; each was committed with spec tests that pass (`go test ./...`).

1. ✅ **Skeleton:** config loading, fetch client, VND price parser, normaliser and matcher with table-driven tests (X-T3 vs X-T30, X-T5 vs X-T50, XF10-24 Mark I vs Mark II, Tamron X vs E-mount).
2. ✅ **First two shops:** fujifilm-xspace (product mode) and vjshop (listing mode, used section), with golden tests from snapshots.
3. ✅ **State, history and diff:** events, `--dry-run` output.
4. ✅ **Notifications:** email digest, then CallMeBot WhatsApp.
5. ✅ **GitHub Actions:** schedule, data commit, secrets.
6. ✅ **Remaining shops:** kyma, then the WooCommerce shops (Store API), then zshop, mayanh24h, giangduydat.
7. ✅ **History site:** Svelte page on GitHub Pages.
8. ✅ **Nice-to-haves:** a per-product target price ("alert below X VND"), a weekly summary.

## Open items

- [x] Collect the product, category and used-section URLs per shop for the watched products (see [shop notes](docs/shops.md)).
- [x] Repo visibility: **public** (GitHub Pages works on the free plan).
- [x] Verify the platform per shop.
- [x] Body-only products exclude kits ("X-T5 + Lens XF 16-50mm"), so kit prices don't mix into the body's history (`kit_excludes` in `config/watchlist.yaml`).
- [x] Price-drop threshold: alert on every drop (`min_drop_pct: 0`, `min_drop_vnd: 0`).
- [x] Multi-page listings are followed (vjshop, WooCommerce, kyma, zshop, giangduydat). mayanh24h: robots.txt allows page 1 only.
- [ ] bncamera flash sales: report watched products in upcoming flash-sale slots (next 3 days) as their own event, by email and WhatsApp, without recording the 2-hour flash price as a regular price. Findings in [shop notes](docs/shops.md#woocommerce-bncamera-mayanhhoangto-x-camera).
- [ ] Set target prices (`target_vnd`, `target_used_vnd`) per product.
- [ ] vjshop: add the Tamron 16-50mm (X) product page once vjshop lists it.
- [ ] vjshop: recheck the XF18-55 (product 4898). It is out of stock; its listing shows 8.890.000₫, but the product API has only `default: 0`, so the listing price may be stale.

## License

BSD 3-Clause; see [LICENSE](LICENSE).
