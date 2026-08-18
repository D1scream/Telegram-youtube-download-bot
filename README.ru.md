# Telegram-бот

[English](README.md)

Telegram-бот на Go: скачивание музыки и видео с YouTube, анализ изображений с поиском статей УК РФ через OpenSearch и LLM.

## Возможности

- **YouTube** (`YT_DLP_ENABLED`): скачивание аудио `/ytm`, видео `/ytv`
- **Анализ изображений** (`OPENSEARCH_URL` + `LLM_API`): описание фото через LLM, поиск статей УК РФ в OpenSearch

## Технологии

- **Язык**: Go 1.25
- **API**: Telegram Bot API, yt-dlp, OpenSearch, SiliconFlow LLM, TEI embeddings
- **Библиотеки**: `go-telegram/bot`, `caarlos0/env`, `godotenv`
- **Архитектура**: слои `adapters` → `usecases` → `transport`, DI в `cmd/main.go`
- **Контейнеризация**: Docker, Docker Compose

## Структура проекта
```text
├── cmd/
├── internal/
│   ├── adapters/               # Telegram, OpenSearch, LLM, yt-dlp
│   ├── config/                 # Загрузка конфигурации
│   ├── entities/               # Доменные сущности
│   ├── transport/
│   │   └── telegram/           # Обработчик команд Telegram
│   └── usecases/
│       ├── imageuk/            # Анализ фото и поиск статей
│       └── youtube/            # Скачивание с YouTube
├── docker/
│   └── opensearch/             # Стек OpenSearch для анализа изображений
├── prompts/                    # System prompt для LLM
├── secrets/
├── docker-compose.yml          # Стек бота
├── Dockerfile
├── example.env
└── README.ru.md
```

## Быстрый старт

### Требования

- Go 1.25+
- Docker и Docker Compose — опционально; OpenSearch — отдельно, если нужен анализ фото

### Настройка

1. Клонировать репозиторий.
2. Скопировать `example.env` в `.env`:
   ```bash
   BOT_TOKEN=123456789:your-token
   ```
3. **YouTube** (опционально) — `YT_DLP_ENABLED=true`, путь к `yt-dlp`, при необходимости куки в `secrets/youtube_cookies.txt`.
4. **Анализ изображений** (опционально) — поднять стек и проиндексировать данные: [docker/opensearch/README.md](docker/opensearch/README.md), затем `OPENSEARCH_URL`, `LLM_API`.

### Запуск

```bash
docker compose up -d --build
```

## Конфигурация

Переменные бота — в `.env`. Полный список — в `example.env` и `internal/config/config.go`.

- `BOT_TOKEN` (обязательно) — токен бота от BotFather

**YouTube** (`YT_DLP_ENABLED=true`):

- `YT_DLP_PATH` — путь к `yt-dlp`, по умолчанию `yt-dlp`
- `YT_DLP_COOKIES_FILE`, `YT_DLP_COOKIES_FROM_BROWSER` — куки при блокировках

**Анализ изображений** (`OPENSEARCH_URL` включает обработку фото):

- `OPENSEARCH_URL` — URL OpenSearch
- `LLM_API` — API-ключ SiliconFlow

## Команды Telegram

- `/ytm <URL>` — скачать аудио с YouTube
- `/ytv <URL>` — скачать видео с YouTube (mkv)
- фото — анализ изображения (если настроен OpenSearch)

### Внешние сервисы
- **yt-dlp** — скачивание с YouTube
- **OpenSearch** — гибридный kNN + текстовый поиск по статьям УК РФ
- **SiliconFlow** — vision LLM для описания изображений
