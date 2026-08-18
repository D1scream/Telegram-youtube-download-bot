package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

type config struct {
	BotToken                string `env:"BOT_TOKEN,required"`
	YtdlpEnabled            bool   `env:"YT_DLP_ENABLED" envDefault:"false"`
	YtdlpPath               string `env:"YT_DLP_PATH" envDefault:"yt-dlp"`
	YtdlpDownloadDir        string `env:"YT_DLP_DOWNLOAD_DIR" envDefault:"yt_downloads"`
	YtdlpCookiesFile        string `env:"YT_DLP_COOKIES_FILE"`
	YtdlpCookiesFromBrowser string `env:"YT_DLP_COOKIES_FROM_BROWSER"`
}

func loadConfig() (config, error) {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return config{}, fmt.Errorf("загрузка .env: %w", err)
	}
	cfg, err := env.ParseAs[config]()
	if err != nil {
		return config{}, fmt.Errorf("разбор конфигурации: %w", err)
	}
	return cfg, nil
}
