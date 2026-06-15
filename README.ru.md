# Telegram-бот

[English](README.md)

Telegram-бот на Go: поиск и скачивание музыки через Soulseek ([slskd](https://github.com/slskd/slskd)) и локальную библиотеку, скачивание музыки и видео с YouTube, анализ изображений с поиском статей УК РФ через OpenSearch и LLM.

## Возможности

- **Музыка** (`SLSKD_URL`): поиск `/find`, скачивание `/downloadN`, загрузка `/upload`, список `/mymusic`, удаление `/deleteN`
- **YouTube** (`YT_DLP_ENABLED`): скачивание аудио `/ytm`, видео `/ytv`
- **Анализ изображений** (`OPENSEARCH_URL` + `LLM_API`): описание фото через LLM, поиск статей УК РФ в OpenSearch

## Технологии

- **Язык**: Go 1.25
- **API**: Telegram Bot API, slskd REST, yt-dlp, OpenSearch, SiliconFlow LLM, TEI embeddings
- **Библиотеки**: `go-telegram/bot`, `go.senan.xyz/taglib`, `caarlos0/env`, `godotenv`
- **Архитектура**: слои `adapters` → `usecases` → `transport`, DI в `cmd/main.go`
- **Контейнеризация**: Docker, Docker Compose

## Структура проекта
```text
├── cmd/
├── internal/
│   ├── adapters/               # Telegram, slskd, OpenSearch, LLM, yt-dlp
│   ├── config/                 # Загрузка конфигурации
│   ├── entities/               # Доменные сущности
│   ├── transport/
│   │   └── telegram/           # Обработчик команд Telegram
│   └── usecases/
│       ├── imageuk/            # Анализ фото и поиск статей
│       ├── music/              # Поиск, загрузка, mymusic, локальный поиск
│       └── youtube/            # Скачивание с YouTube
├── docker/
│   ├── opensearch/             # Стек OpenSearch для анализа изображений
│   └── slskd/                  # Локальный compose и конфиг slskd
├── prompts/                    # System prompt для LLM
├── secrets/
├── music/                      # Локальная библиотека (volume)
├── uploaded_music/             # Загрузки пользователей (volume)
├── docker-compose.yml          # Стек бот + slskd
├── Dockerfile
├── example.env
└── README.ru.md
```

## Быстрый старт

### Требования

- Go 1.25+
- Docker и Docker Compose — для slskd; OpenSearch — отдельно, если нужен анализ фото

### Настройка

1. Клонировать репозиторий.
2. Скопировать `example.env` в `.env`:
   ```bash
   BOT_TOKEN=123456789:your-token
   SLSKD_SLSK_USERNAME=your_username
   SLSKD_SLSK_PASSWORD=your_password
   ```
3. **YouTube** (опционально) — `YT_DLP_ENABLED=true`, путь к `yt-dlp`, при необходимости куки в `secrets/youtube_cookies.txt`.
4. **Анализ изображений** (опционально) — поднять стек и проиндексировать данные: [docker/opensearch/README.md](docker/opensearch/README.md), затем `OPENSEARCH_URL`, `LLM_API`.

`SLSKD_URL`, пути к каталогам и остальные переменные с дефолтами — в `docker-compose.yml` и `example.env`.

### Запуск

```bash
docker compose up -d --build
```

## Конфигурация

Переменные бота — в `.env`. Полный список — в `example.env` и `internal/config/config.go`.

- `BOT_TOKEN` (обязательно) — токен бота от BotFather

**Музыка** (`SLSKD_URL` включает поиск, скачивание, загрузки, `/mymusic`):

- `SLSKD_URL` — базовый URL slskd
- `SLSKD_API_KEY` — API-ключ slskd, если включена авторизация
- `SLSKD_MUSIC_DIR` — локальная библиотека, по умолчанию `music`
- `UPLOADED_MUSIC_DIR` — загрузки пользователей, по умолчанию `uploaded_music`

**YouTube** (`YT_DLP_ENABLED=true`):

- `YT_DLP_PATH` — путь к `yt-dlp`, по умолчанию `yt-dlp`
- `YT_DLP_COOKIES_FILE`, `YT_DLP_COOKIES_FROM_BROWSER` — куки при блокировках

**Анализ изображений** (`OPENSEARCH_URL` включает обработку фото):

- `OPENSEARCH_URL` — URL OpenSearch
- `LLM_API` — API-ключ SiliconFlow

**Soulseek** (в том же `.env`, используются контейнером slskd):

- `SLSKD_SLSK_USERNAME`, `SLSKD_SLSK_PASSWORD` — учётная запись Soulseek

## Команды Telegram

- `/find <запрос>` — поиск музыки: `[C]` локально, затем Soulseek
- `/downloadN` — скачать трек N из последнего `/find`
- `/upload` + файл — загрузить аудио (до 20 МБ, лимит Bot API)
- `/mymusic` — список своих загрузок и кэша (стр. 1)
- `/mymusic N` — страница N списка
- `/deleteN` — удалить файл N из последнего `/mymusic`
- `/ytm <URL>` — скачать аудио с YouTube
- `/ytv <URL>` — скачать видео с YouTube (mkv)
- фото — анализ изображения (если настроен OpenSearch)

## Расписание
- **Снятие бана пира**: бан за неудачное скачивание снимается через 7 дней
- TTL сессий `/find` и `/mymusic`: 30 минут

### Внешние сервисы
- **slskd** — поиск и скачивание через Soulseek
- **yt-dlp** — скачивание с YouTube
- **OpenSearch** — гибридный kNN + текстовый поиск по статьям УК РФ
- **SiliconFlow** — vision LLM для описания изображений


Каталоги на диске: `docker/slskd/data/downloads/` (кэш slskd), `music/` (библиотека), `uploaded_music/` (загрузки).
