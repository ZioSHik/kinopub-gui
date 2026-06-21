export function bytes(n?: number): string {
  if (!n || n <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(v >= 100 || i === 0 ? 0 : 1)} ${units[i]}`;
}

export function speed(bps?: number): string {
  if (!bps || bps <= 0) return "—";
  return `${bytes(bps)}/s`;
}

// TFunc matches the i18n translator. A no-op default keeps these usable without
// a translator (returns the English templates, interpolating placeholders).
type TFunc = (key: string, vars?: Record<string, string | number>) => string;
const idT: TFunc = (k, v) => {
  let o = k;
  if (v) for (const x of Object.keys(v)) o = o.split(`{${x}}`).join(String(v[x]));
  return o;
};

export function eta(seconds?: number, t: TFunc = idT): string {
  if (!seconds || seconds <= 0) return "—";
  if (seconds < 60) return t("{n}s", { n: Math.round(seconds) });
  const m = Math.floor(seconds / 60);
  const s = Math.round(seconds % 60);
  if (m < 60) return t("{m}m {s}s", { m, s });
  const h = Math.floor(m / 60);
  return t("{h}h {m}m", { h, m: m % 60 });
}

export function duration(seconds?: number, t: TFunc = idT): string {
  if (!seconds || seconds <= 0) return "";
  const m = Math.floor(seconds / 60);
  if (m < 60) return t("{m}m", { m });
  const h = Math.floor(m / 60);
  return t("{h}h {m}m", { h, m: m % 60 });
}

export function relTime(iso?: string, t: TFunc = idT): string {
  if (!iso) return "";
  const d = new Date(iso).getTime();
  if (Number.isNaN(d)) return "";
  const diff = (Date.now() - d) / 1000;
  if (diff < 60) return t("just now");
  if (diff < 3600) return t("{n}m ago", { n: Math.floor(diff / 60) });
  if (diff < 86400) return t("{n}h ago", { n: Math.floor(diff / 3600) });
  return t("{n}d ago", { n: Math.floor(diff / 86400) });
}

export function clockTime(iso?: string): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  return d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

export function parseSeasons(seasons: number[]): string {
  // Compact a list of season numbers into a "1,3-5" selection string.
  const sorted = [...new Set(seasons)].sort((a, b) => a - b);
  const parts: string[] = [];
  let i = 0;
  while (i < sorted.length) {
    let j = i;
    while (j + 1 < sorted.length && sorted[j + 1] === sorted[j] + 1) j++;
    parts.push(i === j ? `${sorted[i]}` : `${sorted[i]}-${sorted[j]}`);
    i = j + 1;
  }
  return parts.join(",");
}
