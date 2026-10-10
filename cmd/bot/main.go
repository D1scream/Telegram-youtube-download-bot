package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"telegram-bot/internal/config"
	"telegram-bot/internal/imposter"
	"telegram-bot/internal/msglog"
	"telegram-bot/internal/souchastnik"
	"telegram-bot/internal/transport/telegram"
	"telegram-bot/internal/youtube"
)

const (
	imposterStateFile  = "data/imposter.json"
	wordsFile          = "words_downloaded.txt"
	messageLogDir      = "data/messages"
	messageLogMaxBytes = 1024 * 1024 * 1024
)

func main() {
	if len(os.Args) > 1 {
		os.Exit(runCLI(os.Args[1:]))
	}

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

	msgLog, err := msglog.Open(messageLogDir, messageLogMaxBytes)
	if err != nil {
		logger.Error("Ошибка открытия лога сообщений", "err", err)
		os.Exit(1)
	}
	defer msgLog.Close()
	tg.SetMessageLog(msgLog, logger)

	bank, err := imposter.LoadWordList(wordsFile)
	if err != nil {
		logger.Error("Ошибка загрузки банка слов", "err", err)
		os.Exit(1)
	}
	logger.Info("Банк слов загружен", "words", bank.Len())

	yt, err := youtube.New(youtube.Config{
		DownloadDir: cfg.YtdlpDownloadDir,
		CookiesFile: cfg.YtdlpCookiesFile,
	}, tg, logger)
	if err != nil {
		logger.Error("Ошибка инициализации YouTube", "err", err)
		os.Exit(1)
	}

	logger.Info("Telegram polling запущен")
	checker := newSouchastnik(cfg, logger)
	store, err := imposter.OpenStore(imposterStateFile)
	if err != nil {
		logger.Error("Ошибка загрузки состояния игры", "err", err)
		os.Exit(1)
	}
	handler := telegram.NewHandler(yt, checker, tg, bank, store, cfg.OwnerID, logger)
	if err := tg.Start(ctx, handler.HandleMessage, handler.HandleCallback); err != nil {
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
