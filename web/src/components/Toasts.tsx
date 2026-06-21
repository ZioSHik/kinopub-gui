import clsx from "clsx";
import { CheckCircle2, Info, X, XCircle } from "lucide-react";
import { useApp } from "../store";

export function Toasts() {
  const { toasts, dismissToast } = useApp();
  return (
    <div className="pointer-events-none fixed bottom-4 right-4 z-[60] flex w-full max-w-sm flex-col gap-2">
      {toasts.map((t) => (
        <div
          key={t.id}
          className={clsx(
            "pointer-events-auto flex animate-fade-in items-start gap-3 rounded-xl border px-4 py-3 text-sm shadow-card backdrop-blur",
            t.kind === "success" && "border-emerald-500/25 bg-emerald-500/[0.12] text-emerald-100",
            t.kind === "error" && "border-ember-500/30 bg-ember-500/[0.14] text-rose-100",
            t.kind === "info" && "border-white/10 bg-ink-800/90 text-slate-200",
          )}
        >
          {t.kind === "success" ? (
            <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0" />
          ) : t.kind === "error" ? (
            <XCircle className="mt-0.5 h-4 w-4 shrink-0" />
          ) : (
            <Info className="mt-0.5 h-4 w-4 shrink-0" />
          )}
          <span className="min-w-0 flex-1 break-words">{t.message}</span>
          <button onClick={() => dismissToast(t.id)} className="shrink-0 opacity-60 hover:opacity-100">
            <X className="h-4 w-4" />
          </button>
        </div>
      ))}
    </div>
  );
}
