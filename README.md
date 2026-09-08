# Telegram Bot

[Русский](README.ru.md)

YouTube downloads: `/ytm` audio, `/ytv` video. Optional text checking via a local `souchastnik` inference service.

```bash
cp example.env .env
docker compose up -d --build
```

To enable text checking, set `SOUCHASTNIK_URL` in `.env`. The bot sends `POST /verdict` with
`{"text":"..."}` and expects `{"code":"280"}` or `{"code":"none"}`. Use `/check <text>` in Telegram.
