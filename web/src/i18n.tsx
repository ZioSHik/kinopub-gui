import { createContext, useContext, useMemo, useState, type ReactNode } from "react";

export type Lang = "en" | "ru";

// Russian translations keyed by the English source string. Missing keys fall
// back to English, so the app always renders. Use {name} placeholders for
// interpolation via t(key, { name: value }).
const RU: Record<string, string> = {
  // Brand / nav
  downloader: "загрузчик",
  Download: "Загрузка",
  Queue: "Очередь",
  Library: "Библиотека",
  Doctor: "Доктор",
  Settings: "Настройки",
  Live: "В сети",
  "Reconnecting…": "Переподключение…",
  "App connected": "Приложение подключено",
  "Reconnecting to app…": "Переподключение к приложению…",
  "Signed in": "Вы вошли",
  "Sign in": "Войти",
  "Expand sidebar": "Развернуть панель",
  "Collapse sidebar": "Свернуть панель",
  "{n} days left": "осталось {n} дн.",
  "No subscription": "Нет подписки",
  "Checking subscription…": "Проверка подписки…",
  "Can't reach kino.pub": "Нет связи с kino.pub",
  "ffmpeg ready": "ffmpeg готов",
  "ffmpeg missing": "ffmpeg не найден",
  connected: "подключено",
  reconnecting: "переподключение",

  // Auth gate / panel
  "kino.pub downloader": "kino.pub загрузчик",
  "Sign in to kino.pub to continue": "Войдите в kino.pub, чтобы продолжить",
  "kino.pub authentication": "Авторизация kino.pub",
  Logout: "Выйти",
  "Credentials saved": "Данные сохранены",
  "Login failed": "Не удалось войти",
  "Logged out": "Вы вышли",
  "Logout failed": "Не удалось выйти",

  // Download page (advanced — reached from the Queue; Catalog is the main flow)
  "Advanced download": "Продвинутая загрузка",
  "Download by a kino.pub link": "Загрузка по ссылке kino.pub",
  "Paste a kino.pub link to download it directly. The Catalog is the main way to find titles.":
    "Вставьте ссылку kino.pub, чтобы скачать напрямую. Основной способ искать тайтлы — Каталог.",
  "kino.pub link": "Ссылка kino.pub",
  Preview: "Предпросмотр",
  Quality: "Качество",
  "Auto (highest)": "Авто (макс.)",
  "Output folder": "Папка загрузки",
  "Choose…": "Выбрать…",
  "Advanced options": "Дополнительно",
  Container: "Контейнер",
  "MKV (best multi-audio)": "MKV (лучшее для много-аудио)",
  "Audio tracks": "Аудиодорожки",
  'e.g. "anilibria,!jpn" — patterns; "!"=exclude': 'напр. "anilibria,!jpn" — шаблоны; "!"=исключить',
  all: "все",
  Seasons: "Сезоны",
  "e.g. 1,3-5 — or use the browser below": "напр. 1,3-5 — или отметьте ниже",
  Episodes: "Эпизоды",
  "e.g. 1,3-5": "напр. 1,3-5",
  Concurrency: "Параллельно",
  "parallel downloads (1–16)": "параллельных загрузок (1–16)",
  Retries: "Повторы",
  "Min interval (ms)": "Мин. интервал (мс)",
  "throttle requests (0–60000)": "ограничение запросов (0–60000)",
  "Max simultaneous downloads": "Макс. одновременных загрузок",
  "0 = no limit. When set, extra downloads wait in a queue you can reorder.":
    "0 = без лимита. Если задать — лишние загрузки встают в очередь, которую можно менять местами.",
  Proxy: "Прокси",
  "http / https / socks5": "http / https / socks5",
  "Interactive audio menu": "Интерактивный выбор аудио",
  "Pick tracks before downloading": "Выбрать дорожки перед загрузкой",
  "Force re-download": "Перекачать заново",
  "Ignore completed state": "Игнорировать «уже скачано»",
  "No chunked download": "Без chunked-загрузки",
  "Stream everything via ffmpeg": "Всё через ffmpeg",
  "Verbose logs": "Подробные логи",
  "Show debug-level log lines": "Показывать debug-логи",
  "Extra ffmpeg args": "Доп. аргументы ffmpeg",
  'advanced — e.g. "-c:v libx265 -crf 28"': 'продвинутое — напр. "-c:v libx265 -crf 28"',
  "Leave empty to use saved credentials": "Пусто — использовать сохранённые данные",
  "Start download": "Начать загрузку",
  "ffmpeg not detected — required to download": "ffmpeg не найден — нужен для загрузки",
  "Enter a kino.pub URL first": "Сначала введите ссылку kino.pub",
  "Preview failed": "Не удалось получить предпросмотр",
  'Resolved “{title}” · {n} episodes': "Найдено «{title}» · эпизодов: {n}",
  "Download started": "Загрузка запущена",
  "Failed to start": "Не удалось запустить",
  "ffmpeg not found — install it to download": "ffmpeg не найден — установите его для загрузки",

  // VPN / timeout reminder
  "kino.pub is often unavailable without a VPN. If requests hang or time out, enable a VPN or set a proxy below.":
    "kino.pub часто недоступен без VPN. Если запросы зависают или истекает таймаут — включите VPN или укажите прокси ниже.",
  "Request timed out — kino.pub may be unreachable without a VPN. Enable a VPN or set a proxy, then retry.":
    "Истёк таймаут — kino.pub может быть недоступен без VPN. Включите VPN или укажите прокси и повторите.",

  // Series browser
  "{n} episodes": "эпизодов: {n}",
  "{n} to download": "к загрузке: {n}",
  "{n} done": "готово: {n}",
  "Season {n}": "Сезон {n}",
  "{n} ep": "{n} эп.",
  "{n} done ": "{n} готово",
  "Episode {n}": "Эпизод {n}",
  queued: "в очереди",
  skip: "пропуск",
  done: "готово",

  // Queue
  "{n} active · {m} finished": "{n} активных · {m} завершённых",
  "Clear finished": "Очистить завершённые",
  "No downloads yet": "Пока нет загрузок",
  "Start a download and live progress for every episode shows up here.":
    "Запустите загрузку — живой прогресс по каждому эпизоду появится здесь.",
  Finished: "Завершённые",
  "Cleared {n} finished jobs": "Очищено завершённых: {n}",

  // Job card / statuses
  Queued: "В очереди",
  Resolving: "Получение",
  Downloading: "Загрузка",
  Completed: "Готово",
  Failed: "Ошибка",
  Canceled: "Отменено",
  Paused: "На паузе",
  "dry-run": "проверка",
  "{done}/{total} episodes": "{done}/{total} эпизодов",
  "Resolving source…": "Получение источника…",
  "Preparing…": "Подготовка…",
  "{ok} ok · {failed} failed · {skipped} skipped": "{ok} ок · {failed} ошибок · {skipped} пропущено",
  "Episodes ({n})": "Эпизоды ({n})",
  Log: "Лог",
  Stop: "Стоп",
  Remove: "Удалить",
  Retry: "Повторить",
  Resume: "Продолжить",
  paused: "на паузе",
  "Pause this episode — hold it in the queue": "Поставить серию на паузу — удержать в очереди",
  "Resume this episode": "Продолжить эту серию",
  "{ep} paused": "{ep} на паузе",
  "{ep} resumed": "{ep} продолжается",
  "Paused — progress is kept": "Пауза — прогресс сохранён",
  "Resuming — continuing where it stopped…": "Продолжаю с места остановки…",
  "Retrying — re-downloading what failed…": "Повтор — докачиваю то, что не удалось…",
  "Retrying {ep} — re-downloading…": "Повтор {ep} — докачиваю…",
  "Retry this episode now — without waiting for the rest": "Повторить эту серию сейчас — не дожидаясь остальных",
  Next: "Раньше",
  Prioritize: "В начало",
  "Download this episode next": "Скачать эту серию следующей",
  "{ep} moved to the front — downloading next": "{ep} — в начало очереди, качаю следующей",
  "Moved to the front of the queue": "Перемещено в начало очереди",
  "Stopping job…": "Останавливаю…",
  "retrying (attempt {n})": "повтор (попытка {n})",

  // Library
  "Downloads found in your output folders": "Загрузки из ваших папок",
  Rescan: "Пересканировать",
  "Nothing downloaded yet": "Пока ничего не скачано",
  "Nothing matches the filters": "Ничего не найдено по фильтрам",
  "{n} missing": "нет файлов: {n}",
  "Scan failed": "Сканирование не удалось",
  Movie: "Фильм",
  Serial: "Сериал",
  "All genres": "Все жанры",
  "Recently added": "Сначала новые",
  "Name (A–Z)": "Название (А–Я)",
  "Largest first": "Сначала большие",

  // Doctor
  "Verify downloaded files against the state file and repair inconsistencies.":
    "Сверка скачанных файлов со state-файлом и восстановление целостности.",
  "Folder to check": "Папка для проверки",
  "Repair (--fix)": "Восстановить (--fix)",
  "Remove broken entries & files": "Удалить битые записи и файлы",
  "Clean .tmp": "Очистить .tmp",
  "Delete orphan temp files": "Удалить осиротевшие temp-файлы",
  "Run doctor": "Запустить доктор",
  "In state": "В state",
  Healthy: "Целых",
  Issues: "Проблемы",
  "Series:": "Сериал:",
  "All files are consistent with the state file.": "Все файлы соответствуют state-файлу.",
  "State repaired — run the download again to re-fetch affected episodes.":
    "State восстановлен — запустите загрузку снова, чтобы перекачать затронутые эпизоды.",
  "State repaired": "State восстановлен",
  "All files consistent": "Все файлы целы",
  "{n} issue(s) found": "найдено проблем: {n}",
  "Doctor failed": "Доктор завершился с ошибкой",
  "Missing file": "Файл отсутствует",
  Truncated: "Обрезан",
  "Size mismatch": "Размер не совпал",
  "Incomplete record": "Неполная запись",
  "Orphan .tmp": "Осиротевший .tmp",

  // Settings
  "Defaults applied to every new download.": "Значения по умолчанию для новых загрузок.",
  "Default output folder": "Папка загрузки по умолчанию",
  "Default quality": "Качество по умолчанию",
  "No chunked download by default": "Без chunked-загрузки по умолчанию",
  "Stream everything through ffmpeg": "Всё через ffmpeg",
  "Extra library folders": "Доп. папки библиотеки",
  "Scanned in addition to the output folder.": "Сканируются вдобавок к папке загрузки.",
  Add: "Добавить",
  "None added.": "Не добавлены.",
  System: "Система",
  "not found on PATH": "не найден в PATH",
  "Save settings": "Сохранить настройки",
  "Settings saved": "Настройки сохранены",
  "Save failed": "Не удалось сохранить",

  // Audio menu
  "Choose audio tracks": "Выбор аудиодорожек",
  "Pick which dubs/languages to keep. Your choice is generalized across episodes, so a dub missing from some episode falls back to the same language. No choice within the timer keeps every track.":
    "Выберите озвучки/языки. Выбор обобщается на все серии: если озвучки нет в какой-то серии — берётся другая на том же языке. Без выбора за таймер останутся все дорожки.",
  "Keep all": "Оставить все",
  "Download selected ({n})": "Скачать выбранные ({n})",
  "Failed to submit selection": "Не удалось отправить выбор",
  "{n} of {m} selected": "выбрано {n} из {m}",
  "Select all": "Выбрать все",
  "Deselect all": "Снять все",
  "Only missing": "Только новые",
  "Select only episodes not yet downloaded": "Выбрать только ещё не скачанные серии",
  Downloaded: "Скачано",
  "Downloaded · {res}": "Скачано · {res}",
  "Your last voiceover isn't available here — pick another.":
    "Прошлой озвучки здесь нет — выберите другую.",
  "Only this": "Только эту",
  "Start download ({n})": "Скачать ({n})",
  "Toggle season": "Выбрать сезон",
  "re-attempts per episode after a network error (timeout, reset, 5xx)":
    "повторные попытки на серию при сетевой ошибке (таймаут, сброс, 5xx)",
  "Install ffmpeg": "Установить ffmpeg",
  "Installing ffmpeg…": "Установка ffmpeg…",
  "ffmpeg installed.": "ffmpeg установлен.",
  "ffmpeg install failed": "Не удалось установить ffmpeg",
  "Downloading a static build — this can take a minute.":
    "Скачивается статичная сборка — это может занять минуту.",
  "Downloads a static build from {src}.": "Скачивает статичную сборку из {src}.",
  "Software update": "Обновление",
  "Current version": "Текущая версия",
  "A new version is available": "Доступна новая версия",
  "Update {v}": "Обновить {v}",
  "Update": "Обновить",
  "New version {v} available": "Доступна новая версия {v}",
  "Release notes": "Список изменений",
  "Update & restart": "Обновить и перезапустить",
  "Updating…": "Обновление…",
  "Check for updates": "Проверить обновления",
  "You're on the latest version.": "У вас последняя версия.",
  "Update failed": "Не удалось обновить",
  "Updating to {v} — the app will restart and this tab will reconnect.":
    "Обновление до {v} — приложение перезапустится, и эта вкладка переподключится.",
  "Delete": "Удалить",
  "Delete failed": "Не удалось удалить",
  "Deleted “{title}”": "Удалено «{title}»",
  "Delete “{title}” and all its files from disk? This cannot be undone.":
    "Удалить «{title}» и все его файлы с диска? Это нельзя отменить.",
  "Delete this episode from disk": "Удалить эту серию с диска",
  "Deleted {label}": "Удалено {label}",
  "Delete episode {label} from disk? This frees its space and cannot be undone.":
    "Удалить серию {label} с диска? Это освободит место и необратимо.",

  // Dir picker
  "Choose a folder": "Выбор папки",
  "Parent folder": "Родительская папка",
  "Files download into this folder.": "Файлы скачиваются в эту папку.",
  "Use this folder": "Выбрать эту папку",
  "No sub-folders here.": "Здесь нет подпапок.",

  // Misc
  Cancel: "Отмена",
  started: "начато",
  created: "создано",

  // Time
  "just now": "только что",
  "{n}m ago": "{n} мин назад",
  "{n}h ago": "{n} ч назад",
  "{n}d ago": "{n} дн назад",
  "{n}s": "{n} с",
  "{m}m {s}s": "{m} м {s} с",
  "{h}h {m}m": "{h} ч {m} м",
  "{m}m": "{m} м",
  ETA: "Осталось",

  // Library file actions
  Open: "Открыть",
  "Open folder": "Открыть папку",
  "Reveal in folder": "Показать в папке",
  "Opening…": "Открываю…",
  "Could not open": "Не удалось открыть",
  "File not found": "Файл не найден",

  // kino.pub API login (Settings)
  "kino.pub account (API)": "Аккаунт kino.pub (API)",
  "Sign in once with a device code to search the catalog, preview voiceovers, and download titles.":
    "Войдите один раз по коду устройства — поиск по каталогу, выбор озвучки и загрузка.",
  "Signed in to kino.pub": "Вы вошли в kino.pub",
  "Open the link and enter this code:": "Откройте ссылку и введите код:",
  "Waiting for confirmation…": "Ожидание подтверждения…",
  "Sign in to kino.pub": "Войти в kino.pub",
  "Enter the code on kino.pub/device to finish signing in":
    "Введите код на kino.pub/device, чтобы завершить вход",

  // Catalog (Discover)
  Catalog: "Каталог",
  "Search kino.pub, browse tops and collections, preview voiceovers — and download in one click.":
    "Ищите на kino.pub, смотрите топы и подборки, оценивайте озвучки — и качайте в один клик.",
  "Search films and series on kino.pub…": "Поиск фильмов и сериалов на kino.pub…",
  Popular: "Популярное",
  Hot: "Горячее",
  Fresh: "Новое",
  Collections: "Подборки",
  Search: "Поиск",
  All: "Все",
  Movies: "Фильмы",
  Series: "Сериалы",
  "Nothing found.": "Ничего не найдено.",
  "Load more": "Показать ещё",
  "Catalog request failed": "Не удалось загрузить каталог",
  "Couldn't reach kino.pub": "Не удалось подключиться к kino.pub",
  "If kino.pub is blocked in your region, enable a VPN (or set a proxy in Settings), then try again.":
    "Если kino.pub заблокирован в вашем регионе, включите VPN (или укажите прокси в Настройках) и повторите.",
  "Sign in to kino.pub to browse the catalog": "Войдите в kino.pub, чтобы открыть каталог",
  "The catalog, search, voiceovers and one-click downloads use the official kino.pub API. Sign in once in Settings.":
    "Каталог, поиск, озвучки и загрузка в один клик работают через официальное API kino.pub. Войдите один раз в Настройках.",
  "Go to Settings": "Перейти в Настройки",

  // Title detail
  "Loading…": "Загрузка…",
  Title: "Тайтл",
  min: "мин",
  Voiceover: "Озвучка",
  "(all tracks)": "(все дорожки)",
  "(all selected)": "(выбраны все)",
  "(none)": "(ничего)",
  "Select at least one voiceover": "Выберите хотя бы одну озвучку",
  "({n} selected)": "(выбрано: {n})",
  "Voiceover list appears after sign-in / for available titles.":
    "Список озвучек появляется после входа / для доступных тайтлов.",
  "Download ({n})": "Скачать ({n})",
  "Select at least one episode": "Выберите хотя бы один эпизод",
  Similar: "Похожее",

  // Catalog v2 — tabs, filter, collections, history
  "Search kino.pub, browse tops, collections and history, preview voiceovers — and download in one click.":
    "Ищите на kino.pub, смотрите топы, подборки и историю, оценивайте озвучки — и качайте в один клик.",
  Browse: "Обзор",
  History: "История",
  "I'm watching": "Я смотрю",
  Bookmarks: "Закладки",
  "{n} titles": "{n} тайтлов",
  Clear: "Очистить",
  New: "Новые",
  "Most watched": "Просматриваемые",
  Categories: "Категории",
  Subscriptions: "Подписки",
  Filter: "Фильтр",
  Type: "Тип",
  Genre: "Жанр",
  Country: "Страна",
  Sort: "Сортировка",
  Any: "Любой",
  "4K": "4K",
  Concerts: "Концерты",
  Documentary: "Документальное",
  "TV shows": "ТВ-шоу",
  // Catalog categories (kino.pub's category sidebar) + their genre row
  Anime: "Аниме",
  Documentaries: "Докуфильмы",
  Docuseries: "Докусериалы",
  Sport: "Спорт",
  Genres: "Жанры",
  "By update": "По обновлению",
  "KP rating": "Рейтинг КП",
  "IMDb rating": "Рейтинг IMDb",
  Year: "Год",
  "Release year": "Год выхода",
  "Kinopoisk rating": "Рейтинг Кинопоиска",
  "AC3 sound": "Звук AC3",
  "With subtitles": "С субтитрами",
  "Reset filters": "Сбросить фильтры",
  Director: "Режиссёр",
  Cast: "В ролях",
  "Open card": "Открыть карточку",
  "Season {s}. Episode {e}": "Сезон {s}. Эпизод {e}",
  Watched: "Просмотрено",

  // Player
  Watch: "Смотреть",
  Player: "Плеер",
  Close: "Закрыть",
  "Your browser can’t play HLS video.": "Ваш браузер не умеет воспроизводить HLS-видео.",
  "Failed to load stream": "Не удалось загрузить поток",
  "Playback error — try reopening.": "Ошибка воспроизведения — откройте заново.",
  "Previous episode": "Предыдущая серия",
  "Next episode": "Следующая серия",
  "Back {n}s": "Назад {n} с",
  "Forward {n}s": "Вперёд {n} с",
  "Audio track": "Аудиодорожка",
  Auto: "Авто",
  Play: "Воспроизвести",
  Pause: "Пауза",
  Mute: "Без звука",
  Unmute: "Включить звук",
  Seek: "Перемотка",
  Volume: "Громкость",
  Fullscreen: "На весь экран",
  "Exit fullscreen": "Выйти из полноэкранного",
  "Continue watching?": "Продолжить просмотр?",
  "You stopped at {time}": "Вы остановились на {time}",
  "Continue from {time}": "Продолжить с {time}",
  "Start over": "Сначала",
};

interface I18nValue {
  lang: Lang;
  setLang: (l: Lang) => void;
  t: (key: string, vars?: Record<string, string | number>) => string;
}

const I18nCtx = createContext<I18nValue | null>(null);

function detectLang(): Lang {
  const saved = localStorage.getItem("kinopub.lang");
  if (saved === "ru" || saved === "en") return saved;
  return navigator.language?.toLowerCase().startsWith("ru") ? "ru" : "en";
}

export function I18nProvider({ children }: { children: ReactNode }) {
  const [lang, setLangState] = useState<Lang>(() => detectLang());

  const setLang = (l: Lang) => {
    localStorage.setItem("kinopub.lang", l);
    document.documentElement.lang = l;
    setLangState(l);
  };

  const value = useMemo<I18nValue>(() => {
    const t = (key: string, vars?: Record<string, string | number>) => {
      let out = lang === "ru" ? RU[key] ?? key : key;
      if (vars) {
        for (const k of Object.keys(vars)) {
          out = out.split(`{${k}}`).join(String(vars[k]));
        }
      }
      return out;
    };
    return { lang, setLang, t };
  }, [lang]);

  return <I18nCtx.Provider value={value}>{children}</I18nCtx.Provider>;
}

export function useI18n(): I18nValue {
  const ctx = useContext(I18nCtx);
  if (!ctx) throw new Error("useI18n must be used within I18nProvider");
  return ctx;
}

// looksLikeTimeout reports whether an error message indicates a network timeout
// (commonly caused by kino.pub being unreachable without a VPN).
export function looksLikeTimeout(msg?: string): boolean {
  if (!msg) return false;
  const m = msg.toLowerCase();
  return (
    m.includes("deadline exceeded") ||
    m.includes("timeout") ||
    m.includes("timed out") ||
    m.includes("context deadline") ||
    m.includes("no such host") ||
    m.includes("i/o timeout")
  );
}
