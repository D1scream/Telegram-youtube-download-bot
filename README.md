# Telegram Bot

Telegram bot with features: music search and download via Soulseek ([slskd](https://github.com/slskd/slskd)), local library and user uploads, image analysis with Criminal Code of the Russian Federation (УК РФ) article lookup via OpenSearch and an LLM, and an HTTP endpoint for sending messages.

Features are enabled by configuration. Only `BOT_TOKEN` is required.

[Русский](README.ru.md)

## Features

- **Search** (`/find <query>`): Soulseek search + local files `[C]`
- **Download** (`/downloadN`): download a track from search results, clean up session messages
- **Upload** (`/upload` + audio file): save to `uploaded_music/`, available in `/find`
- **My files** (`/mymusic`, `/mymusic 2`): list user files (uploaded and cached), 10 per page
- **Delete** (`/deleteN`): delete a file by number from `/mymusic`
- **Image analysis** (photo): describe the image with an LLM, search matching УК РФ articles in OpenSearch, reply with results
- **HTTP API** (`POST /send`): send a text message to a chat by ID
- **slskd webhook** (`POST /webhooks/slskd`): download completion events
- Peer ban on failed downloads (7-day expiry; todo: add env variable) with blacklist sync to slskd

## Technologies

- **Language**: Go 1.25
- **APIs**: Telegram Bot API, slskd REST, OpenSearch, SiliconFlow LLM, TEI embeddings
- **Libraries**: `go-telegram/bot`, `go.senan.xyz/taglib`, `caarlos0/env`, `godotenv`
- **Architecture**: `adapters` → `usecases` → `transport` layers, DI in `cmd/main.go`
- **Containerization**: Docker, Docker Compose

## Project Structure
```text
├── cmd/                        # Application entry point
├── internal/
│   ├── adapters/               # Telegram, slskd, OpenSearch, LLM
│   ├── config/                 # Configuration loading
│   ├── entities/               # Domain entities
│   ├── server/                 # HTTP router
│   ├── transport/
│   │   ├── http/               # REST handlers (/send, /webhooks/slskd)
│   │   └── telegram/           # Telegram command handler
│   └── usecases/
│       ├── imageuk/            # Photo analysis and article search
│       ├── messaging/          # Send message use case
│       └── music/              # Search, upload, mymusic, local search
├── docker/
│   ├── opensearch/             # OpenSearch stack for image analysis
│   └── slskd/                  # Local slskd compose and config
├── prompts/                    # LLM system prompts
├── music/                      # Local library (volume)
├── uploaded_music/             # User uploads (volume)
├── docker-compose.yml          # Production stack (bot + slskd)
├── Dockerfile
├── example.env
└── README.ru.md
```

## Quick Start

### Prerequisites

- Go 1.25+
- Docker and Docker Compose

### Setup

1. Clone the repository.
2. Copy `example.env` to `.env`:
   ```bash
   BOT_TOKEN=123456789:your-token
   SLSKD_SLSK_USERNAME=your_username
   SLSKD_SLSK_PASSWORD=your_password
   SLSKD_WEBHOOK_SECRET=your-webhook-secret
   ```
3. **Image analysis** (optional) — `OPENSEARCH_URL`, `LLM_API`.

`SLSKD_URL`, directory paths, and other variables with defaults are set in `docker-compose.yml`.

### Running

```bash
docker compose up -d --build
```

## Configuration

Bot variables go in `.env`.

- `BOT_TOKEN` (required) — Telegram bot token from BotFather
- `PORT` — HTTP server port, default `9000`

**Music** (`SLSKD_URL` enables search, download, uploads, `/mymusic`):

- `SLSKD_URL` — slskd base URL
- `SLSKD_API_KEY` — slskd API key, if auth is enabled
- `SLSKD_WEBHOOK_SECRET` — shared secret for `X-Webhook-Secret` header
- `SLSKD_WEBHOOK_CALLBACK_URL` — webhook URL registered in slskd, default `http://host.docker.internal:9000/webhooks/slskd`
- `SLSKD_SEARCH_FILE_LIMIT` — max files per search, local + slskd, default `50`
- `SLSKD_SEARCH_DISPLAY_LIMIT` — max tracks in `/find` reply, default `10`
- `SLSKD_ALLOWED_FORMATS` — allowed formats, default `mp3,flac,ogg,wav,m4a,aac`
- `SLSKD_DOWNLOADS_DIR` — slskd download cache, default `docker/slskd/data/downloads`
- `SLSKD_MUSIC_DIR` — local library, default `music`
- `UPLOADED_MUSIC_DIR` — user uploads, default `uploaded_music`

**Image analysis** (`OPENSEARCH_URL` enables photo handling):

- `OPENSEARCH_URL` — OpenSearch URL
- `LLM_API` — SiliconFlow API key
- `LLM_SYSTEM_PROMPT_PATH` — system prompt path, default `prompts/image_analysis_system.txt`
- `OPENSEARCH_INDEX` — index name, default `uk_rf`
- `OPENSEARCH_SEARCH_PIPELINE` — search pipeline, default `uk_rf-hybrid`
- `EMBEDDINGS_URL` — TEI embeddings service, default `http://localhost:8080`
- `SEARCH_KNN_K` — kNN neighbours for search, default `20`
- `SEARCH_MIN_SCORE` — minimum relevance score, default `0.55`

**Soulseek** (same `.env`, used by the slskd container):

- `SLSKD_SLSK_USERNAME`, `SLSKD_SLSK_PASSWORD` — Soulseek account
- `SLSKD_WEBHOOK_SECRET` — webhook secret (same as the bot)

## Telegram Commands

- `/find <query>` — search music: `[C]` locally, then Soulseek
- `/downloadN` — download track N from the last `/find`
- `/upload` + file — upload audio (up to 20 MB, Bot API limit; todo: deploy local Bot API)
- `/mymusic` — list your uploads and cache (page 1)
- `/mymusic N` — page N of the list
- `/deleteN` — delete file N from the last `/mymusic`
- photo — analyze image (when OpenSearch is configured)

## Schedule

- **Peer ban expiry**: failed download bans are lifted after 7 days
- Session TTL for `/find` and `/mymusic`: 30 minutes

## HTTP API

- **`POST /send`** — send a text message
  ```json
  { "chat_id": -1001234567890, "text": "Hello" }
  ```

### External Services

- **slskd**: `POST /api/v0/searches` for search, transfers API for downloads, YAML options API for blacklist and webhook
- **OpenSearch**: hybrid kNN + text search over indexed УК РФ articles
- **SiliconFlow**: vision LLM for image description

On disk: `downloads/` (slskd cache), `music/` (library), `uploaded_music/` (uploads).
