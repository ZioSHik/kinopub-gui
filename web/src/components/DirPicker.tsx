import { useEffect, useState } from "react";
import { ArrowUp, Check, Folder, FolderOpen } from "lucide-react";
import { api, type FSListing } from "../api";
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

  const load = (path: string) => {
    setLoading(true);
    setError("");
    api
      .fs(path)
      .then(setListing)
      .catch((e) => setError(String(e.message || e)))
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    if (open) load(initial || "");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  return (
    <Modal open={open} onClose={onClose} title={t("Choose a folder")}>
      <div className="space-y-3">
        <div className="flex items-center gap-2">
          <button
            className="btn-ghost px-3 py-2"
            onClick={() => listing && load(listing.parent)}
            disabled={!listing || loading}
            title={t("Parent folder")}
          >
            <ArrowUp className="h-4 w-4" />
          </button>
          <div className="flex-1 truncate rounded-xl border border-white/[0.08] bg-ink-900/70 px-3 py-2 font-mono text-xs text-slate-300">
            {listing?.path || "…"}
          </div>
        </div>

        <div className="h-64 overflow-y-auto rounded-xl border border-white/[0.06] bg-ink-900/40 p-1.5">
          {loading ? (
            <div className="flex h-full items-center justify-center text-slate-500">
              <Spinner className="h-5 w-5" />
            </div>
          ) : error ? (
            <div className="p-4 text-sm text-ember-400">{error}</div>
          ) : listing && listing.dirs.length > 0 ? (
            listing.dirs.map((d) => (
              <button
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
            className="btn-primary"
            disabled={!listing}
            onClick={() => {
              if (listing) {
                onSelect(listing.path);
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
