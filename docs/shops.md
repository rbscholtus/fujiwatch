# Shop notes

How each shop is read, what it allows, and its quirks. One section per shop (a test checks that every shop with an adapter has one). The adapter kinds are described in [architecture.md: shop adapters](architecture.md#shop-adapters); how to add a shop is in [usage.md](usage.md#adding-a-shop).

When a shop changes its site, or a new quirk turns up, note it here.

---

## xspace

fujifilm-xspace.vn, Haravan. Adapter: `haravan.go`.

- Product mode fetches the product JSON at `/products/<handle>.js` (verified): prices in VND × 100, with `available` per variant.
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

kyma.vn, custom platform. CSS layout in `adapter.go`.

- Cards are `li.li-product`; the first `span` in `.item__price` is the current price (the second is the struck-through one). No stock labels on category pages.
- The search page is server-rendered, but its search is picky; a query like `XF 18-55mm f/2.8-4` returned 0 results. Watch category pages (`/ong-kinh-fujifilm`, `/may-anh-fujifilm`) or exact product URLs instead.

## zshop

zshop.vn, CS-Cart (not Magento). CSS layouts in `adapter.go`.

- Category cards `.ty-grid-list__item`, price `.ty-price .ty-price-num`, out-of-stock label `.ty-qty-out-of-stock`.
- Product pages: `h1.ty-product-block-title`, price in `.ty-product-block__price-actual`.
- Upcoming products have no price (→ 0).

## mayanh24h

mayanh24h.com, custom platform (AngularJS front, server-rendered lists). CSS layouts in `adapter.go`.

- `robots.txt` disallows every URL with a query string, so only plain category and product URLs can be watched.
- Cards `.product-item` with a `.stock-status` label; used items are graded in the title ("Mới 98%" = 98% condition).
- Product pages: `h1.text_1`, price `h2.text-primary`, stock after "Tình trạng:".

## giangduydat

giangduydat.vn, custom platform. CSS layouts in `adapter.go`.

- Category cards `li.idx-prod`; the price reads "Từ 4.490.000 đ" (from).
- A product page lists every unit (new, used 85%, 90%, …) as its own schema.org `Offer` with a name and price, so product pages are watched in **listing** mode. Each unit gets the page URL plus a `#<slug of its name>` fragment as identity.

## WooCommerce: bncamera, mayanhhoangto, x-camera

bncamera.com, mayanhhoangto.com, x-camera.vn. Adapter: `woocommerce.go`.

- The Store API, `GET /wp-json/wc/store/v1/products`, returns clean JSON with prices and stock; it is enabled by default. The adapter rewrites shop URLs: product page → `?slug=<slug>`, search page `?s=<term>` → `?search=<term>`, category page → `?category=<last path segment>` (an unknown slug returns `[]`), always with `per_page=100`.
- Prices are strings in minor units (check `currency_minor_unit`); names contain HTML entities (`&#8211;`).
- `is_on_backorder` ("Cho phép đặt hàng trước", pre-order allowed) counts as **not** in stock even when `is_in_stock` is true.
- Fallback for HTML pages: `.product .price .woocommerce-Price-amount`.
- Responses embed full HTML descriptions; snapshots in `testdata/` are trimmed to the fields the adapter reads.
