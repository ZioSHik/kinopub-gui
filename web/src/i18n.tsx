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
  "Signed in": "Вы вошли",
  "Sign in": "Войти",
  "ffmpeg ready": "ffmpeg готов",
  "ffmpeg missing": "ffmpeg не найден",
  connected: "подключено",
  reconnecting: "переподключение",

  // Auth gate / panel
  "kino.pub downloader": "kino.pub загрузчик",
  "Sign in to kino.pub to continue": "Войдите в kino.pub, чтобы продолжить",
  "kino.pub authentication": "Авторизация kino.pub",
  Logout: "Выйти",
  "Cookie {preview} · {n} keys": "Cookie {preview} · ключей: {n}",
  "Why this is needed": "Зачем это нужно",
  "kino.pub sits behind Cloudflare. Paste the Cookie header from a logged-in browser session (DevTools → Network → request headers), or auto-import it from a browser below. The User-Agent must match the browser that issued the cookies.":
    "kino.pub за Cloudflare. Вставьте заголовок Cookie из залогиненной сессии браузера (DevTools → Network → заголовки запроса) или импортируйте из браузера ниже. User-Agent должен совпадать с браузером, выдавшим куки.",
  "Cookie header": "Заголовок Cookie",
  "e.g. cf_clearance=…; _identity=…; PHPSESSID=…": "напр. cf_clearance=…; _identity=…; PHPSESSID=…",
  "User-Agent": "User-Agent",
  "Pre-filled with this browser's UA. It must match the browser the cookies came from — for browser import, open this app in that same browser.":
    "Подставлен UA текущего браузера. Он должен совпадать с браузером, откуда куки — для импорта открывайте приложение в том же браузере.",
  "Save cookie": "Сохранить cookie",
  "Or import from a browser": "Или импорт из браузера",
  "Tip: open this app in the same browser you import from — the User-Agent above then matches the imported cf_clearance, which Cloudflare requires. On macOS, allow Keychain access when prompted (Yandex/Chrome) or grant Full Disk Access (Safari).":
    "Совет: открывайте приложение в том же браузере, откуда импортируете — тогда User-Agent совпадёт с импортированным cf_clearance, который требует Cloudflare. На macOS разрешите доступ к Keychain (Yandex/Chrome) или дайте Full Disk Access (Safari).",
  "Credentials saved": "Данные сохранены",
  "Login failed": "Не удалось войти",
  "Logged out": "Вы вышли",
  "Logout failed": "Не удалось выйти",

  // Download page
  "New download": "Новая загрузка",
  "Paste a kino.pub page link, a podcast feed link, or a local feed file.":
    "Вставьте ссылку на страницу kino.pub, на podcast-feed или локальный feed-файл.",
  "kino.pub URL or feed": "Ссылка kino.pub или feed",
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
  "Local feed file": "Локальный feed-файл",
  "path to a saved RSS/XML feed (optional)": "путь к сохранённому RSS/XML (необязательно)",
  "One-off cookie override": "Разовое переопределение cookie",
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
  "New download ": "Новая загрузка",
  Finished: "Завершённые",
  "Cleared {n} finished jobs": "Очищено завершённых: {n}",

  // Job card / statuses
  Queued: "В очереди",
  Resolving: "Получение",
  Downloading: "Загрузка",
  Completed: "Готово",
  Failed: "Ошибка",
  Canceled: "Отменено",
  "dry-run": "проверка",
  "{done}/{total} episodes": "{done}/{total} эпизодов",
  "Resolving source…": "Получение источника…",
  "Preparing…": "Подготовка…",
  "{ok} ok · {failed} failed · {skipped} skipped": "{ok} ок · {failed} ошибок · {skipped} пропущено",
  "Episodes ({n})": "Эпизоды ({n})",
  Log: "Лог",
  Stop: "Стоп",
  Remove: "Удалить",
  "Stopping job…": "Останавливаю…",
  "retrying (attempt {n})": "повтор (попытка {n})",

  // Library
  "Downloads found in your output folders": "Загрузки из ваших папок",
  Rescan: "Пересканировать",
  "Nothing downloaded yet": "Пока ничего не скачано",
  "{n} missing": "нет файлов: {n}",
  "Scan failed": "Сканирование не удалось",

  // Doctor
  "Verify downloaded files against the state file and repair inconsistencies.":
    "Сверка скачанных файлов со state-файлом и восстановление целостности.",
  "Folder to check": "Папка для проверки",
  "Repair (--fix)": "Восстановить (--fix)",
  "Remove broken entries & files": "Удалить битые записи и файлы",
  "Clean .tmp": "Очистить .tmp",
  "Delete orphan temp files": "Удалить осиротевшие temp-файлы",
  "Skip probe": "Без сверки длительности",
  "Faster, no network": "Быстрее, без сети",
  "Run doctor": "Запустить доктор",
  "In state": "В state",
  Healthy: "Целых",
  Issues: "Проблемы",
  Skipped: "Пропущено",
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
  "Duration mismatch": "Длительность не совпала",

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
  "Only this": "Только эту",

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

  // Sign-in hint (Download page)
  "You're not signed in. Page links (/item/view/…) need kino.pub cookies. Direct podcast feeds, local feed files and the Library work without signing in.":
    "Вы не вошли. Ссылки на страницы (/item/view/…) требуют куки kino.pub. Прямые podcast-feed, локальные feed-файлы и Библиотека работают без входа.",

  // Library file actions
  Open: "Открыть",
  "Open folder": "Открыть папку",
  "Reveal in folder": "Показать в папке",
  "Opening…": "Открываю…",
  "Could not open": "Не удалось открыть",
  "File not found": "Файл не найден",
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
