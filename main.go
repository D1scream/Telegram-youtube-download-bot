package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"telegram-bot/internal/config"
	"telegram-bot/internal/souchastnik"
	"telegram-bot/internal/telegram"
	"telegram-bot/internal/youtube"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		logger.Error("Ошибка загрузки конфигурации", "err", err)
		os.Exit(1)
	}

	tg, err := telegram.New(cfg.BotToken)
	if err != nil {
		logger.Error("Ошибка инициализации Telegram", "err", err)
		os.Exit(1)
	}

	logger.Info("Telegram polling запущен")
	checker := newSouchastnik(cfg, logger)
	if err := tg.Start(ctx, newHandler(newYouTube(cfg, tg, logger), checker, tg, logger).handleMessage); err != nil {
		logger.Error("Telegram polling завершился с ошибкой", "err", err)
		os.Exit(1)
	}
	logger.Info("Сервер остановлен")
}

func newSouchastnik(cfg config.Config, logger *slog.Logger) *souchastnik.Client {
	if cfg.SouchastnikURL == "" {
		logger.Info("Проверка текста отключена (SOUCHASTNIK_URL не задан)")
		return nil
	}
	logger.Info("Проверка текста включена", "url", cfg.SouchastnikURL)
	return souchastnik.New(souchastnik.Config{
		URL:            cfg.SouchastnikURL,
		TimeoutSeconds: cfg.SouchastnikTimeout,
	})
}

func newYouTube(cfg config.Config, tg *telegram.Bot, logger *slog.Logger) *youtube.Service {
	if !cfg.YtdlpEnabled {
		logger.Info("YouTube /ytm /ytv отключены (YT_DLP_ENABLED=false)")
		return nil
	}
	yt, err := youtube.New(youtube.Config{
		Bin:            cfg.YtdlpPath,
		DownloadDir:    cfg.YtdlpDownloadDir,
		CookiesFile:    cfg.YtdlpCookiesFile,
		CookiesBrowser: cfg.YtdlpCookiesFromBrowser,
	}, tg, logger)
	if err != nil {
		logger.Error("YouTube отключён", "err", err)
		return nil
	}
	logger.Info("YouTube /ytm /ytv включены", "cookies_file", cfg.YtdlpCookiesFile, "cookies_browser", cfg.YtdlpCookiesFromBrowser)
	return yt
}
