# Technical README

Этот файл описывает внутреннюю архитектуру `bond_alert_gin`: какие компоненты за что отвечают, как проходит полный цикл обработки новости и где смотреть при отладке.

## 1) Архитектура

Сервис состоит из 4 основных контуров:

- **Telegram контур**: обработка команд пользователя (`/start`, `/add`, `/list`, `/remove`).
- **Планировщик**: периодический запуск парсинга и рассылки.
- **Интеграции**: MOEX (поиск облигаций), OpenRouter (сентимент), сайты новостей (Smart-Lab, Finam, RSS).
- **Persistence**: PostgreSQL (пользователи, подписки, новости, доставка).

Точка входа: `cmd/server/main.go`.

## 2) Поток данных (end-to-end)

### 2.1 Подписка пользователя

1. Пользователь отправляет `/add <ISIN|SECID>`.
2. `internal/telegram/telegram.go` валидирует ввод через `internal/validator`.
3. Вызывается `internal/moex.ResolveBond(...)`:
   - поиск бумаги в ISS;
   - выбор облигационного инструмента;
   - добор деталей по `secid` (ISIN/имя/board).
4. `internal/store` делает `UpsertBond(...)` и активирует подписку `SetSubscriptionActive(...)`.

### 2.2 Фоновый цикл парсинга

1. Cron из `main.go` запускает `jobs.RunParsingCycle(...)` раз в `PARSING_INTERVAL_MINUTES`.
2. `jobs` получает список облигаций с активными подписками.
3. Для каждой бумаги:
   - `parser.Collect(...)` тянет новости из:
     - Smart-Lab;
     - bonds.finam.ru;
     - RSS (`RSS_FEED_URLS`).
   - дедуп по URL.
4. Для каждой новой новости:
   - если summary короткий, догружается тело статьи `FetchArticleBody(...)`;
   - вызывается OpenRouter `AnalyzeSentimentOrNeutral(...)`;
   - сохраняется запись в `news`.
5. `notifier.Deliver(...)` отправляет сообщение всем активным подписчикам.
6. В `news_delivery` фиксируется факт отправки.

## 3) Карта модулей

- `cmd/server/main.go`  
  Инициализация приложения, БД, Telegram режима (webhook/polling), cron, Gin HTTP server, graceful shutdown.

- `internal/config/config.go`  
  Загрузка env-переменных, дефолты, разбор RSS и интервала.

- `internal/app/app.go`  
  DI-контейнер: `Store`, `OpenRouter client`, `Telegram BotAPI`, конфиг.

- `internal/db/db.go`  
  Создание `pgxpool`.

- `internal/store/store.go`  
  Все SQL-операции: пользователи, облигации, подписки, новости, доставка.

- `internal/telegram/telegram.go`  
  Long polling и обработчики команд.

- `internal/httpserver/httpserver.go`  
  Gin-роуты: `/`, `/health`, `/telegram/webhook`.

- `internal/moex/resolve.go`  
  Резолв ISIN/тикера через MOEX ISS.

- `internal/parser/parser.go`  
  Парсинг сайтов/лент + доп. извлечение тела новости.

- `internal/openrouter/openrouter.go`  
  Вызов `chat/completions`, извлечение JSON из ответа, нормализация сентимента.

- `internal/jobs/jobs.go`  
  Координация pipeline сбора/анализа/рассылки.

- `internal/notifier/notifier.go`  
  Формат Telegram-сообщения и отправка подписчикам.

- `migrations/001_init.sql`  
  Начальная схема базы данных.

## 4) База данных

Основные таблицы:

- `users`
- `bonds`
- `subscriptions` (many-to-many + `is_active`)
- `news` (URL уникален, хранит сентимент и reason)
- `news_delivery` (кому и когда отправлено)

Схема совпадает с Python-версией по смыслу (MVP).

## 5) Режимы Telegram

- **Long polling**: если `TELEGRAM_WEBHOOK_URL` пустой.
- **Webhook**: если `TELEGRAM_WEBHOOK_URL` задан.

Важно: библиотека `go-telegram-bot-api/v5` (используемая версия) ограничивает удобную установку `secret_token`; входящий секрет проверяется в Gin по заголовку `X-Telegram-Bot-Api-Secret-Token`.

## 6) Переменные окружения (ключевые)

Обязательные:

- `TELEGRAM_BOT_TOKEN`
- `DATABASE_URL`
- `OPENROUTER_API_KEY`

Опциональные:

- `OPENROUTER_BASE_URL`
- `OPENROUTER_MODEL`
- `PARSING_INTERVAL_MINUTES`
- `RSS_FEED_URLS`
- `TELEGRAM_WEBHOOK_URL`
- `TELEGRAM_WEBHOOK_SECRET`
- `TELEGRAM_HTTP_TIMEOUT_SEC`
- `TELEGRAM_GET_UPDATES_TIMEOUT`
- `LISTEN_ADDR`

## 7) Docker runtime

- `Dockerfile` собирает бинарник и запускает `/server`.
- `scripts/docker-entrypoint.sh`:
  1. ждёт доступность Postgres (`pg_isready`);
  2. накатывает `migrations/001_init.sql`;
  3. запускает сервер.
- `docker-compose.yml` поднимает `postgres` + `bot`.

## 8) Отладка и типовые проблемы

- **`missing go.sum entry`**  
  Выполнить `go mod tidy`, закоммитить `go.sum`.

- **Проблемы с Telegram API / timeout**  
  Увеличить `TELEGRAM_HTTP_TIMEOUT_SEC`; проверить сеть из контейнера.

- **Нет новостей из Finam**  
  Возможен антибот/блокировки — это ожидаемо; используйте RSS + Smart-Lab.

- **OpenRouter вернул невалидный текст**  
  Клиент извлекает JSON regexp-ом; при ошибке fallback: `NEUTRAL`.

## 9) Минимальный smoke test

1. `docker compose up --build -d`
2. Проверить `GET /health` -> `{"status":"ok"}`
3. В Telegram:
   - `/start`
   - `/add RU000A101F94`
   - `/list`
4. Подождать один цикл парсинга (`PARSING_INTERVAL_MINUTES`) и проверить доставку.

