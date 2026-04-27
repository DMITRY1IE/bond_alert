# Technical README

Этот файл описывает внутреннюю архитектуру `bond_alert_gin`: какие компоненты за что отвечают, как проходит полный цикл обработки новости и где смотреть при отладке.

## 1) Архитектура

Сервис состоит из 4 основных контуров:

- **Telegram контур**: обработка команд пользователя (`/start`, `/add`, `/list`, `/remove`).
- **Планировщик**: периодический запуск парсинга и рассылки.
- **Интеграции**: MOEX (поиск облигаций), OpenRouter (сентимент), сайты новостей (Smart-Lab, Finam).
- **Persistence**: PostgreSQL (пользователи, подписки, новости, доставка).

Точка входа: `cmd/server/main.go`.

## 2) Поток данных (end-to-end)

### 2.1 Подписка пользователя

1. Пользователь отправляет `/add <ISIN|SECID>`.
2. `internal/telegram/telegram.go` валидирует ввод через `internal/validator`.
3. Проверка доступа по `TELEGRAM_ALLOWED_USER_IDS` (если задан).
4. Вызывается `internal/moex.ResolveBond(...)`:
   - поиск бумаги в ISS;
   - выбор облигационного инструмента;
   - добор деталей по `secid` (ISIN/имя/board).
5. `internal/store` делает `UpsertBond(...)` и активирует подписку `SetSubscriptionActive(...)`.

### 2.2 Фоновый цикл парсинга

1. Cron из `main.go` запускает `jobs.RunParsingCycle(...)` раз в `PARSING_INTERVAL_MINUTES`.
2. `jobs` получает список облигаций с активными подписками.
3. Для каждой бумаги:
   - `parser.Collect(...)` тянет новости из:
     - Smart-Lab (UTF-8, парсит дату из текста);
     - Finam News (windows-1251, текущая дата).
   - поиск по ISIN, тикеру, названию, эмитенту;
   - дедуп по URL.
4. Для каждой новой новости:
   - если summary короткий, догружается тело статьи `FetchArticleBody(...)`;
   - вызывается OpenRouter `AnalyzeSentimentOrNeutral(...)`;
   - сохраняется запись в `news` с датой публикации.
5. `notifier.Deliver(...)` отправляет сообщение всем активным подписчикам.
6. В `news_delivery` фиксируется факт отправки.

## 3) Карта модулей

- `cmd/server/main.go`  
  Инициализация приложения, БД, Telegram режима (webhook/polling), cron, Gin HTTP server, graceful shutdown.

- `internal/config/config.go`  
  Загрузка env-переменных, дефолты, парсинг списка разрешённых пользователей.

- `internal/app/app.go`  
  DI-контейнер: `Store`, `OpenRouter client`, `Telegram BotAPI`, конфиг.

- `internal/db/db.go`  
  Создание `pgxpool`.

- `internal/store/store.go`  
  Все SQL-операции: пользователи, облигации, подписки, новости, доставка.

- `internal/telegram/telegram.go`  
  Long polling, проверка доступа, обработчики команд. После `/start` показывает пример новости.

- `internal/httpserver/httpserver.go`  
  Gin-роуты: `/`, `/health`, `/telegram/webhook`.

- `internal/moex/resolve.go`  
  Резолв ISIN/тикера через MOEX ISS. Rate limiting 100ms между запросами.

- `internal/parser/parser.go`  
  Парсинг SmartLab и Finam с rate limiting 100ms. Декодирование windows-1251 для Finam.

- `internal/openrouter/openrouter.go`  
  Вызов `chat/completions`, извлечение JSON из ответа, нормализация сентимента.

- `internal/jobs/jobs.go`  
  Координация pipeline сбора/анализа/рассылки.

- `internal/notifier/notifier.go`  
  Формат Telegram-сообщения (включая дату) и отправка подписчикам.

- `migrations/001_init.sql`  
  Начальная схема базы данных.

## 4) База данных

Основные таблицы:

- `users` — Telegram пользователи
- `bonds` — облигации (ISIN, тикер, название, эмитент)
- `subscriptions` — many-to-many users/bonds + `is_active`
- `news` — новости (URL уникален, сентимент, reason, дата публикации)
- `news_delivery` — кому и когда отправлено

## 5) Источники новостей

| Источник | URL | Кодировка | Дата |
|----------|-----|-----------|------|
| SmartLab | `https://smart-lab.ru/blog/news/` | UTF-8 | Парсится из текста |
| Finam | `https://bonds.finam.ru/news/today/` | windows-1251 | Текущая дата |

### Ключевые слова для поиска

- ISIN облигации (например, `SU26233RMFS5`)
- Тикер SECID
- Название облигации (слова >= 3 символов)
- Эмитент (компания-выпуск)

## 6) Режимы Telegram

- **Long polling**: если `TELEGRAM_WEBHOOK_URL` пустой.
- **Webhook**: если `TELEGRAM_WEBHOOK_URL` задан.

## 7) Ограничение доступа

Переменная `TELEGRAM_ALLOWED_USER_IDS` — список Telegram ID через запятую.
Если пусто — доступ для всех.

## 8) Переменные окружения

### Обязательные

- `TELEGRAM_BOT_TOKEN` — токен от @BotFather
- `OPENROUTER_API_KEY` — ключ OpenRouter

### Опциональные

| Переменная | По умолчанию | Описание |
|------------|--------------|----------|
| `TELEGRAM_ALLOWED_USER_IDS` | "" | Разрешённые ID через запятую |
| `TELEGRAM_WEBHOOK_URL` | "" | URL для webhook |
| `TELEGRAM_WEBHOOK_SECRET` | "" | Секрет webhook |
| `PARSING_INTERVAL_MINUTES` | 30 | Интервал парсинга |
| `OPENROUTER_MODEL` | inclusionai/ling-2.6-flash:free | Модель LLM |
| `LOG_LEVEL` | INFO | Уровень логирования |
| `LISTEN_ADDR` | :8000 | Адрес HTTP сервера |

## 9) Docker runtime

Запуск одной командой:

```bash
docker compose up --build -d
```

- `Dockerfile` собирает бинарник и запускает `/server`
- `scripts/docker-entrypoint.sh`:
  1. ждёт доступность Postgres (`pg_isready`);
  2. накатывает `migrations/001_init.sql`;
  3. запускает сервер
- `docker-compose.yml` поднимает `postgres` + `bot`

## 10) Rate Limiting

- MOEX ISS: 100ms между запросами (max ~10 req/s)
- Parser: 100ms между запросами к внешним сайтам

## 11) Отладка

### Нет новостей

1. Проверить логи: `docker compose logs bot`
2. Проверить ключевые слова — длина >= 3 символов
3. Проверить что облигация в подписках: `/list` в боте

### Доступ запрещён

Проверить `TELEGRAM_ALLOWED_USER_IDS` в .env и пересоздать контейнер.

### OpenRouter ошибки

- Проверить `OPENROUTER_API_KEY`
- Проверить баланс на openrouter.ai

## 12) Smoke test

```bash
# 1. Запуск
docker compose up --build -d

# 2. Health check
curl http://localhost:8000/health
# {"status":"ok"}

# 3. Telegram
/start
/add SU26233RMFS5
/list

# 4. Проверить логи парсинга
docker compose logs bot | grep -i "jobs:"
```
