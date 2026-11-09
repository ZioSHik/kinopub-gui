import { useEffect, useRef, useState } from "react";
import { ArrowUp, Check, Folder, FolderOpen } from "lucide-react";
import { api, isNavigationAbort, type FSListing } from "../api";
import { useI18n } from "../i18n";
import { Modal, Spinner } from "./ui";

export function DirPicker({
  open,
  initial,
  onClose,
  onSelect,
}: {
  open: boolean;
  initial?: string;
  onClose: () => void;
  onSelect: (path: string) => void;
}) {
  const { t } = useI18n();
  const [listing, setListing] = useState<FSListing | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [path, setPath] = useState("");
  const request = useRef(0);

  const load = async (path: string): Promise<FSListing | null> => {
    const id = ++request.current;
    setLoading(true);
    setError("");
    try {
      const result = await api.fs(path.trim());
      if (id !== request.current) return null;
      const next = { ...result, dirs: result.dirs ?? [] };
      setListing(next);
      setPath(next.path);
      return next;
    } catch (e) {
      if (id === request.current && !isNavigationAbort(e)) {
        setError(String(e instanceof Error ? e.message : e));
      }
      return null;
    } finally {
      if (id === request.current) setLoading(false);
    }
  };

  useEffect(() => {
    if (open) {
      setListing(null);
      setPath(initial || "");
      void load(initial || "");
    }
    return () => { ++request.current; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  return (
    <Modal open={open} onClose={onClose} title={t("Choose a folder")}>
      <div className="space-y-3">
        <form className="flex items-center gap-2" onSubmit={(e) => {
          e.preventDefault();
          void load(path);
        }}>
          <button
            type="button"
            className="btn-ghost px-3 py-2"
            onClick={() => listing && load(listing.parent)}
            disabled={!listing || loading}
            title={t("Parent folder")}
          >
            <ArrowUp className="h-4 w-4" />
          </button>
          <input
            aria-label={t("Folder path")}
            className="input min-w-0 flex-1 font-mono text-xs"
            value={path}
            onChange={(e) => setPath(e.target.value)}
            placeholder={t("Folder path")}
          />
          <button type="submit" className="btn-ghost px-3 py-2" disabled={loading}>
            {t("Go to folder")}
          </button>
        </form>

        <div className="h-64 overflow-y-auto rounded-xl border border-white/[0.06] bg-ink-900/40 p-1.5">
          {loading ? (
            <div className="flex h-full items-center justify-center text-slate-500">
              <Spinner className="h-5 w-5" />
            </div>
          ) : error ? (
            <div className="p-4 text-sm text-ember-400">{error}</div>
          ) : listing?.dirs && listing.dirs.length > 0 ? (
            listing.dirs.map((d) => (
              <button
                type="button"
                key={d.path}
                onClick={() => load(d.path)}
                className="flex w-full items-center gap-2.5 rounded-lg px-3 py-2 text-left text-sm text-slate-300 hover:bg-white/[0.05]"
              >
                <Folder className="h-4 w-4 shrink-0 text-gold-400/80" />
                <span className="truncate">{d.name}</span>
              </button>
            ))
          ) : (
            <div className="p-4 text-sm text-slate-500">{t("No sub-folders here.")}</div>
          )}
        </div>

        <div className="flex items-center justify-between gap-3 pt-1">
          <span className="text-xs text-slate-500">
            <FolderOpen className="mr-1 inline h-3.5 w-3.5" />
            {t("Files download into this folder.")}
          </span>
          <button
            type="button"
            className="btn-primary"
            disabled={loading || !path.trim()}
            onClick={async () => {
              const selected = await load(path);
              if (selected) {
                onSelect(selected.path);
                onClose();
              }
            }}
          >
            <Check className="h-4 w-4" />
            {t("Use this folder")}
          </button>
        </div>
      </div>
    </Modal>
  );
}
