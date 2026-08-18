package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	cfg, err := loadConfig()
	if err != nil {
		logger.Error("Ошибка загрузки конфигурации", "err", err)
		os.Exit(1)
	}

	tg, err := newTelegram(cfg.BotToken)
	if err != nil {
		logger.Error("Ошибка инициализации Telegram", "err", err)
		os.Exit(1)
	}

	logger.Info("Telegram polling запущен")
	if err := tg.Start(ctx, newHandler(newYouTube(cfg, tg, logger), tg, logger).HandleMessage); err != nil {
		logger.Error("Telegram polling завершился с ошибкой", "err", err)
		os.Exit(1)
	}
	logger.Info("Сервер остановлен")
}
