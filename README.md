# Telegram Bot

[Русский](README.ru.md)

Go Telegram bot: music search and download via Soulseek ([slskd](https://github.com/slskd/slskd)) and a local library, YouTube audio/video downloads, image analysis with Criminal Code of the Russian Federation (УК РФ) article lookup via OpenSearch and an LLM.

## Features

- **Music** (`SLSKD_URL`): search `/find`, download `/downloadN`, upload `/upload`, list `/mymusic`, delete `/deleteN`
- **YouTube** (`YT_DLP_ENABLED`): audio `/ytm`, video `/ytv`
- **Image analysis** (`OPENSEARCH_URL` + `LLM_API`): describe photos with an LLM, search УК РФ articles in OpenSearch

## Technologies

- **Language**: Go 1.25
- **APIs**: Telegram Bot API, slskd REST, yt-dlp, OpenSearch, SiliconFlow LLM, TEI embeddings
- **Libraries**: `go-telegram/bot`, `go.senan.xyz/taglib`, `caarlos0/env`, `godotenv`
- **Architecture**: `adapters` → `usecases` → `transport` layers, DI in `cmd/main.go`
- **Containerization**: Docker, Docker Compose

## Project Structure
```text
├── cmd/
├── internal/
│   ├── adapters/               # Telegram, slskd, OpenSearch, LLM, yt-dlp
│   ├── config/                 # Configuration loading
│   ├── entities/               # Domain entities
│   ├── transport/
│   │   └── telegram/           # Telegram command handler
│   └── usecases/
│       ├── imageuk/            # Photo analysis and article search
│       ├── music/              # Search, upload, mymusic, local search
│       └── youtube/            # YouTube downloads
├── docker/
│   ├── opensearch/             # OpenSearch stack for image analysis
│   └── slskd/                  # Local slskd compose and config
├── prompts/                    # LLM system prompts
├── secrets/
├── music/                      # Local library (volume)
├── uploaded_music/             # User uploads (volume)
├── docker-compose.yml          # Bot + slskd stack
├── Dockerfile
├── example.env
└── README.md
```

## Quick Start

### Prerequisites

- Go 1.25+
- Docker and Docker Compose — for slskd; OpenSearch separately, if image analysis is needed

### Setup

1. Clone the repository.
2. Copy `example.env` to `.env`:
   ```bash
   BOT_TOKEN=123456789:your-token
   SLSKD_SLSK_USERNAME=your_username
   SLSKD_SLSK_PASSWORD=your_password
   ```
3. **YouTube** (optional) — `YT_DLP_ENABLED=true`, path to `yt-dlp`, cookies in `secrets/youtube_cookies.txt` if needed.
4. **Image analysis** (optional) — deploy the stack and index data: [docker/opensearch/README.md](docker/opensearch/README.md), then set `OPENSEARCH_URL`, `LLM_API`.

`SLSKD_URL`, directory paths, and other variables with defaults — in `docker-compose.yml` and `example.env`.

### Running

```bash
docker compose up -d --build
```

## Configuration

Bot variables go in `.env`. Full list — in `example.env` and `internal/config/config.go`.

- `BOT_TOKEN` (required) — Telegram bot token from BotFather

**Music** (`SLSKD_URL` enables search, download, uploads, `/mymusic`):

- `SLSKD_URL` — slskd base URL
- `SLSKD_API_KEY` — slskd API key, if auth is enabled
- `SLSKD_MUSIC_DIR` — local library, default `music`
- `UPLOADED_MUSIC_DIR` — user uploads, default `uploaded_music`

**YouTube** (`YT_DLP_ENABLED=true`):

- `YT_DLP_PATH` — path to `yt-dlp`, default `yt-dlp`
- `YT_DLP_COOKIES_FILE`, `YT_DLP_COOKIES_FROM_BROWSER` — cookies when blocked

**Image analysis** (`OPENSEARCH_URL` enables photo handling):

- `OPENSEARCH_URL` — OpenSearch URL
- `LLM_API` — SiliconFlow API key

**Soulseek** (same `.env`, used by the slskd container):

- `SLSKD_SLSK_USERNAME`, `SLSKD_SLSK_PASSWORD` — Soulseek account

## Telegram Commands

- `/find <query>` — search music: `[C]` locally, then Soulseek
- `/downloadN` — download track N from the last `/find`
- `/upload` + file — upload audio (up to 20 MB, Bot API limit)
- `/mymusic` — list your uploads and cache (page 1)
- `/mymusic N` — page N of the list
- `/deleteN` — delete file N from the last `/mymusic`
- `/ytm <URL>` — download audio from YouTube
- `/ytv <URL>` — download video from YouTube (mkv)
- photo — analyze image (when OpenSearch is configured)

## Schedule

- **Peer ban expiry**: failed download bans are lifted after 7 days
- Session TTL for `/find` and `/mymusic`: 30 minutes

### External Services

- **slskd** — search and download via Soulseek
- **yt-dlp** — YouTube downloads
- **OpenSearch** — hybrid kNN + text search over УК РФ articles
- **SiliconFlow** — vision LLM for image description


On disk: `docker/slskd/data/downloads/` (slskd cache), `music/` (library), `uploaded_music/` (uploads).
