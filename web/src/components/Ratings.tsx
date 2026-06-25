import { ThumbsUp } from "lucide-react";
import type { DiscoverItem } from "../api";

// rateColor tints a 0–10 score green when good, amber when mid, dim when low.
function rateColor(v: number): string {
  if (v >= 7) return "text-emerald-400";
  if (v >= 5) return "text-amber-300";
  return "text-slate-400";
}

// Ratings shows kino.pub local (👍), Kinopoisk and IMDb scores, styled like the
// site: a source mark followed by the number. Shared by the catalog grid and
// the title-detail card so they look identical.
export function Ratings({ item, className }: { item: Pick<DiscoverItem, "rating" | "kinopoiskRating" | "imdbRating">; className?: string }) {
  if (item.rating <= 0 && item.kinopoiskRating <= 0 && item.imdbRating <= 0) return null;
  return (
    <div className={`flex flex-nowrap items-center gap-x-2.5 text-xs font-bold ${className || ""}`}>
      {item.rating > 0 && (
        <span className="inline-flex items-center gap-1">
          <ThumbsUp className="h-3.5 w-3.5 text-slate-400" />
          <span className={rateColor(item.rating)}>{item.rating.toFixed(1)}</span>
        </span>
      )}
      {item.kinopoiskRating > 0 && (
        <span className="inline-flex items-center gap-1">
          <span className="rounded bg-orange-500/90 px-1 text-[8px] font-extrabold text-black">КП</span>
          <span className={rateColor(item.kinopoiskRating)}>{item.kinopoiskRating.toFixed(1)}</span>
        </span>
      )}
      {item.imdbRating > 0 && (
        <span className="inline-flex items-center gap-1">
          <span className="rounded bg-yellow-400/90 px-1 text-[8px] font-extrabold text-black">IMDb</span>
          <span className={rateColor(item.imdbRating)}>{item.imdbRating.toFixed(1)}</span>
        </span>
      )}
    </div>
  );
}
