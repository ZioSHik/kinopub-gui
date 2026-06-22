import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Bookmark, KeyRound, Loader2, Search } from "lucide-react";
import {
  api,
  imgURL,
  type DiscoverBookmark,
  type DiscoverCollection,
  type DiscoverItem,
  type ItemsQuery,
  type NamedRef,
} from "../api";
import { useApp } from "../store";
import { useI18n } from "../i18n";
import {
  bookmarkTitle,
  collectionTitle,
  dismiss,
  pushRoute,
  rememberBookmarkTitle,
  rememberCollectionTitle,
  replaceRoute,
  useRoute,
} from "../router";
import { EmptyState } from "../components/ui";
import { Ratings } from "../components/Ratings";
import { TitleDetail } from "../components/TitleDetail";
import {
  FilterPanel,
  YEAR_MAX,
  YEAR_MIN,
  defaultFilter,
  type FilterState,
} from "../components/FilterPanel";

type Tab = "new" | "collections" | "watching" | "bookmarks" | "history";
type ColTab = "new" | "popular" | "watched" | "categories" | "subs";
type WatchTab = "serials" | "movies";

const COL_SORT: Record<"new" | "popular" | "watched", string> = {
  new: "created-",
  popular: "views-",
  watched: "watchers-",
};

function filterToQuery(f: FilterState): ItemsQuery {
  return {
    type: f.type || undefined,
    genre: f.genre || undefined,
    country: f.country || undefined,
    sort: f.sort || undefined,
    yearFrom: f.yearFrom > YEAR_MIN ? f.yearFrom : undefined,
    yearTo: f.yearTo < YEAR_MAX ? f.yearTo : undefined,
    kpFrom: f.kpFrom > 0 ? f.kpFrom : undefined,
    kpTo: f.kpTo < 10 ? f.kpTo : undefined,
    imdbFrom: f.imdbFrom > 0 ? f.imdbFrom : undefined,
    imdbTo: f.imdbTo < 10 ? f.imdbTo : undefined,
    ac3: f.ac3 || undefined,
    subtitles: f.subtitles || undefined,
  };
}

export function DiscoverPage({ onStarted, onOpenSettings }: { onStarted: () => void; onOpenSettings: () => void }) {
  const { kpauth, toast } = useApp();
  const { t } = useI18n();
  const loggedIn = kpauth.loggedIn;

  // The open collection and title card live in the URL hash (the single source
  // of truth), so both survive a reload and browser back/forward.
  const route = useRoute();
  const collectionId = route.collectionId ?? null;
  const bookmarkId = route.bookmarkId ?? null;
  const detailId = route.itemId ?? null;
  // Leaving a folder (collection or bookmark) drops back to the bare catalog.
  const leaveFolder = () => {
    if (collectionId || bookmarkId) replaceRoute({ page: "discover" });
  };

  const [tab, setTab] = useState<Tab>("new");
  const [colTab, setColTab] = useState<ColTab>("new");
  const [watchTab, setWatchTab] = useState<WatchTab>("serials");

  // Switching a primary tab also closes any open folder.
  const selectTab = (t: Tab) => {
    setTab(t);
    leaveFolder();
  };

  // Live search (debounced).
  const [search, setSearch] = useState("");
  const [committedSearch, setCommittedSearch] = useState("");
  useEffect(() => {
    const q = search.trim();
    const id = window.setTimeout(() => setCommittedSearch(q.length >= 2 ? q : ""), 350);
    return () => window.clearTimeout(id);
  }, [search]);

  // Filter (debounced so dragging sliders doesn't spam the API).
  const [filter, setFilter] = useState<FilterState>(defaultFilter);
  const [debFilter, setDebFilter] = useState<FilterState>(filter);
  useEffect(() => {
    const id = window.setTimeout(() => setDebFilter(filter), 400);
    return () => window.clearTimeout(id);
  }, [filter]);

  const [items, setItems] = useState<DiscoverItem[]>([]);
  const [collections, setCollections] = useState<DiscoverCollection[]>([]);
  const [bookmarks, setBookmarks] = useState<DiscoverBookmark[]>([]);
  const [genres, setGenres] = useState<NamedRef[]>([]);
  const [page, setPage] = useState(1);
  const [hasMore, setHasMore] = useState(false);
  const [loading, setLoading] = useState(false);

  const searching = committedSearch.length >= 2;
  const collectionsListMode =
    !searching && tab === "collections" && !collectionId && (colTab === "new" || colTab === "popular" || colTab === "watched");
  const categoriesMode = !searching && tab === "collections" && colTab === "categories" && !collectionId;
  const bookmarksListMode = !searching && tab === "bookmarks" && !bookmarkId;

  // Load genres once for "Categories".
  useEffect(() => {
    if (!loggedIn) return;
    api.discoverGenres().then((r) => setGenres(r.items || [])).catch(() => {});
  }, [loggedIn]);

  const fetchItems = useCallback(
    (p: number) => {
      if (searching) return api.discoverSearch(committedSearch, p);
      if (collectionId) return api.discoverCollection(collectionId, p);
      if (bookmarkId) return api.discoverBookmark(bookmarkId, p);
      if (tab === "history") return api.discoverHistory(p);
      if (tab === "watching") return api.discoverWatching(false, watchTab, p);
      if (tab === "collections" && colTab === "subs") return api.discoverWatching(true, "serials", p);
      return api.discoverItems({ ...filterToQuery(debFilter), page: p });
    },
    [searching, committedSearch, collectionId, bookmarkId, tab, watchTab, colTab, debFilter],
  );

  const loadPage = useCallback(
    async (reset: boolean) => {
      if (!loggedIn || categoriesMode) return;
      const next = reset ? 1 : page + 1;
      setLoading(true);
      try {
        if (collectionsListMode) {
          const r = await api.discoverCollections(COL_SORT[colTab as "new" | "popular" | "watched"], next);
          setCollections((c) => (reset ? r.items : [...c, ...r.items]));
          setHasMore((r.items?.length ?? 0) > 0 && next < 25);
          setPage(next);
        } else if (bookmarksListMode) {
          // Bookmark folders come back in a single, unpaginated list.
          const r = await api.discoverBookmarks();
          setBookmarks(r.items || []);
          setHasMore(false);
          setPage(1);
        } else {
          const r = await fetchItems(next);
          setItems((it) => (reset ? r.items : [...it, ...r.items]));
          setHasMore(r.hasMore);
          setPage(r.page || next);
        }
      } catch (e: any) {
        toast(e.message || t("Catalog request failed"), "error");
      } finally {
        setLoading(false);
      }
    },
    [loggedIn, categoriesMode, collectionsListMode, bookmarksListMode, colTab, page, fetchItems, toast, t],
  );

  // Reset + load whenever the data source changes.
  const sourceKey = useMemo(
    () => JSON.stringify([loggedIn, committedSearch, tab, colTab, watchTab, collectionId, bookmarkId, debFilter]),
    [loggedIn, committedSearch, tab, colTab, watchTab, collectionId, bookmarkId, debFilter],
  );
  useEffect(() => {
    if (!loggedIn) return;
    setItems([]);
    setCollections([]);
    setBookmarks([]);
    setPage(1);
    setHasMore(false);
    loadPage(true);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sourceKey]);

  // Infinite scroll: observe a sentinel; the ref always holds the latest loader.
  const sentinelRef = useRef<HTMLDivElement>(null);
  const loadMoreRef = useRef<() => void>(() => {});
  loadMoreRef.current = () => {
    if (hasMore && !loading) loadPage(false);
  };
  useEffect(() => {
    const el = sentinelRef.current;
    if (!el) return;
    const ob = new IntersectionObserver((es) => es[0]?.isIntersecting && loadMoreRef.current());
    ob.observe(el);
    return () => ob.disconnect();
  }, []);

  const openCatalogGenre = (id: string) => {
    setFilter({ ...defaultFilter(), genre: id });
    setTab("new");
  };

  // Opening a card or collection pushes a hash route, so the URL reflects the
  // exact view, a reload restores it, and browser-back closes it. The router
  // owns all of that — no more synthetic history entries here.
  const openDetail = (id: string) =>
    pushRoute({
      page: "discover",
      collectionId: collectionId ?? undefined,
      bookmarkId: bookmarkId ?? undefined,
      itemId: id,
    });
  const openCollection = (c: DiscoverCollection) => {
    rememberCollectionTitle(c.id, c.title);
    pushRoute({ page: "discover", collectionId: c.id });
  };
  const openBookmark = (b: DiscoverBookmark) => {
    rememberBookmarkTitle(b.id, b.title);
    pushRoute({ page: "discover", bookmarkId: b.id });
  };

  // Touching the filter brings the user to the (catalog) results it controls.
  const onFilterChange = (f: FilterState) => {
    setFilter(f);
    setTab("new");
    leaveFolder();
    setSearch("");
  };

  if (!loggedIn) {
    return (
      <div className="mx-auto max-w-6xl">
        <Header />
        <EmptyState
          icon={<KeyRound className="h-6 w-6" />}
          title={t("Sign in to kino.pub to browse the catalog")}
          hint={t("The catalog, search, voiceovers and one-click downloads use the official kino.pub API. Sign in once in Settings.")}
          action={
            <button className="btn-primary" onClick={onOpenSettings}>
              <KeyRound className="h-4 w-4" /> {t("Go to Settings")}
            </button>
          }
        />
      </div>
    );
  }

  return (
    <div className="mx-auto max-w-6xl space-y-5">
      <Header />

      {/* Live search */}
      <div className="relative">
        <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-500" />
        <input
          className="input pl-9"
          placeholder={t("Search films and series on kino.pub…")}
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        {searching && (
          <button
            onClick={() => setSearch("")}
            className="absolute right-3 top-1/2 -translate-y-1/2 text-xs text-slate-500 hover:text-slate-300"
          >
            {t("Clear")}
          </button>
        )}
      </div>

      {/* Filter lives right under the search and drives the catalog results. */}
      <FilterPanel value={filter} onChange={onFilterChange} onReset={() => onFilterChange(defaultFilter())} />

      {!searching && (
        <>
          {/* Primary tabs */}
          <div className="flex flex-wrap items-center gap-2">
            <SubChip active={tab === "new"} onClick={() => selectTab("new")}>{t("Browse")}</SubChip>
            <SubChip active={tab === "collections"} onClick={() => selectTab("collections")}>{t("Collections")}</SubChip>
            <SubChip active={tab === "watching"} onClick={() => selectTab("watching")}>{t("I'm watching")}</SubChip>
            <SubChip active={tab === "bookmarks"} onClick={() => selectTab("bookmarks")}>{t("Bookmarks")}</SubChip>
            <SubChip active={tab === "history"} onClick={() => selectTab("history")}>{t("History")}</SubChip>
          </div>

          {/* Browse: quick sort presets (same style as collections) */}
          {tab === "new" && (
            <div className="flex flex-wrap gap-2">
              <SubChip active={filter.sort === "views-"} onClick={() => setFilter((f) => ({ ...f, sort: "views-" }))}>{t("Popular")}</SubChip>
              <SubChip active={filter.sort === "created-"} onClick={() => setFilter((f) => ({ ...f, sort: "created-" }))}>{t("Fresh")}</SubChip>
              <SubChip active={filter.sort === "watchers-"} onClick={() => setFilter((f) => ({ ...f, sort: "watchers-" }))}>{t("Hot")}</SubChip>
            </div>
          )}

          {/* Collections sub-tabs */}
          {tab === "collections" && !collectionId && (
            <div className="flex flex-wrap gap-2">
              <SubChip active={colTab === "new"} onClick={() => setColTab("new")}>{t("New")}</SubChip>
              <SubChip active={colTab === "popular"} onClick={() => setColTab("popular")}>{t("Popular")}</SubChip>
              <SubChip active={colTab === "watched"} onClick={() => setColTab("watched")}>{t("Most watched")}</SubChip>
              <SubChip active={colTab === "categories"} onClick={() => setColTab("categories")}>{t("Categories")}</SubChip>
              <SubChip active={colTab === "subs"} onClick={() => setColTab("subs")}>{t("Subscriptions")}</SubChip>
            </div>
          )}

          {/* "I'm watching" sub-tabs (serials / movies) */}
          {tab === "watching" && (
            <div className="flex flex-wrap gap-2">
              <SubChip active={watchTab === "serials"} onClick={() => setWatchTab("serials")}>{t("Series")}</SubChip>
              <SubChip active={watchTab === "movies"} onClick={() => setWatchTab("movies")}>{t("Movies")}</SubChip>
            </div>
          )}

          {collectionId && (
            <div className="flex items-center gap-2 text-sm">
              <button className="text-gold-300 hover:underline" onClick={() => dismiss({ page: "discover" })}>
                ← {t("Collections")}
              </button>
              <span className="text-slate-500">/</span>
              <span className="font-medium text-slate-200">{collectionTitle(collectionId) || t("Collection")}</span>
            </div>
          )}

          {bookmarkId && (
            <div className="flex items-center gap-2 text-sm">
              <button className="text-gold-300 hover:underline" onClick={() => dismiss({ page: "discover" })}>
                ← {t("Bookmarks")}
              </button>
              <span className="text-slate-500">/</span>
              <span className="font-medium text-slate-200">{bookmarkTitle(bookmarkId) || t("Bookmarks")}</span>
            </div>
          )}
        </>
      )}

      {/* Content */}
      {loading && items.length === 0 && collections.length === 0 && bookmarks.length === 0 && !categoriesMode ? (
        <SkeletonGrid wide={collectionsListMode || bookmarksListMode} />
      ) : categoriesMode ? (
        <div className="flex flex-wrap gap-2">
          {genres.map((g) => (
            <button key={g.id} onClick={() => openCatalogGenre(g.id)} className="chip text-slate-300 hover:bg-white/[0.06]">
              {g.title}
            </button>
          ))}
        </div>
      ) : collectionsListMode ? (
        <CollectionsGrid collections={collections} onOpen={openCollection} />
      ) : bookmarksListMode ? (
        <BookmarksGrid folders={bookmarks} onOpen={openBookmark} />
      ) : tab === "history" && !searching ? (
        <HistoryView items={items} onOpen={(it) => openDetail(it.id)} />
      ) : (
        <ItemsGrid items={items} onOpen={(it) => openDetail(it.id)} />
      )}

      {/* Empty / loader / infinite-scroll sentinel */}
      {!loading &&
        !categoriesMode &&
        (bookmarksListMode
          ? bookmarks.length === 0
          : collectionsListMode
            ? collections.length === 0
            : items.length === 0) && (
          <p className="py-10 text-center text-sm text-slate-500">{t("Nothing found.")}</p>
        )}
      {loading && (items.length > 0 || collections.length > 0 || bookmarks.length > 0) && (
        <div className="flex justify-center py-6 text-slate-400">
          <Loader2 className="h-5 w-5 animate-spin" />
        </div>
      )}
      <div ref={sentinelRef} className="h-1" />

      {detailId && (
        <TitleDetail
          id={detailId}
          onClose={() =>
            dismiss({
              page: "discover",
              collectionId: collectionId ?? undefined,
              bookmarkId: bookmarkId ?? undefined,
            })
          }
          // A "similar" pick swaps the card in place (one modal = one history
          // entry), so the X button closes cleanly instead of stepping back
          // through every card visited.
          onPick={(it) =>
            replaceRoute({
              page: "discover",
              collectionId: collectionId ?? undefined,
              bookmarkId: bookmarkId ?? undefined,
              itemId: it.id,
            })
          }
          onStarted={onStarted}
        />
      )}
    </div>
  );
}

function Header() {
  const { t } = useI18n();
  return (
    <header>
      <h1 className="text-2xl font-bold text-slate-100">{t("Catalog")}</h1>
      <p className="mt-1 text-sm text-slate-400">
        {t("Search kino.pub, browse tops, collections and history, preview voiceovers — and download in one click.")}
      </p>
    </header>
  );
}

function SubChip({ active, onClick, children }: { active: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <button
      onClick={onClick}
      className={`rounded-lg px-3 py-1.5 text-sm transition ${active ? "bg-gold-500/[0.14] text-gold-200" : "text-slate-400 hover:bg-white/[0.05] hover:text-slate-200"}`}
    >
      {children}
    </button>
  );
}

function badgeOf(it: DiscoverItem, t: (k: string, v?: Record<string, string | number>) => string): string {
  if (it.season && it.season > 0 && it.episode) return t("Season {s}. Episode {e}", { s: it.season, e: it.episode });
  if (it.episode && it.episode > 1) return t("Episode {n}", { n: it.episode });
  return it.subtitle || "";
}

function ItemsGrid({ items, onOpen }: { items: DiscoverItem[]; onOpen: (it: DiscoverItem) => void }) {
  const { t } = useI18n();
  return (
    <div className="grid grid-cols-3 gap-3 sm:grid-cols-4 md:grid-cols-5 lg:grid-cols-6">
      {items.map((it) => {
        const badge = badgeOf(it, t);
        return (
          <button key={it.id} onClick={() => onOpen(it)} className="group text-left" title={it.originalTitle || it.title}>
            <div className="relative overflow-hidden rounded-xl bg-gradient-to-br from-ink-700 to-ink-850">
              <img
                src={imgURL(it.poster)}
                alt={it.title}
                loading="lazy"
                className="aspect-[2/3] w-full object-cover transition duration-200 group-hover:scale-[1.03]"
                onError={(e) => ((e.currentTarget as HTMLImageElement).style.visibility = "hidden")}
              />
              {badge && (
                <span className="absolute bottom-1.5 left-1.5 right-1.5 truncate rounded-md bg-black/75 px-1.5 py-0.5 text-center text-[10px] font-semibold text-emerald-300">
                  {badge}
                </span>
              )}
            </div>
            <p className="mt-1.5 truncate text-xs font-semibold text-slate-100">{it.title}</p>
            {it.originalTitle && <p className="truncate text-[11px] text-slate-500">{it.originalTitle}</p>}
            <p className="text-[11px] text-slate-500">{it.year || ""}</p>
            <Ratings item={it} className="mt-1" />
          </button>
        );
      })}
    </div>
  );
}

// SkeletonGrid shows placeholder cards on cold start so the catalog isn't empty
// while the first page loads.
function SkeletonGrid({ wide }: { wide?: boolean }) {
  return (
    <div className={wide ? "grid grid-cols-2 gap-3 sm:grid-cols-3 md:grid-cols-4" : "grid grid-cols-3 gap-3 sm:grid-cols-4 md:grid-cols-5 lg:grid-cols-6"}>
      {Array.from({ length: wide ? 8 : 18 }).map((_, i) => (
        <div key={i} className="animate-pulse">
          <div className={`${wide ? "aspect-video" : "aspect-[2/3]"} w-full rounded-xl bg-white/[0.05]`} />
          <div className="mt-1.5 h-3 w-3/4 rounded bg-white/[0.05]" />
          {!wide && <div className="mt-1 h-2.5 w-1/3 rounded bg-white/[0.04]" />}
        </div>
      ))}
    </div>
  );
}

// HistoryView groups watch history into per-day sections (newest first).
function HistoryView({ items, onOpen }: { items: DiscoverItem[]; onOpen: (it: DiscoverItem) => void }) {
  const { lang } = useI18n();
  const fmt = new Intl.DateTimeFormat(lang === "ru" ? "ru-RU" : "en-US", { day: "numeric", month: "long", year: "numeric" });
  const groups: { day: string; items: DiscoverItem[] }[] = [];
  for (const it of items) {
    const day = it.watchedAt ? fmt.format(new Date(it.watchedAt * 1000)) : "";
    const last = groups[groups.length - 1];
    if (last && last.day === day) last.items.push(it);
    else groups.push({ day, items: [it] });
  }
  return (
    <div className="space-y-6">
      {groups.map((g, i) => (
        <div key={i}>
          {g.day && <h3 className="mb-2.5 text-sm font-bold text-slate-200">{g.day}</h3>}
          <ItemsGrid items={g.items} onOpen={onOpen} />
        </div>
      ))}
    </div>
  );
}

// BookmarksGrid shows the account's bookmark folders. Folders have no poster
// (the API returns only id/title/count), so each card is an icon tile.
function BookmarksGrid({ folders, onOpen }: { folders: DiscoverBookmark[]; onOpen: (b: DiscoverBookmark) => void }) {
  const { t } = useI18n();
  return (
    <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 md:grid-cols-4">
      {folders.map((b) => (
        <button key={b.id} onClick={() => onOpen(b)} className="group text-left" title={b.title}>
          <div className="flex aspect-video w-full items-center justify-center rounded-xl bg-gradient-to-br from-ink-700 to-ink-850 transition duration-200 group-hover:from-ink-600 group-hover:to-ink-800">
            <Bookmark className="h-8 w-8 text-gold-300/70" />
          </div>
          <p className="mt-1.5 truncate text-sm font-medium text-slate-200">{b.title}</p>
          <p className="text-[11px] text-slate-500">{t("{n} titles", { n: b.count })}</p>
        </button>
      ))}
    </div>
  );
}

function CollectionsGrid({
  collections,
  onOpen,
}: {
  collections: DiscoverCollection[];
  onOpen: (c: DiscoverCollection) => void;
}) {
  return (
    <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 md:grid-cols-4">
      {collections.map((c) => (
        <button key={c.id} onClick={() => onOpen(c)} className="group text-left" title={c.title}>
          <div className="relative overflow-hidden rounded-xl bg-gradient-to-br from-ink-700 to-ink-850">
            <img
              src={imgURL(c.poster)}
              alt={c.title}
              loading="lazy"
              className="aspect-video w-full object-cover transition duration-200 group-hover:scale-[1.03]"
              onError={(e) => ((e.currentTarget as HTMLImageElement).style.visibility = "hidden")}
            />
          </div>
          <p className="mt-1.5 truncate text-sm font-medium text-slate-200">{c.title}</p>
        </button>
      ))}
    </div>
  );
}
