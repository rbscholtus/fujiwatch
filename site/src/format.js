// 27490000 → "27.490.000₫" (matches the Go price.FormatVND).
export function vnd(v) {
  if (!v) return "liên hệ";
  return v.toString().replace(/\B(?=(\d{3})+(?!\d))/g, ".") + "₫";
}

// 27490000 → "27.5M" for axis labels.
export function vndShort(v) {
  return (v / 1e6).toFixed(v >= 1e8 ? 0 : 1).replace(/\.0$/, "") + "M";
}

export function day(iso) {
  return new Date(iso).toLocaleDateString("vi-VN", { timeZone: "Asia/Ho_Chi_Minh" });
}

// Stable colour per shop, readable in light and dark mode.
const palette = ["#2563eb", "#d97706", "#059669", "#db2777", "#7c3aed", "#0891b2", "#dc2626", "#65a30d", "#9333ea", "#ea580c"];
export function shopColor(shop, shops) {
  const i = shops.indexOf(shop);
  return palette[(i < 0 ? 0 : i) % palette.length];
}
