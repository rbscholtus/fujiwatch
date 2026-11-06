<script>
  import { vnd, vndShort, shopColor } from "./format.js";

  /** @type {{ series: {shop: string, condition: string, points: {date: string, price_vnd: number, url: string}[]}[], shops: string[] }} */
  let { series, shops, targets = [] } = $props();

  let width = $state(0);
  const height = 300;
  const pad = { top: 12, right: 16, bottom: 28, left: 52 };

  let hidden = $state(new Set());
  let hoverDate = $state(null);

  const key = (s) => s.shop + "|" + s.condition;
  const visible = $derived(series.filter((s) => !hidden.has(key(s))));

  const dates = $derived([...new Set(visible.flatMap((s) => s.points.map((p) => p.date)))].sort());
  const t = (d) => Date.parse(d + "T00:00:00Z");

  const xDomain = $derived(dates.length ? [t(dates[0]), t(dates[dates.length - 1])] : [0, 1]);
  const yDomain = $derived.by(() => {
    const prices = visible.flatMap((s) => s.points.map((p) => p.price_vnd)).concat(targets.map(([, v]) => v));
    if (!prices.length) return [0, 1];
    let lo = Math.min(...prices), hi = Math.max(...prices);
    const margin = Math.max((hi - lo) * 0.08, hi * 0.02);
    return [Math.max(0, lo - margin), hi + margin];
  });

  const innerW = $derived(Math.max(width - pad.left - pad.right, 10));
  const innerH = height - pad.top - pad.bottom;
  const x = (d) => {
    const [a, b] = xDomain;
    return pad.left + (b === a ? innerW / 2 : ((t(d) - a) / (b - a)) * innerW);
  };
  const y = (v) => {
    const [a, b] = yDomain;
    return pad.top + innerH - ((v - a) / (b - a)) * innerH;
  };

  const yTicks = $derived.by(() => {
    const [a, b] = yDomain;
    return Array.from({ length: 5 }, (_, i) => a + ((b - a) * i) / 4);
  });
  const xTicks = $derived.by(() => {
    if (dates.length <= 1) return dates;
    const n = Math.min(6, dates.length);
    return Array.from({ length: n }, (_, i) => dates[Math.round((i * (dates.length - 1)) / (n - 1))]);
  });

  const path = (s) => s.points.map((p, i) => `${i ? "L" : "M"}${x(p.date).toFixed(1)},${y(p.price_vnd).toFixed(1)}`).join("");

  function onMove(e) {
    if (!dates.length) return;
    const rect = e.currentTarget.getBoundingClientRect();
    const px = e.clientX - rect.left;
    let best = dates[0];
    for (const d of dates) if (Math.abs(x(d) - px) < Math.abs(x(best) - px)) best = d;
    hoverDate = best;
  }

  const hoverRows = $derived(
    hoverDate
      ? visible
          .map((s) => ({ s, p: s.points.find((p) => p.date === hoverDate) }))
          .filter((r) => r.p)
          .sort((a, b) => a.p.price_vnd - b.p.price_vnd)
      : []
  );

  function toggle(s) {
    const k = key(s);
    const next = new Set(hidden);
    next.has(k) ? next.delete(k) : next.add(k);
    hidden = next;
  }
</script>

<div class="chart" bind:clientWidth={width}>
  {#if !series.length}
    <p class="muted">No price history yet.</p>
  {:else}
    <svg width={Math.max(width, 1)} {height} role="img" aria-label="Price history"
      onpointermove={onMove} onpointerleave={() => (hoverDate = null)}>
      {#each yTicks as v}
        <line class="grid" x1={pad.left} x2={width - pad.right} y1={y(v)} y2={y(v)} />
        <text class="tick" x={pad.left - 6} y={y(v) + 4} text-anchor="end">{vndShort(v)}</text>
      {/each}
      {#each xTicks as d}
        <text class="tick" x={x(d)} y={height - 8} text-anchor="middle">{d.slice(5)}</text>
      {/each}

      {#each visible as s (key(s))}
        {@const c = shopColor(s.shop, shops)}
        <path d={path(s)} fill="none" stroke={c} stroke-width="2"
          stroke-dasharray={s.condition === "used" ? "5 4" : null} />
        {#if s.condition === "used" || s.points.length === 1}
          {#each s.points as p}
            <circle cx={x(p.date)} cy={y(p.price_vnd)} r="3.5" fill={s.condition === "used" ? "var(--bg)" : c} stroke={c} stroke-width="2" />
          {/each}
        {/if}
      {/each}

      {#each targets as [cond, v]}
        <line class="target" x1={pad.left} x2={width - pad.right} y1={y(v)} y2={y(v)} />
        <text class="tick target-label" x={width - pad.right} y={y(v) - 4} text-anchor="end">target {cond} {vndShort(v)}</text>
      {/each}

      {#if hoverDate}
        <line class="hover" x1={x(hoverDate)} x2={x(hoverDate)} y1={pad.top} y2={pad.top + innerH} />
      {/if}
    </svg>

    <div class="readout small" aria-live="polite">
      {#if hoverDate}
        <strong>{hoverDate}</strong>
        {#each hoverRows as r}
          <span><i style:background={shopColor(r.s.shop, shops)}></i>{r.s.shop} ({r.s.condition}) {vnd(r.p.price_vnd)}</span>
        {/each}
      {:else}
        <span class="muted">Hover the chart for prices on a day. Dashed lines with open markers are used listings.</span>
      {/if}
    </div>

    <div class="legend small">
      {#each series as s (key(s))}
        <button type="button" class:off={hidden.has(key(s))} onclick={() => toggle(s)}
          aria-pressed={!hidden.has(key(s))}>
          <i style:background={shopColor(s.shop, shops)} class:used={s.condition === "used"}></i>
          {s.shop} · {s.condition}
        </button>
      {/each}
    </div>
  {/if}
</div>

<style>
  /* The wrapper sizes from the page, never from the SVG inside it, so the
     chart shrinks to phone width instead of widening the page. */
  .chart { width: 100%; min-width: 0; overflow: hidden; }
  svg { display: block; max-width: 100%; touch-action: pan-y; }
  .grid { stroke: var(--line); stroke-width: 1; }
  .hover { stroke: var(--muted); stroke-width: 1; stroke-dasharray: 2 3; }
  .tick { fill: var(--muted); font-size: 11px; }
  .target { stroke: var(--accent); stroke-width: 1.5; stroke-dasharray: 8 4; }
  .target-label { fill: var(--accent); }
  .readout { min-height: 2.6em; display: flex; flex-wrap: wrap; gap: 4px 14px; align-items: center; margin-top: 4px; }
  .legend { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 8px; }
  .legend button {
    font: inherit; color: var(--fg); background: var(--card); border: 1px solid var(--line);
    border-radius: 999px; padding: 2px 10px; cursor: pointer;
  }
  .legend button.off { opacity: 0.4; text-decoration: line-through; }
  i { display: inline-block; width: 10px; height: 10px; border-radius: 50%; margin-right: 5px; vertical-align: -1px; }
  i.used { background-image: linear-gradient(90deg, transparent 50%, var(--bg) 50%); }
</style>
