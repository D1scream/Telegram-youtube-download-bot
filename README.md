# Telegram Bot

[Русский](README.ru.md)

Go Telegram bot: YouTube audio/video downloads and image analysis with Criminal Code of the Russian Federation (УК РФ) article lookup via OpenSearch and an LLM.

## Features

- **YouTube** (`YT_DLP_ENABLED`): audio `/ytm`, video `/ytv`
- **Image analysis** (`OPENSEARCH_URL` + `LLM_API`): describe photos with an LLM, search УК РФ articles in OpenSearch

## Technologies

- **Language**: Go 1.25
- **APIs**: Telegram Bot API, yt-dlp, OpenSearch, SiliconFlow LLM, TEI embeddings
- **Libraries**: `go-telegram/bot`, `caarlos0/env`, `godotenv`
- **Architecture**: `adapters` → `usecases` → `transport` layers, DI in `cmd/main.go`
- **Containerization**: Docker, Docker Compose

## Project Structure
```text
├── cmd/
├── internal/
│   ├── adapters/               # Telegram, OpenSearch, LLM, yt-dlp
│   ├── config/                 # Configuration loading
│   ├── entities/               # Domain entities
│   ├── transport/
│   │   └── telegram/           # Telegram command handler
│   └── usecases/
│       ├── imageuk/            # Photo analysis and article search
│       └── youtube/            # YouTube downloads
├── docker/
│   └── opensearch/             # OpenSearch stack for image analysis
├── prompts/                    # LLM system prompts
├── secrets/
├── docker-compose.yml          # Bot stack
├── Dockerfile
├── example.env
└── README.md
```

## Quick Start

### Prerequisites

- Go 1.25+
- Docker and Docker Compose — optional; OpenSearch separately, if image analysis is needed

### Setup

1. Clone the repository.
2. Copy `example.env` to `.env`:
   ```bash
   BOT_TOKEN=123456789:your-token
   ```
3. **YouTube** (optional) — `YT_DLP_ENABLED=true`, path to `yt-dlp`, cookies in `secrets/youtube_cookies.txt` if needed.
4. **Image analysis** (optional) — deploy the stack and index data: [docker/opensearch/README.md](docker/opensearch/README.md), then set `OPENSEARCH_URL`, `LLM_API`.

### Running

```bash
docker compose up -d --build
```

## Configuration

Bot variables go in `.env`. Full list — in `example.env` and `internal/config/config.go`.

- `BOT_TOKEN` (required) — Telegram bot token from BotFather

**YouTube** (`YT_DLP_ENABLED=true`):

- `YT_DLP_PATH` — path to `yt-dlp`, default `yt-dlp`
- `YT_DLP_COOKIES_FILE`, `YT_DLP_COOKIES_FROM_BROWSER` — cookies when blocked

**Image analysis** (`OPENSEARCH_URL` enables photo handling):

- `OPENSEARCH_URL` — OpenSearch URL
- `LLM_API` — SiliconFlow API key

## Telegram Commands

- `/ytm <URL>` — download audio from YouTube
- `/ytv <URL>` — download video from YouTube (mkv)
- photo — analyze image (when OpenSearch is configured)

### External Services

- **yt-dlp** — YouTube downloads
- **OpenSearch** — hybrid kNN + text search over УК РФ articles
- **SiliconFlow** — vision LLM for image description
