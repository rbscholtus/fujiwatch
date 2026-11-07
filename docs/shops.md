# Shop notes

How each shop is read, what it allows, and its quirks. One section per shop (a test checks that every shop with an adapter has one). The adapter kinds are described in [architecture.md: shop adapters](architecture.md#shop-adapters); how to add a shop is in [usage.md](usage.md#adding-a-shop).

When a shop changes its site, or a new quirk turns up, note it here.

---

## xspace

fujifilm-xspace.vn, Fujifilm's own store (Haravan; new gear only). Adapter: `haravan.go`.

- Product mode fetches the product JSON at `/products/<handle>.js`: prices in VND × 100, with `available` per variant.
- **Variants:** a camera page has variants for the body and each kit ("Body / Đen / …", "XF16-50mm Kit / Bạc / …"), with their own price and stock. The adapter yields one offer per variant, titled "<product> (<variant>)", and the runner uses the cheapest in-stock variant that matches the product, so a kit never stands in for an out-of-stock body.
- Product handles are in `/sitemap_products_1.xml`. It sells no XF18-55 on its own (kit lens) and no third-party lenses.
- Fallback for HTML product pages: `og:price:amount` (e.g. `27490000`) and `og:price:currency` = `VND`. Stock text is in `.checkProduct`: "Còn hàng" (in stock) or "Hết hàng" (out of stock). Related-product cards elsewhere on the page also say "Hết hàng", so don't search the whole page.
- `robots.txt` disallows `/search`.

## vjshop

vjshop.vn, Next.js. Adapter: `vjshop.go`.

- **Listings** embed the product list in `<script id="__NEXT_DATA__" type="application/json">` at `props.pageProps.products` (`core.name`, `core.slug`, `real_price`, `core.status.raw`, `core.product_category.slug`, `core.condition`). The price is not in the visible server-rendered text.
- **Pages:** 36 items per page; a `per_page` parameter is ignored. `props.pageProps.meta` gives `current_page`, `last_page` and `total`, and the adapter follows `?page=N` to the last page. `meta.total` = 0 marks a genuinely empty listing.
- **Product pages** are rendered client-side and carry **no** product data, so product mode fetches `https://api.vjshop.vn/api/products/<slug>` instead. That response has no `real_price`, only `prices.{default,sale,flashsale}` with priorities and optional `from`/`to` windows (Vietnam time), so the adapter picks the highest-priority price active now. An out-of-stock product can have only `default: 0` there while its listing still shows a `real_price`.
- **URLs:** the site links products under a category path (`/ong-kinh-mirrorless/<slug>`), but listings report `https://vjshop.vn/<slug>`. Configure product pages in the short form, so the listing and the product page are one offer. Some products have alias slugs (`xf-18-55mm-…` and `fujifilm-xf-18-55mm-…` are product 4898); the API returns the canonical `core.slug`. Product URLs carry the variant (e.g. `...-silver-chinh-hang`).
- **Used goods** ("Đồ cũ chất", `/do-cu-chat`) has per-brand sub-categories: `/may-anh-ong-kinh-cu-fujifilm`, `-sigma`, `-tamron`. `robots.txt` disallows `/may-anh-cu`, `/ong-kinh-cu` and a few other `*-cu` paths, so don't watch those. Used items are in `*-cu` categories and carry a condition grade; the adapter marks them used.
- **Promotions** (`/khuyen-mai`) use the listing layout and accept a brand filter: `?brands=3,11,12` (brand IDs: Fujifilm 3, Sigma 11, Tamron 12; the IDs are in `props.pageProps.brands`). Unfiltered it is ~550 items over 16 pages.
- Lens categories by brand: `/ong-kinh-mirrorless-fujifilm`, `-sigma` (all mounts), `-tamron`.
- Unknown slugs return HTTP 200 with an empty page, so a wrong URL shows up as `adapter_broken`, not as a 404.

## kyma

kyma.vn, custom platform (ASP.NET). Adapter: `kyma.go` (CSS layouts plus "show more").

- Category cards are `li.li-product`; the first `span` in `.item__price` is the current price (the second is the struck-through one). No stock labels on category pages.
- **Pages:** a category page shows 30 items and a "Xem thêm N sản phẩm" (show N more) button. The button POSTs `categoryId`, `paging`, `pageSize`, `sortBy`, `priceMin`, `priceMax` (hidden `#categoryId`, `#sortBy`, `#priceMinMore`, `#priceMaxMore` on the page) to `/Product/ShowMoreNextProCate`, which answers `{"list": "<li>…</li>", "paging": n, "remainingProducts": n}`. The adapter sends one POST with `paging=0&pageSize=300`, which returns the whole category (`remainingProducts` is then negative). The endpoint refuses GET, and GET parameters on the category page are ignored. Its cards are `.item__product` with the title in `a.item__name h3`.
- Search (`/tim-kiem.html?k=`) is useless: it returns the same unrelated "new products" for any query. Watch categories: `/may-anh-mirrorless-fujifilm` (bodies), `/ong-kinh-fujifilm`, `/ong-kinh-sigma`, `/ong-kinh-tamron`. No used section.
- Colour variants are separate cards with `?product-id=` URLs.
- **Stock:** category cards show none, and **sold-out products are dropped from the categories** (the sold-out X-T30 II is not in the full list of 70). A watched product would vanish, go "gone" after 3 runs, and return as a `new_listing`. Product pages stay up when sold out and carry JSON-LD with `availability` (`InStock` / `OutOfStock`, price 0 and "HẾT HÀNG" when sold out); the adapter reads them through the generic extractor. So the watched products' pages (each colour has its own `?product-id=` URL) are sources of their own, listed before the categories so they win the shared URL.
- Responses are sometimes slow (timeouts after 20 s, twice in a row on 2026-11-07); a failed product page leaves the offer to the category for that run.

## zshop

zshop.vn, CS-Cart (not Magento). CSS layouts in `adapter.go`.

- Category cards `.ty-grid-list__item`, price `.ty-price .ty-price-num`, out-of-stock label `.ty-qty-out-of-stock`. 40 per page; the next page is `a.ty-pagination__next` (`/<category>/page-N/`), absent on the last page.
- Categories: `/may-anh-mirrorless-fujifilm/`, `/lens-fujifilm/`, `/lens-sigma/`, `/lens-tamron/`. Used ("Kho hàng cũ", `/kho-hang-cu/`, ~15 pages of everything) has Fujifilm sub-categories `/body-fujifilm-cu/` and `/lens-fujifilm-hang-cu/`; used titles end in "- Likenew".
- Product pages: `h1.ty-product-block-title`, price in `.ty-product-block__price-actual`.
- Upcoming products have no price (→ 0). The cine "MK X 18-55mm T2.9" is excluded from the XF18-55.
- **Stock:** no category card carried the out-of-stock label (107 cards checked), and discontinued products' pages are deleted (404) or redirected (301): the new XF16-55 Mark I, X-T3, X-T4. A product page would then fail daily rather than report "out of stock", so only categories are watched; a sold-out product goes "gone" after 3 runs.

## mayanh24h

mayanh24h.com, custom platform (AngularJS front, server-rendered lists). CSS layouts in `adapter.go`.

- `robots.txt` disallows every URL with a query string (`Disallow: *?*`), and listings page with `?page=N`, so **only page 1 (16 items) of a listing** can be read; Fujifilm mirrorless alone has 6 pages. Used listings are watched on page 1; new gear through product pages.
- Product URLs are in the sitemaps: `/sitemap.xml` is an index of `/sitemap/product/sitemap1.xml` … `sitemap6.xml` (~6,000 products). Many old pages are still listed: discontinued products show no price, and some pages are single used units ("Mới 95%"). Some slugs without a mount are for another mount (`sigma-10-18mm-dc-dn.html` is Sony); product mode reports those as `mismatch`.
- Cards `.product-item` with a `.stock-status` label; used items are graded in the title ("Mới 98%" = 98% condition).
- Product pages: `h1.text_1`, price `h2.text-primary`, stock after "Tình trạng:" (missing on some pages: stock unknown).
- Responses were slow once (a run of 16 pages took over 6 minutes); single requests normally take under a second.

## giangduydat

giangduydat.vn, custom platform. CSS layouts in `adapter.go`.

- A product page lists every unit (new, used 85%, 90%, …) as its own schema.org `Offer` with a name and price, so product pages are watched in **listing** mode. Each unit gets the page URL plus a `#<slug of its name>` fragment as identity (a page with one unit: the page URL).
- **Category cards carry no usable price:** "Từ 4.490.000 đ" (from) is the lowest of the product's units, new or used, and a single used unit is shown under the plain product name ("Fujifilm X-T3, 22.500.000 đ" is a 98% used body). The category layout therefore reads no price; categories only discover products, and each watched product's page is a source of its own, listed before the categories so it wins when both find the same URL.
- Category cards `li.idx-prod`; 40 per page, the next page is the "tiếp" (next) link in `#page` (`<category>_pN`), plain text on the last page.
- Categories: `/c/may-anh-mirrorless` (all brands, ~6 pages), `/c/ong-kinh-fujifilm`, `/c/ong-kinh-sigma`, `/c/ong-kinh-tamron`, `/c/ong-kinh-thanh-ly` (clearance), `/c/may-anh-cu` (used).

## WooCommerce: bncamera, mayanhhoangto, x-camera

bncamera.com, mayanhhoangto.com, x-camera.vn. Adapter: `woocommerce.go`.

- The Store API, `GET /wp-json/wc/store/v1/products`, returns clean JSON with prices and stock. The adapter rewrites shop URLs: product page → `?slug=<slug>`, search page `?s=<term>` → `?search=<term>`, category page → `?category=<last path segment>` (an unknown slug returns `[]`), always with `per_page=100`.
- **Pages:** 100 per page at most. A full page is followed by `&page=N+1`; a page past the last one is `[]` (HTTP 200). The response headers `X-WP-Total` and `X-WP-TotalPages` give the totals.
- **Variable products:** a `type: variable` product with a `price_range` reports its cheapest variation as its price, and the variations mix new and used ("ỐNG KÍNH FUJIFILM 10-24mm f/4 R OIS WR": 10M used … 22.5M new), body and kits (x-camera X-T5: 38.49M body … 51.49M with XF16-80) and add-ons (mayanhhoangto X-E5: 33M body, 9M for "Len fuji 23 2.8 mua cùng", the XF23 bought with it). So such a product is not an offer itself: after each listing page, one request `?type=variation&parent=<ids>&per_page=100` fetches the variations of all its variable products, and each variation is an offer titled "<product> (<label>)" (label: `variation`, e.g. "Phân loại: Hàng mới nhập khẩu"), with its own permalink (`?attribute_pa_…=`), price and stock. The matcher then rejects kits and add-ons, and the label sets the condition. A product page (product mode) fetches its variations the same way.
- **mayanhhoangto's labels:** "Hàng mới chính hãng" (new, official), "Hàng mới nhập khẩu" (new, import), "Hàng đẹp" / "Hàng khá đẹp" / "Khá đẹp 90%" (used, good / fairly good condition), "Cũ hoạt động tốt" (used). A label with "đẹp" and without "mới" is read as used.
- **bncamera flash sales** (`mode: flash`, adapter `bnflash.go`): the homepage runs a "BNCamera Flash Sale" (plugin `bn-flash-sale`) in three daily slots, 10–12h, 12–14h and 20–22h, with tabs for today and the next two days; slots are filled at least a day ahead (5 products each on 2026-11-07). For some products the flash price exists only in the flash sale: on 2026-11-07, Sony A7V 58M vs 67.99M in the Store API, X-S20 body 29M vs 32.5M (others were equal). The homepage holds the schedule: `section.bnfs-section` (`data-bnfs-campaign`), day tabs `data-day-id="day_auto_YYYYMMDD"` (Vietnam date), slot tabs `data-timeslot-id` with `data-start` / `data-end` (Unix), the "all" category tab (`data-category="all"` `data-tab-id`), and `bnfsPublic` (`nonce`, `serverTime`). Each slot that has not ended by `serverTime` is a POST to `/wp-admin/admin-ajax.php` (allowed by robots.txt): `action=bnfs_get_products`, `nonce`, `campaign_id`, `day_id`, `timeslot_id`, `tab_id`, `limit=0`, `ui=`. The answer is `{"success": true, "data": {"html": …, "countdown": {"start", "end", "status"}, "count"}}`; its cards are `article.bnfs-product-card`: title in `.bnfs-product-card__title a` (product name only; the variant is in the link's `attribute_pa_…` parameters, which the adapter appends to the title), `.bnfs-product-card__sale-price`, the regular price in `del`, and "Đã bán 0/7 suất" (sold 0 of 7 units on offer). A flash price is valid for 2 hours, so it is never recorded as a price.
- **Discounts:** the Store API `price` is already the sale price (`regular_price` is the price before it), so discounts are recorded without a separate page. mayanhhoangto has no sale page; its homepage only shows category sections, and `?on_sale=true` matches about 1,100 products, nearly the whole catalogue.
- A category includes its sub-categories (bncamera `do-cu` = used cameras + used lenses + used drones). Category slugs, counts and permalinks: `/wp-json/wc/store/v1/products/categories?per_page=100`.
- Watch categories, not searches: `?s=fujifilm` also matches accessories (385 results at bncamera), and at x-camera it includes rentals ("Thuê máy ảnh FUJIFILM X-T4"), which would match a body at a rental price.
- Per shop: bncamera `/danh-muc/…` (Fujifilm bodies and lenses, Sigma, Tamron, `do-cu` used); x-camera (Fujifilm-focused) `/san-pham/may-anh/mirroless/`, `/san-pham/ong-kinh/ong-kinh-mirroless/`, `/san-pham/do-cu/` (used bodies only); mayanhhoangto Fujifilm bodies and lenses, and `ong-kinh-tamron-sigma-tokina-viltrox`; no used section.
- Prices are strings in minor units (check `currency_minor_unit`); names contain HTML entities (`&#8211;`).
- `is_on_backorder` ("Cho phép đặt hàng trước", pre-order allowed) counts as **not** in stock even when `is_in_stock` is true.
- Fallback for HTML pages: `.product .price .woocommerce-Price-amount`.
- Responses embed full HTML descriptions; snapshots in `testdata/` are trimmed to the fields the adapter reads.
