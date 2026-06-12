# Telegram-бот

[English](README.md)

Telegram-бот с возможностями: поиск и скачивание музыки через Soulseek ([slskd](https://github.com/slskd/slskd)), локальная библиотека и загрузки пользователей, анализ изображений с поиском статей УК РФ через OpenSearch и LLM, HTTP-эндпоинт для отправки сообщений.

Функции включаются через конфигурацию. Обязателен только `BOT_TOKEN`.

## Возможности

- **Поиск** (`/find <запрос>`): поиск в Soulseek + локальные файлы `[C]`
- **Скачивание** (`/downloadN`): скачивание файла из поисковой выдачи, очистка сообщений сессии
- **Загрузка** (`/upload` + аудиофайл): сохранение в `uploaded_music/`, доступно в `/find`
- **Мои файлы** (`/mymusic`, `/mymusic 2`): список файлов пользователя (uploaded и cached), по 10 на страницу
- **Удаление** (`/deleteN`): удаление файла по номеру из `/mymusic`
- **Анализ изображений** (фото): описание картинки через LLM, поиск подходящих статей УК РФ в OpenSearch, 
ответ с результатами
- **HTTP API** (`POST /send`): отправка текстового сообщения в чат по ID
- **Webhook slskd** (`POST /webhooks/slskd`): события о завершении скачивания
- Бан пиров при неудачном скачивании (срок 7 дней todo: добавить переменную окружения) с синхронизацией blacklist в slskd

## Технологии

- **Язык**: Go 1.25
- **API**: Telegram Bot API, slskd REST, OpenSearch, SiliconFlow LLM, TEI embeddings
- **Библиотеки**: `go-telegram/bot`, `go.senan.xyz/taglib`, `caarlos0/env`, `godotenv`
- **Архитектура**: слои `adapters` → `usecases` → `transport`, DI в `cmd/main.go`
- **Контейнеризация**: Docker, Docker Compose

## Структура проекта
```text
├── cmd/                        # Точка входа
├── internal/
│   ├── adapters/               # Telegram, slskd, OpenSearch, LLM
│   ├── config/                 # Загрузка конфигурации
│   ├── entities/               # Доменные сущности
│   ├── server/                 # HTTP-роутер
│   ├── transport/
│   │   ├── http/               # REST-обработчики (/send, /webhooks /slskd)
│   │   └── telegram/           # Обработчик команд Telegram
│   └── usecases/
│       ├── imageuk/            # Анализ фото и поиск статей
│       ├── messaging/          # Отправка сообщений
│       └── music/              # Поиск, загрузка, mymusic, локальный поиск
├── docker/
│   ├── opensearch/             # Стек OpenSearch для анализа изображений
│   └── slskd/                  # Локальный compose и конфиг slskd
├── prompts/                    # System prompt для LLM
├── music/                      # Локальная библиотека (volume)
├── uploaded_music/             # Загрузки пользователей (volume)
├── docker-compose.yml          # Продакшен-стек (бот + slskd)
├── Dockerfile
├── example.env
└── README.ru.md
```

## Быстрый старт

### Требования

- Go 1.25+
- Docker и Docker Compose — для slskd и OpenSearch

### Настройка

1. Клонировать репозиторий.
2. Скопировать `example.env` в `.env`:
   ```bash
   BOT_TOKEN=123456789:your-token
   SLSKD_SLSK_USERNAME=your_username
   SLSKD_SLSK_PASSWORD=your_password
   SLSKD_WEBHOOK_SECRET=your-webhook-secret
   ```
3. **Анализ изображений** (опционально) — `OPENSEARCH_URL`, `LLM_API`.

`SLSKD_URL`, пути к каталогам и остальные переменные с дефолтами заданы в `docker-compose.yml`.

### Запуск

```bash
docker compose up -d --build
```

## Конфигурация

Переменные бота — в `.env`.

- `BOT_TOKEN` (обязательно) — токен бота от BotFather
- `PORT` — порт HTTP-сервера, по умолчанию `9000`

**Музыка** (`SLSKD_URL` включает поиск, скачивание, загрузки, `/mymusic`):

- `SLSKD_URL` — базовый URL slskd
- `SLSKD_API_KEY` — API-ключ slskd, если включена авторизация
- `SLSKD_WEBHOOK_SECRET` — общий секрет для заголовка `X-Webhook-Secret`
- `SLSKD_WEBHOOK_CALLBACK_URL` — URL webhook в slskd, по умолчанию `http://host.docker.internal:9000/webhooks/slskd`
- `SLSKD_SEARCH_FILE_LIMIT` — макс. файлов при поиске, локально + slskd, по умолчанию `50`
- `SLSKD_SEARCH_DISPLAY_LIMIT` — макс. треков в ответе `/find`, по умолчанию `10`
- `SLSKD_ALLOWED_FORMATS` — разрешённые форматы, по умолчанию `mp3,flac,ogg,wav,m4a,aac`
- `SLSKD_DOWNLOADS_DIR` — кэш скачиваний slskd, по умолчанию `docker/slskd/data/downloads`
- `SLSKD_MUSIC_DIR` — локальная библиотека, по умолчанию `music`
- `UPLOADED_MUSIC_DIR` — загрузки пользователей, по умолчанию `uploaded_music`

**Анализ изображений** (`OPENSEARCH_URL` включает обработку фото):

- `OPENSEARCH_URL` — URL OpenSearch
- `LLM_API` — API-ключ SiliconFlow
- `LLM_SYSTEM_PROMPT_PATH` — путь к system prompt, по умолчанию `prompts/image_analysis_system.txt`
- `OPENSEARCH_INDEX` — индекс, по умолчанию `uk_rf`
- `OPENSEARCH_SEARCH_PIPELINE` — pipeline поиска, по умолчанию `uk_rf-hybrid`
- `EMBEDDINGS_URL` — сервис TEI embeddings, по умолчанию `http://localhost:8080`
- `SEARCH_KNN_K` — число соседей kNN при поиске, по умолчанию `20`
- `SEARCH_MIN_SCORE` — минимальный score, по умолчанию `0.55`

**Soulseek** (в том же `.env`, используются контейнером slskd):

- `SLSKD_SLSK_USERNAME`, `SLSKD_SLSK_PASSWORD` — учётная запись Soulseek
- `SLSKD_WEBHOOK_SECRET` — секрет webhook (тот же, что у бота)

## Команды Telegram

- `/find <запрос>` — поиск музыки: `[C]` локально, затем Soulseek
- `/downloadN` — скачать трек N из последнего `/find`
- `/upload` + файл — загрузить аудио (до 20 МБ, лимит Bot API todo: развернуть telegram bot api)
- `/mymusic` — список своих загрузок и кэша (стр. 1)
- `/mymusic N` — страница N списка
- `/deleteN` — удалить файл N из последнего `/mymusic`
- фото — анализ изображения (если настроен OpenSearch)

## Расписание
- **Снятие бана пира**: бан за неудачное скачивание снимается через 7 дней
- TTL сессий `/find` и `/mymusic`: 30 минут

## HTTP API
- **`POST /send`** — отправка текстового сообщения
  ```json
  { "chat_id": -1001234567890, "text": "Привет" }
  ```

### Внешние сервисы
- **slskd**: `POST /api/v0/searches` для поиска, transfers API для скачивания, YAML options API для blacklist и webhook
- **OpenSearch**: гибридный kNN + текстовый поиск по проиндексированным статьям УК РФ
- **SiliconFlow**: vision LLM для описания изображений


Каталоги на диске: `downloads/` (кэш slskd), `music/` (библиотека), `uploaded_music/` (загрузки).
