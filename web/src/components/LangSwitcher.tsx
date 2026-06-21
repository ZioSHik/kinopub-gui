import clsx from "clsx";
import { useI18n, type Lang } from "../i18n";

const LANGS: { id: Lang; label: string }[] = [
  { id: "en", label: "EN" },
  { id: "ru", label: "RU" },
];

export function LangSwitcher() {
  const { lang, setLang } = useI18n();
  return (
    <div className="inline-flex items-center rounded-full border border-white/[0.08] bg-white/[0.02] p-0.5">
      {LANGS.map((l) => (
        <button
          key={l.id}
          onClick={() => setLang(l.id)}
          className={clsx(
            "rounded-full px-2.5 py-1 text-xs font-semibold transition",
            lang === l.id ? "bg-gold-500 text-ink-950" : "text-slate-400 hover:text-slate-200",
          )}
        >
          {l.label}
        </button>
      ))}
    </div>
  );
}
