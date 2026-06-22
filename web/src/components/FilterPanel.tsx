import { useEffect, useState, type ReactNode } from "react";
import { ChevronDown, ChevronUp, RotateCcw, SlidersHorizontal } from "lucide-react";
import { api, type NamedRef } from "../api";
import { useI18n } from "../i18n";

export interface FilterState {
  type: string;
  genre: string;
  country: string;
  sort: string;
  yearFrom: number;
  yearTo: number;
  kpFrom: number;
  kpTo: number;
  imdbFrom: number;
  imdbTo: number;
  ac3: boolean;
  subtitles: boolean;
}

export const YEAR_MIN = 1912;
export const YEAR_MAX = 2026;

export const defaultFilter = (): FilterState => ({
  type: "",
  genre: "",
  country: "",
  sort: "updated-",
  yearFrom: YEAR_MIN,
  yearTo: YEAR_MAX,
  kpFrom: 0,
  kpTo: 10,
  imdbFrom: 0,
  imdbTo: 10,
  ac3: false,
  subtitles: false,
});

const TYPES = [
  { v: "", label: "All" },
  { v: "movie", label: "Movies" },
  { v: "serial", label: "Series" },
  { v: "4k", label: "4K" },
  { v: "concert", label: "Concerts" },
  { v: "documovie", label: "Documentary" },
  { v: "tvshow", label: "TV shows" },
];

const SORTS = [
  { v: "updated-", label: "By update" },
  { v: "views-", label: "Popular" },
  { v: "created-", label: "Fresh" },
  { v: "watchers-", label: "Hot" },
  { v: "year-", label: "Year" },
  { v: "kinopoisk-", label: "KP rating" },
  { v: "imdb-", label: "IMDb rating" },
];

export function FilterPanel({
  value,
  onChange,
  onReset,
}: {
  value: FilterState;
  onChange: (f: FilterState) => void;
  onReset: () => void;
}) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const [genres, setGenres] = useState<NamedRef[]>([]);
  const [countries, setCountries] = useState<NamedRef[]>([]);

  // Genres are fetched per content type so the list is clean (the unfiltered
  // endpoint mixes in music/docu junk). Default to movie genres.
  useEffect(() => {
    api.discoverGenres(value.type || "movie").then((r) => setGenres(r.items || [])).catch(() => {});
  }, [value.type]);
  useEffect(() => {
    api.discoverCountries().then((r) => setCountries(r.items || [])).catch(() => {});
  }, []);

  const set = <K extends keyof FilterState>(k: K, v: FilterState[K]) => onChange({ ...value, [k]: v });

  return (
    <div className="card overflow-hidden">
      <button
        onClick={() => setOpen((o) => !o)}
        className="flex w-full items-center gap-2 px-4 py-3 text-sm font-medium text-slate-200 hover:bg-white/[0.03]"
      >
        <SlidersHorizontal className="h-4 w-4 text-gold-400" /> {t("Filter")}
        {open ? <ChevronUp className="ml-auto h-4 w-4" /> : <ChevronDown className="ml-auto h-4 w-4" />}
      </button>
      {open && (
        <div className="space-y-4 border-t border-white/[0.06] p-4">
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
            <Select label={t("Type")} value={value.type} onChange={(v) => set("type", v)} options={TYPES.map((x) => ({ v: x.v, label: t(x.label) }))} />
            <Select label={t("Genre")} value={value.genre} onChange={(v) => set("genre", v)} options={[{ v: "", label: t("Any") }, ...genres.map((g) => ({ v: g.id, label: g.title }))]} />
            <Select label={t("Country")} value={value.country} onChange={(v) => set("country", v)} options={[{ v: "", label: t("Any") }, ...countries.map((c) => ({ v: c.id, label: c.title }))]} />
            <Select label={t("Sort")} value={value.sort} onChange={(v) => set("sort", v)} options={SORTS.map((x) => ({ v: x.v, label: t(x.label) }))} />
          </div>

          <Range label={t("Release year")} min={YEAR_MIN} max={YEAR_MAX} step={1} from={value.yearFrom} to={value.yearTo} onChange={(a, b) => onChange({ ...value, yearFrom: a, yearTo: b })} />

          <div className="grid gap-4 sm:grid-cols-2">
            <Range label={t("Kinopoisk rating")} min={0} max={10} step={0.5} from={value.kpFrom} to={value.kpTo} onChange={(a, b) => onChange({ ...value, kpFrom: a, kpTo: b })} />
            <Range label={t("IMDb rating")} min={0} max={10} step={0.5} from={value.imdbFrom} to={value.imdbTo} onChange={(a, b) => onChange({ ...value, imdbFrom: a, imdbTo: b })} />
          </div>

          <div className="flex flex-wrap items-center gap-5">
            <Check label={t("AC3 sound")} checked={value.ac3} onChange={(v) => set("ac3", v)} />
            <Check label={t("With subtitles")} checked={value.subtitles} onChange={(v) => set("subtitles", v)} />
            <button onClick={onReset} className="ml-auto flex items-center gap-1.5 text-xs text-slate-400 hover:text-gold-300">
              <RotateCcw className="h-3.5 w-3.5" /> {t("Reset filters")}
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

function Select({
  label,
  value,
  onChange,
  options,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  options: { v: string; label: string }[];
}) {
  return (
    <div>
      <label className="label">{label}</label>
      <select className="input" value={value} onChange={(e) => onChange(e.target.value)}>
        {options.map((o) => (
          <option key={o.v} value={o.v}>
            {o.label}
          </option>
        ))}
      </select>
    </div>
  );
}

// Range is a two-slider min/max control. Two native sliders keep it robust and
// accessible; the active span is shown as a value badge.
function Range({
  label,
  min,
  max,
  step,
  from,
  to,
  onChange,
}: {
  label: string;
  min: number;
  max: number;
  step: number;
  from: number;
  to: number;
  onChange: (from: number, to: number) => void;
}) {
  const pct = (v: number) => ((v - min) / (max - min)) * 100;
  return (
    <div>
      <div className="mb-2.5 flex items-center justify-between">
        <label className="label mb-0">{label}</label>
        <span className="rounded bg-gold-500/15 px-1.5 py-0.5 font-mono text-xs text-gold-300">
          {from} – {to}
        </span>
      </div>
      <div className="relative h-4">
        <div className="absolute top-1/2 h-1.5 w-full -translate-y-1/2 rounded-full bg-white/10" />
        <div
          className="absolute top-1/2 h-1.5 -translate-y-1/2 rounded-full bg-gold-500/70"
          style={{ left: `${pct(from)}%`, right: `${100 - pct(to)}%` }}
        />
        <input
          type="range"
          className="range-dual"
          min={min}
          max={max}
          step={step}
          value={from}
          onChange={(e) => onChange(Math.min(Number(e.target.value), to), to)}
        />
        <input
          type="range"
          className="range-dual"
          min={min}
          max={max}
          step={step}
          value={to}
          onChange={(e) => onChange(from, Math.max(Number(e.target.value), from))}
        />
      </div>
    </div>
  );
}

function Check({ label, checked, onChange }: { label: ReactNode; checked: boolean; onChange: (v: boolean) => void }) {
  return (
    <label className="flex cursor-pointer items-center gap-2 text-sm text-slate-300">
      <input type="checkbox" checked={checked} onChange={(e) => onChange(e.target.checked)} className="h-4 w-4 accent-gold-500" />
      {label}
    </label>
  );
}
