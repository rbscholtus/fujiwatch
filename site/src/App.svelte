<script>
  import Chart from "./Chart.svelte";
  import { vnd, day } from "./format.js";

  let index = $state(null);
  let error = $state("");
  let selected = $state(decodeURIComponent(location.hash.slice(1)));
  let product = $state(null);
  let condition = $state("all");

  // Data files are generated at build time (fujiwatch sitedata) and served
  // next to the page; there is no backend.
  async function load(path) {
    const res = await fetch(path, { cache: "no-cache" });
    if (!res.ok) throw new Error(`${path}: HTTP ${res.status}`);
    return res.json();
  }

  load("./data/index.json")
    .then((d) => {
      index = d;
      if (!d.products.some((p) => p.id === selected)) selected = d.products[0]?.id ?? "";
    })
    .catch((e) => (error = e.message));

  $effect(() => {
    if (!selected) return;
    history.replaceState(null, "", "#" + encodeURIComponent(selected));
    product = null;
    const id = selected;
    load(`./data/${id}.json`)
      .then((d) => { if (selected === id) product = d; })
      .catch((e) => (error = e.message));
  });

  const entry = $derived(index?.products.find((p) => p.id === selected));
  const shops = $derived([...new Set((product?.series ?? []).map((s) => s.shop))].sort());
  const series = $derived((product?.series ?? []).filter((s) => condition === "all" || s.condition === condition));
  const offers = $derived((entry?.offers ?? []).filter((o) => condition === "all" || o.condition === condition));
</script>

<main>
  <header>
    <h1>fujiwatch</h1>
    <p class="muted small">
      Fujifilm X-mount prices in Vietnamese camera shops, checked daily.
      {#if index}Updated {day(index.generated)}.{/if}
    </p>
  </header>

  {#if error}
    <p class="error">Could not load data: {error}</p>
  {:else if !index}
    <p class="muted">Loading…</p>
  {:else}
    <nav class="products" aria-label="Products">
      {#each index.products as p (p.id)}
        <button type="button" class:active={p.id === selected} onclick={() => (selected = p.id)}
          aria-current={p.id === selected ? "page" : null}>
          {p.name}
        </button>
      {/each}
    </nav>

    {#if entry}
      <section class="best">
        {#each ["new", "used"] as c}
          {@const b = entry.best[c]}
          <div class="card">
            <div class="muted small">Best {c}{#if entry.targets?.[c]} · target {vnd(entry.targets[c])}{/if}</div>
            {#if b}
              <div class="price"><a href={b.url} rel="noopener">{vnd(b.price_vnd)}</a></div>
              <div class="small">{b.shop}{b.in_stock === null ? " · stock unknown" : ""}</div>
            {:else}
              <div class="price muted">–</div>
              <div class="small muted">no offer in stock</div>
            {/if}
          </div>
        {/each}
      </section>

      <div class="filter small" role="radiogroup" aria-label="Condition">
        {#each ["all", "new", "used"] as c}
          <label><input type="radio" bind:group={condition} value={c} /> {c}</label>
        {/each}
      </div>

      <h2>Price history</h2>
      {#if product}
        <Chart {series} {shops}
          targets={Object.entries(entry.targets ?? {}).filter(([c]) => condition === "all" || c === condition)} />
      {:else}
        <p class="muted">Loading…</p>
      {/if}

      <h2>Current offers ({offers.length})</h2>
      {#if offers.length}
        <div class="table-wrap">
          <table>
            <thead>
              <tr><th>Shop</th><th>Cond.</th><th class="num">Price</th><th>Stock</th><th>Listing</th></tr>
            </thead>
            <tbody>
              {#each offers as o (o.shop + o.url)}
                <tr>
                  <td>{o.shop}</td>
                  <td>{o.condition}</td>
                  <td class="num">{vnd(o.price_vnd)}</td>
                  <td class:good={o.in_stock === true} class:bad={o.in_stock === false}>
                    {o.in_stock === null ? "?" : o.in_stock ? "yes" : "no"}
                  </td>
                  <td><a href={o.url} rel="noopener">{o.title}</a>
                    <div class="muted small">since {day(o.first_seen)}</div></td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {:else}
        <p class="muted">No offers right now.</p>
      {/if}
    {/if}
  {/if}

  <footer class="muted small">
    Source: <a href="https://github.com/rbscholtus/fujiwatch">github.com/rbscholtus/fujiwatch</a>
  </footer>
</main>

<style>
  .products { display: flex; flex-wrap: wrap; gap: 6px; margin: 12px 0 16px; }
  .products button {
    font: inherit; color: var(--fg); background: var(--card); border: 1px solid var(--line);
    border-radius: 6px; padding: 6px 10px; cursor: pointer;
  }
  .products button.active { border-color: var(--accent); box-shadow: inset 0 0 0 1px var(--accent); }
  .best { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 10px; }
  .card { background: var(--card); border: 1px solid var(--line); border-radius: 8px; padding: 10px 12px; }
  .price { font-size: 1.35rem; font-weight: 600; font-variant-numeric: tabular-nums; }
  .price a { text-decoration: none; }
  .filter { display: flex; gap: 14px; margin-top: 14px; }
  .good { color: var(--good); }
  .bad { color: var(--bad); }
  .error { color: var(--bad); }
  footer { margin-top: 32px; padding-top: 12px; border-top: 1px solid var(--line); }
</style>
