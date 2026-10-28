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
  "Offline library": "Офлайн библиотека",
  Doctor: "Доктор",
  Settings: "Настройки",
  Profile: "Профиль",
  Live: "В сети",
  "Reconnecting…": "Переподключение…",
  "App connected": "Приложение подключено",
  "Reconnecting to app…": "Переподключение к приложению…",
  "Signed in": "Вы вошли",
  "Sign in": "Войти",
  "in Profile": "в Профиль",
  "Sign in in Profile": "Войти — в Профиль",
  "Install in Settings": "Установить в Настройках",
  "ffmpeg missing — install it in Settings": "ffmpeg не найден — установите в Настройках",
  "Expand sidebar": "Развернуть панель",
  "Collapse sidebar": "Свернуть панель",
  "{n} days left": "осталось {n} дн.",
  "No subscription": "Нет подписки",
  "Checking subscription…": "Проверка подписки…",
  "Can't reach kino.watch": "Нет связи с kino.watch",
  "ffmpeg ready": "ffmpeg готов",
  "ffmpeg missing": "ffmpeg не найден",
  connected: "подключено",
  reconnecting: "переподключение",

  // Auth gate / panel
  "kino.watch downloader": "kino.watch загрузчик",
  "Sign in to kino.watch to continue": "Войдите в kino.watch, чтобы продолжить",
  "kino.watch authentication": "Авторизация kino.watch",
  Logout: "Выйти",
  "Credentials saved": "Данные сохранены",
  "Login failed": "Не удалось войти",
  "Logged out": "Вы вышли",
  "Logout failed": "Не удалось выйти",

  // Download page (advanced — reached from the Queue; Catalog is the main flow)
  "Advanced download": "Продвинутая загрузка",
  "Download by a kino.watch link": "Загрузка по ссылке kino.watch",
  "Paste a kino.watch link to download it directly. The Catalog is the main way to find titles.":
    "Вставьте ссылку kino.watch, чтобы скачать напрямую. Основной способ искать тайтлы — Каталог.",
  "kino.watch link": "Ссылка kino.watch",
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
  Proxy: "Прокси",
  "http / https / socks5": "http / https / socks5",
  "Interactive audio menu": "Интерактивный выбор аудио",
  "Pick tracks before downloading": "Выбрать дорожки перед загрузкой",
  "Force re-download": "Перекачать заново",
  "Ignore completed state": "Игнорировать «уже скачано»",
  "Extra ffmpeg args": "Доп. аргументы ffmpeg",
  'advanced — e.g. "-c:v libx265 -crf 28"': 'продвинутое — напр. "-c:v libx265 -crf 28"',
  "Leave empty to use saved credentials": "Пусто — использовать сохранённые данные",
  "Start download": "Начать загрузку",
  "ffmpeg not detected — required to download": "ffmpeg не найден — нужен для загрузки",
  "Enter a kino.watch URL first": "Сначала введите ссылку kino.watch",
  "Preview failed": "Не удалось получить предпросмотр",
  'Resolved “{title}” · {n} episodes': "Найдено «{title}» · эпизодов: {n}",
  "Added to the queue": "Добавлено в очередь",
  "In the queue — open": "В очереди — открыть",
  "Already downloaded — open": "Уже скачано — открыть",
  "In the queue": "В очереди",
  "Open the queue": "Открыть очередь",
  "{n} already in the queue": "{n} уже в очереди",
  // Verbatim from the server: the duplicate guard in handleCreateJob.
  "already in the queue": "уже в очереди",
  "Failed to start": "Не удалось запустить",
  "ffmpeg not found — install it to download": "ffmpeg не найден — установите его для загрузки",

  // VPN / timeout reminder
  "kino.watch is often unavailable without a VPN. If requests hang or time out, enable a VPN or set a proxy below.":
    "kino.watch часто недоступен без VPN. Если запросы зависают или истекает таймаут — включите VPN или укажите прокси ниже.",
  "Request timed out — kino.watch may be unreachable without a VPN. Enable a VPN or set a proxy, then retry.":
    "Истёк таймаут — kino.watch может быть недоступен без VPN. Включите VPN или укажите прокси и повторите.",
  "Sign in to kino.watch (Profile) to resolve and download titles.":
    "Войдите в kino.watch (Профиль), чтобы распознавать ссылки и качать тайтлы.",

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
  "{n} of {m} episodes failed": "не удалось скачать эпизодов: {n} из {m}",
  "Episodes ({n})": "Эпизоды ({n})",
  Log: "Лог",
  Remove: "Удалить",
  Retry: "Повторить",
  Resume: "Продолжить",
  paused: "на паузе",
  "Pause this episode — hold it in the queue": "Поставить серию на паузу — удержать в очереди",
  "Resume this episode": "Продолжить эту серию",
  "{ep} paused": "{ep} на паузе",
  "{ep} resumed": "{ep} продолжается",
  "{ep} canceled — the rest keep downloading": "{ep} отменена — остальные качаются дальше",
  "Remove this job and delete the {size} it already downloaded? This cannot be undone.":
    "Удалить задание и стереть уже скачанные {size}? Отменить будет нельзя.",
  "This job holds {size} of partly downloaded data, but another job is still using it — the files stay on disk. Remove the card?":
    "Задание держит {size} недокачанных данных, но их использует другое задание — файлы останутся на диске. Убрать карточку?",
  "Cancel this episode — the rest keep downloading": "Отменить эту серию — остальные продолжат качаться",
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
  "Estimated size — refines as it downloads (HLS has no fixed total)":
    "Оценка размера — уточняется по мере загрузки (у HLS нет фиксированного итога)",

  "Select episodes missing the selected quality or voiceover": "Выбрать серии без нужного качества или озвучки",
  "Voiceover not recorded": "Озвучка не указана",
  "{n} of {m} downloaded episodes match the selected quality and voiceover":
    "Выбранным качеству и озвучке соответствуют {n} из {m} скачанных серий",

  // Library
  "Downloads found in your output folders": "Загрузки из ваших папок",
  Rescan: "Пересканировать",
  "Scanning your folders…": "Сканирую папки…",
  "Nothing downloaded yet": "Пока ничего не скачано",
  "Nothing matches the filters": "Ничего не найдено по фильтрам",
  "{n} missing": "нет файлов: {n}",
  "File missing": "Файл не найден",
  "{n} seasons": "сезонов: {n}",
  "Scan failed": "Сканирование не удалось",
  Movie: "Фильм",
  Serial: "Сериал",
  "On disk": "На диске",
  "{n} downloading · {m} on disk": "{n} качается · {m} на диске",
  "Show actions": "Показать действия",
  "Search by title or genre…": "Поиск по названию или жанру…",
  "Clear search": "Очистить поиск",
  "{n} found": "найдено: {n}",
  "Show only this genre": "Показать только этот жанр",
  "List view": "Списком",
  "Tiles view": "Плиткой",
  "All genres": "Все жанры",
  "Recently added": "Сначала новые",
  "Name (A–Z)": "Название (А–Я)",
  "Largest first": "Сначала большие",

  // Doctor
  "Verify downloaded files against the state file and repair inconsistencies.":
    "Сверка скачанных файлов со state-файлом и восстановление целостности.",
  "Folder to check": "Папка для проверки",
  Repair: "Восстановить",
  "Remove broken entries & files": "Удалить битые записи и файлы",
  "Unfinished downloads are using this folder ({n}) — repair and cleanup are off.":
    "Папку занимают незавершённые загрузки ({n}) — починка и очистка выключены.",
  "Their temp files hold what's already downloaded — cleanup would wipe that progress. Finish or cancel them, or just check the folder without repairing.":
    "Во временных файлах лежит уже скачанное: очистка сотрёт прогресс. Завершите или отмените загрузку — либо просто проверьте папку без починки.",
  "Nothing was changed: downloads in progress are using this folder.":
    "Ничего не изменено: эту папку занимают незавершённые загрузки.",
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
  "Check downloads": "Проверка загрузок",
  "Find missing or broken files and clean up leftovers.":
    "Найти пропавшие и битые файлы, убрать остатки.",
  "Interface language": "Язык интерфейса",
  "Applies to this browser.": "Применяется в этом браузере.",
  "Update available": "Доступно обновление",
  "Defaults applied to every new download.": "Значения по умолчанию для новых загрузок.",
  "Default output folder": "Папка загрузки по умолчанию",
  "Default quality": "Качество по умолчанию",
  "Extra library folders": "Доп. папки библиотеки",
  "Scanned in addition to the output folder.": "Сканируются вдобавок к папке загрузки.",
  Add: "Добавить",
  "None added.": "Не добавлены.",
  System: "Система",
  "not found on PATH": "не найден в PATH",
  "Save settings": "Сохранить настройки",
  "Settings saved": "Настройки сохранены",
  "Save failed": "Не удалось сохранить",
  "Changes are saved automatically.": "Изменения сохраняются автоматически.",
  "Saving…": "Сохранение…",
  Saved: "Сохранено",

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
  "Only missing": "Недостающие",
  "Select only episodes not yet downloaded": "Выбрать только ещё не скачанные серии",
  Downloaded: "Скачано",
  "Downloaded · {details}": "Скачано · {details}",
  "voiceover substituted": "озвучка заменена",
  "Your last voiceover isn't available here — pick another.":
    "Прошлой озвучки здесь нет — выберите другую.",
  "Only this": "Только эту",
  "Start download ({n})": "Скачать ({n})",
  "Toggle season": "Выбрать сезон",
  "Install ffmpeg": "Установить ffmpeg",
  "Installing ffmpeg…": "Установка ffmpeg…",
  "ffmpeg installed.": "ffmpeg установлен.",
  "ffmpeg install failed": "Не удалось установить ffmpeg",
  "Downloading a static build — this can take a minute.":
    "Скачивается статичная сборка — это может занять минуту.",
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
  Folder: "Папка",
  "Reveal in folder": "Показать в папке",
  "Opening…": "Открываю…",
  "Could not open": "Не удалось открыть",
  "File not found": "Файл не найден",

  // Profile page + kino.watch API login
  "Your kino.watch account and subscription.": "Ваш аккаунт kino.watch и подписка.",
  "Subscription ends": "Подписка до",
  "kino.watch account": "Аккаунт kino.watch",
  "Not signed in": "Вы не вошли",
  "kino.watch account (API)": "Аккаунт kino.watch (API)",
  "Sign in once with a device code to search the catalog, preview voiceovers, and download titles.":
    "Войдите один раз по коду устройства — поиск по каталогу, выбор озвучки и загрузка.",
  "Signed in to kino.watch": "Вы вошли в kino.watch",
  "Open the link and enter this code:": "Откройте ссылку и введите код:",
  "Waiting for confirmation…": "Ожидание подтверждения…",
  "Sign in to kino.watch": "Войти в kino.watch",
  "Enter the code on kino.watch/device to finish signing in":
    "Введите код на kino.watch/device, чтобы завершить вход",

  // Catalog (Discover)
  Catalog: "Каталог",
  "Search films and series on kino.watch…": "Поиск фильмов и сериалов на kino.watch…",
  "Showing title matches. Clear the search to browse by category, genre and filters.":
    "Показаны совпадения по названию. Очистите поиск, чтобы фильтровать по категории, жанру и параметрам.",
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
  "Couldn't reach kino.watch": "Не удалось подключиться к kino.watch",
  "If kino.watch is blocked in your region, enable a VPN (or set a proxy in Settings), then try again.":
    "Если kino.watch заблокирован в вашем регионе, включите VPN (или укажите прокси в Настройках) и повторите.",
  "Sign in to kino.watch to browse the catalog": "Войдите в kino.watch, чтобы открыть каталог",
  "The catalog, search, voiceovers and one-click downloads use the official kino.watch API. Sign in once in Profile.":
    "Каталог, поиск, озвучки и загрузка в один клик работают через официальное API kino.watch. Войдите один раз в Профиль.",
  "Go to Profile": "Перейти в Профиль",
  "Go to Settings": "Перейти в Настройки",

  // Title detail
  "Loading…": "Загрузка…",
  Title: "Тайтл",
  min: "мин",
  "TV series": "Сериал",
  "up to {q}": "до {q}",
  "{n} of {m} downloaded": "скачано {n} из {m}",
  "Show more": "Показать полностью",
  "Show less": "Свернуть",
  Voiceover: "Озвучка",
  Dubbing: "Дубляж",
  "Multi-voice translation": "Многоголосый перевод",
  "Two-voice translation": "Двухголосый перевод",
  "Author's translation": "Авторский перевод",
  "Single-voice translation": "Одноголосый перевод",
  "Original soundtrack": "Оригинал",
  Commentary: "Комментарии",
  "Other tracks": "Другие дорожки",
  "Choose a voiceover": "Выберите озвучку",
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

  // Catalog v2 — filter, collections, history
  History: "История",
  "I'm watching": "Я смотрю",
  Bookmarks: "Закладки",
  "{n} titles": "{n} тайтлов",

  // Collections, I'm watching, Bookmarks and History — each its own page in the
  // sidebar now, instead of a chip inside the Catalog.
  "Search kino.watch by title, or narrow the whole library down by category, genre and rating.":
    "Ищите на kino.watch по названию — или сужайте всю библиотеку по категории, жанру и рейтингу.",
  Collection: "Подборка",
  "kino.watch's own curated lists — open one and download straight from it.":
    "Готовые подборки kino.watch — откройте любую и качайте прямо из неё.",
  "Sign in to kino.watch to browse collections": "Войдите в kino.watch, чтобы смотреть подборки",
  "No collections found": "Подборок не нашлось",
  "This collection is empty": "В этой подборке пусто",
  "Series and films you're part-way through, plus the shows you follow.":
    "Сериалы и фильмы, которые вы не досмотрели, и шоу, на которые подписаны.",
  "Sign in to kino.watch to see what you're watching": "Войдите в kino.watch, чтобы увидеть, что вы смотрите",
  "Nothing in progress": "Ничего не начато",
  "Start something on kino.watch and it waits for you here until you finish it.":
    "Начните что-нибудь на kino.watch — оно будет ждать вас здесь, пока не досмотрите.",
  "No subscriptions yet": "Подписок пока нет",
  "Series you subscribe to on kino.watch are listed here.":
    "Сериалы, на которые вы подписались на kino.watch, появятся здесь.",
  "Your kino.watch bookmark folders — open one and download straight from it.":
    "Ваши папки закладок на kino.watch — откройте любую и качайте прямо из неё.",
  "Everything you've watched on kino.watch, newest first — open a title to download it.":
    "Всё, что вы смотрели на kino.watch, начиная с последнего — откройте тайтл, чтобы скачать.",
  "Sign in to kino.watch to see your bookmarks": "Войдите в kino.watch, чтобы увидеть закладки",
  "Sign in to kino.watch to see your watch history": "Войдите в kino.watch, чтобы увидеть историю просмотров",
  "No bookmarks yet": "Закладок пока нет",
  "Folders you create on kino.watch show up here, ready to download from.":
    "Папки, созданные на kino.watch, появятся здесь — сразу готовые к загрузке.",
  "This folder is empty": "В этой папке пусто",
  "Nothing watched yet": "Вы ещё ничего не смотрели",
  "Titles you play on kino.watch — here or anywhere else — show up on this page.":
    "Тайтлы, которые вы включаете на kino.watch — здесь или где угодно — появятся на этой странице.",
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
  // Catalog categories (kino.watch's category sidebar) + their genre row
  Anime: "Аниме",
  Documentaries: "Докуфильмы",
  Docuseries: "Докусериалы",
  Sport: "Спорт",
  Genres: "Жанры",
  "By update": "По обновлению",
  "By rating": "По рейтингу",
  "By views": "По просмотрам",
  "By watchers": "По зрителям",
  // "What's new" — kino.watch's own charts, on their own page
  "What's new": "Новинки",
  "kino.watch's own charts. To browse by genre, rating and year, use the Catalog.":
    "Готовые топы kino.watch. Чтобы фильтровать по жанру, рейтингу и году, откройте Каталог.",
  "Sign in to kino.watch to see what's new": "Войдите в kino.watch, чтобы смотреть новинки",
  "This chart is empty right now": "В этом топе сейчас пусто",
  "IMDb rating": "Рейтинг IMDb",
  Year: "Год",
  "Release year": "Год выхода",
  "Kinopoisk rating": "Рейтинг Кинопоиска",
  "AC3 sound": "Звук AC3",
  "With subtitles": "С субтитрами",
  "Reset filters": "Сбросить фильтры",
  // Catalog filter panel — active-condition chips, country combobox, presets
  "Remove filter": "Убрать фильтр",
  from: "от",
  to: "до",
  Minimum: "минимум",
  Maximum: "максимум",
  "All countries": "Все страны",
  "Search country…": "Поиск страны…",
  "Sound & subtitles": "Звук и субтитры",
  Subtitles: "Субтитры",
  KP: "КП",
  "Last 2 years": "Последние 2 года",
  "2020s": "2020-е",
  "2010s": "2010-е",
  "2000s": "2000-е",
  "1990s": "1990-е",
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
  "Some partial download files could not be deleted — check the download folder":
    "Часть временных файлов не удалось удалить — проверьте папку загрузок",
  "Couldn't scan your folders": "Не удалось просканировать папки",
  "Finished downloads": "Завершённые загрузки",
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

// translate renders a dictionary key in the current language OUTSIDE React —
// for code with no hook context, like the store's SSE event handler. Components
// use useI18n().t instead, which also re-renders on a language switch.
export function translate(key: string): string {
  return detectLang() === "ru" ? RU[key] ?? key : key;
}

// looksLikeCanceled reports whether an error message is really just "the user
// stopped it". The backend already folds cancellations into a plain "canceled",
// but an older job restored from disk can still carry the raw Go chain
// ("segment 10 failed: context canceled"), so match that shape too.
export function looksLikeCanceled(msg?: string): boolean {
  if (!msg) return false;
  const m = msg.toLowerCase();
  return m === "canceled" || m === "cancelled" || m.includes("context canceled");
}

// looksLikeTimeout reports whether an error message indicates a network timeout
// (commonly caused by kino.watch being unreachable without a VPN).
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
