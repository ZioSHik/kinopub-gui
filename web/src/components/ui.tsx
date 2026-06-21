import { memo, useEffect, type ReactNode } from "react";
import clsx from "clsx";
import { Film, Loader2, X } from "lucide-react";
import { imgURL } from "../api";

export function ProgressBar({
  value,
  variant = "gold",
  active = false,
  className,
}: {
  value: number;
  variant?: "gold" | "green" | "rose" | "slate" | "blue";
  active?: boolean;
  className?: string;
}) {
  const fill = {
    gold: "from-gold-400 to-gold-600",
    green: "from-emerald-400 to-emerald-600",
    rose: "from-ember-400 to-ember-500",
    slate: "from-slate-500 to-slate-600",
    blue: "from-sky-400 to-sky-600",
  }[variant];
  return (
    <div className={clsx("relative h-2 overflow-hidden rounded-full bg-white/[0.06]", className)}>
      <div
        className={clsx("h-full rounded-full bg-gradient-to-r transition-[width] duration-300", fill)}
        style={{ width: `${Math.max(0, Math.min(100, value))}%` }}
      >
        {active && (
          <div className="absolute inset-0 -translate-x-full animate-shimmer bg-gradient-to-r from-transparent via-white/25 to-transparent" />
        )}
      </div>
    </div>
  );
}

export function Spinner({ className }: { className?: string }) {
  return <Loader2 className={clsx("animate-spin", className)} />;
}

// PosterImage is memoized: a job card re-renders many times per second during a
// download, and without memo the <img> would re-render (and visibly flicker /
// "smear") on every tick. Props are stable strings, so memo keeps it static.
export const PosterImage = memo(function PosterImage({
  url,
  alt,
  className,
}: {
  url?: string;
  alt?: string;
  className?: string;
}) {
  return (
    <div
      className={clsx(
        "relative overflow-hidden bg-gradient-to-br from-ink-700 to-ink-850",
        className,
      )}
    >
      {/* Placeholder sits behind the image (z-0); the image overlays it (z-10). */}
      <div className="pointer-events-none absolute inset-0 z-0 flex items-center justify-center">
        <Film className="h-8 w-8 text-white/15" />
      </div>
      {url ? (
        <img
          src={imgURL(url)}
          alt={alt || ""}
          loading="lazy"
          decoding="async"
          draggable={false}
          className="absolute inset-0 z-10 h-full w-full select-none object-cover"
          onError={(e) => {
            (e.currentTarget as HTMLImageElement).style.display = "none";
          }}
        />
      ) : null}
    </div>
  );
});

export function Modal({
  open,
  onClose,
  title,
  children,
  wide,
}: {
  open: boolean;
  onClose: () => void;
  title: ReactNode;
  children: ReactNode;
  wide?: boolean;
}) {
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, onClose]);

  if (!open) return null;
  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center overflow-y-auto bg-black/70 p-4 backdrop-blur-sm sm:p-8">
      <div className="absolute inset-0" onClick={onClose} />
      <div
        className={clsx(
          "card relative my-auto w-full animate-fade-in p-6",
          wide ? "max-w-3xl" : "max-w-lg",
        )}
      >
        <div className="mb-5 flex items-center justify-between gap-4">
          <h2 className="text-lg font-semibold text-slate-100">{title}</h2>
          <button onClick={onClose} className="rounded-lg p-1.5 text-slate-400 hover:bg-white/[0.06] hover:text-slate-100">
            <X className="h-5 w-5" />
          </button>
        </div>
        {children}
      </div>
    </div>
  );
}

export function Toggle({
  checked,
  onChange,
  label,
  hint,
}: {
  checked: boolean;
  onChange: (v: boolean) => void;
  label: string;
  hint?: string;
}) {
  return (
    <label className="flex cursor-pointer items-start justify-between gap-3 rounded-xl border border-white/[0.06] bg-ink-900/40 px-3.5 py-3 transition hover:border-white/[0.1]">
      <span>
        <span className="block text-sm font-medium text-slate-200">{label}</span>
        {hint && <span className="mt-0.5 block text-xs text-slate-500">{hint}</span>}
      </span>
      <button
        type="button"
        onClick={() => onChange(!checked)}
        className={clsx(
          "relative mt-0.5 h-6 w-11 shrink-0 rounded-full transition",
          checked ? "bg-gold-500" : "bg-white/[0.12]",
        )}
      >
        <span
          className={clsx(
            "absolute top-0.5 h-5 w-5 rounded-full bg-white shadow transition",
            checked ? "left-[22px]" : "left-0.5",
          )}
        />
      </button>
    </label>
  );
}

export function Field({
  label,
  hint,
  children,
}: {
  label: string;
  hint?: string;
  children: ReactNode;
}) {
  return (
    <div>
      <label className="label">{label}</label>
      {children}
      {hint && <p className="mt-1 text-xs text-slate-500">{hint}</p>}
    </div>
  );
}

export function EmptyState({
  icon,
  title,
  hint,
  action,
}: {
  icon: ReactNode;
  title: string;
  hint?: string;
  action?: ReactNode;
}) {
  return (
    <div className="flex flex-col items-center justify-center rounded-2xl border border-dashed border-white/[0.08] py-16 text-center">
      <div className="mb-4 grid h-14 w-14 place-items-center rounded-2xl bg-white/[0.04] text-slate-500">
        {icon}
      </div>
      <p className="text-sm font-medium text-slate-300">{title}</p>
      {hint && <p className="mt-1 max-w-sm text-sm text-slate-500">{hint}</p>}
      {action && <div className="mt-5">{action}</div>}
    </div>
  );
}
