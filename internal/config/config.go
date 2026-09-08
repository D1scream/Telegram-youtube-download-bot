package config

import (
	"errors"
	"fmt"
	"os"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

type Config struct {
	BotToken                string `env:"BOT_TOKEN,required"`
	SouchastnikURL          string `env:"SOUCHASTNIK_URL"`
	SouchastnikTimeout      int    `env:"SOUCHASTNIK_TIMEOUT_SECONDS" envDefault:"30"`
	YtdlpEnabled            bool   `env:"YT_DLP_ENABLED" envDefault:"false"`
	YtdlpPath               string `env:"YT_DLP_PATH" envDefault:"yt-dlp"`
	YtdlpDownloadDir        string `env:"YT_DLP_DOWNLOAD_DIR" envDefault:"yt_downloads"`
	YtdlpCookiesFile        string `env:"YT_DLP_COOKIES_FILE"`
	YtdlpCookiesFromBrowser string `env:"YT_DLP_COOKIES_FROM_BROWSER"`
}

func Load() (Config, error) {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return Config{}, fmt.Errorf("загрузка .env: %w", err)
	}
	cfg, err := env.ParseAs[Config]()
	if err != nil {
		return Config{}, fmt.Errorf("разбор конфигурации: %w", err)
	}
	return cfg, nil
}
