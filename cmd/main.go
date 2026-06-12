package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"telegram-bot/internal/adapters"
	"telegram-bot/internal/config"
	"telegram-bot/internal/prompt"
	"telegram-bot/internal/server"
	httptransport "telegram-bot/internal/transport/http"
	"telegram-bot/internal/transport/telegram"
	"telegram-bot/internal/usecases/imageuk"
	"telegram-bot/internal/usecases/messaging"
	"telegram-bot/internal/usecases/music"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	if err := godotenv.Load(); err != nil {
		logger.Warn("Файл .env не найден или не загружен")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	cfg, err := config.LoadConfig()
	if err != nil {
		logger.Error("Ошибка загрузки конфигурации", "err", err)
		os.Exit(1)
	}

	telegramAdapter, err := adapters.NewTelegramAdapter(cfg.BotToken)
	if err != nil {
		logger.Error("Ошибка инициализации Telegram", "err", err)
		os.Exit(1)
	}

	sendMessageService := messaging.NewSendMessageService(telegramAdapter, logger)
	photoAnalyzer := newPhotoAnalyzer(cfg, telegramAdapter, logger)
	musicService := newMusicService(cfg, telegramAdapter, logger)
	musicUploader := newMusicUploader(cfg, musicService, telegramAdapter, logger)
	myMusicService := music.NewMyMusicService(musicUploader, musicService, telegramAdapter, telegramAdapter, logger)
	telegramHandler := telegram.NewHandler(photoAnalyzer, musicService, musicUploader, myMusicService, telegramAdapter, logger)

	var slskdWebhook http.Handler
	if musicService != nil {
		slskdWebhook = httptransport.NewSlskdWebhookHandler(musicService, cfg.SlskdWebhookSecret, logger)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		logger.Info("Telegram polling запущен")
		if err := telegramAdapter.StartPolling(ctx, telegramHandler.HandleMessage); err != nil {
			logger.Error("Telegram polling завершился с ошибкой", "err", err)
			cancel()
		}
		logger.Info("Telegram polling остановлен")
	}()

	httpServer := &http.Server{
		Addr: ":" + cfg.Port,
		Handler: server.New(server.Dependencies{
			SendMessage:  sendMessageService,
			SlskdWebhook: slskdWebhook,
			Logger:       logger,
		}),
	}

	go func() {
		logger.Info("HTTP сервер запущен", "port", cfg.Port)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("HTTP сервер завершился с ошибкой", "err", err)
			cancel()
		}
	}()

	<-ctx.Done()
	logger.Info("Получен сигнал завершения, останавливаем сервер...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("Ошибка остановки HTTP сервера", "err", err)
		os.Exit(1)
	}

	wg.Wait()
	logger.Info("Сервер остановлен")
}

func newPhotoAnalyzer(cfg *config.Config, telegramAdapter *adapters.TelegramAdapter, logger *slog.Logger) telegram.PhotoAnalyzer {
	if strings.TrimSpace(cfg.OpenSearchURL) == "" {
		logger.Info("Анализ изображений отключён (OPENSEARCH_URL не задан)")
		return nil
	}
	if cfg.LLMAPIKey == "" {
		logger.Warn("OPENSEARCH_URL задан, но LLM_API пуст — анализ изображений недоступен")
		return nil
	}

	systemPrompt, err := prompt.NewFile(cfg.LLMSystemPromptPath)
	if err != nil {
		logger.Error("Ошибка загрузки system prompt", "path", cfg.LLMSystemPromptPath, "err", err)
		os.Exit(1)
	}

	llmAdapter := adapters.NewSiliconFlowAdapter(cfg.LLMAPIKey, systemPrompt)
	embeddingsAdapter := adapters.NewEmbeddingsAdapter(cfg.EmbeddingsURL)
	openSearchAdapter := adapters.NewOpenSearchAdapter(
		cfg.OpenSearchURL,
		cfg.OpenSearchIndex,
		embeddingsAdapter,
		cfg.SearchKNNK,
		cfg.OpenSearchSearchPipeline,
	)
	logger.Info("Анализ изображений включён", "opensearch_url", cfg.OpenSearchURL)
	return imageuk.NewAnalyzeImageService(
		telegramAdapter,
		llmAdapter,
		openSearchAdapter,
		telegramAdapter,
		cfg.SearchMinScore,
		logger,
	)
}

func ensureSlskdWebhook(searcher *adapters.SlskdAdapter, logger *slog.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var lastErr error
	for range 5 {
		if err := searcher.EnsureWebhookConfigured(ctx); err != nil {
			lastErr = err
			time.Sleep(2 * time.Second)
			continue
		}
		return
	}
	logger.Warn("Не удалось настроить webhook slskd", "err", lastErr)
}

func newMusicService(cfg *config.Config, telegramAdapter *adapters.TelegramAdapter, logger *slog.Logger) *music.SearchMusicService {
	if strings.TrimSpace(cfg.SlskdURL) == "" {
		logger.Info("Поиск музыки отключён (SLSKD_URL не задан)")
		return nil
	}

	searcher := adapters.NewSlskdAdapter(
		cfg.SlskdURL,
		cfg.SlskdAPIKey,
		cfg.SlskdWebhookCallbackURL,
		cfg.SlskdWebhookSecret,
	)
	ensureSlskdWebhook(searcher, logger)
	logger.Info("Поиск музыки включён", "slskd_url", cfg.SlskdURL)
	return music.NewSearchMusicService(
		searcher,
		searcher,
		telegramAdapter,
		telegramAdapter,
		cfg.SlskdDownloadsDir,
		[]string{cfg.SlskdDownloadsDir, cfg.SlskdMusicDir, cfg.UploadedMusicDir},
		cfg.SlskdSearchFileLimit,
		cfg.SlskdSearchDisplayLimit,
		cfg.MusicAllowedFormats(),
		logger,
	)
}

func newMusicUploader(cfg *config.Config, musicService *music.SearchMusicService, telegramAdapter *adapters.TelegramAdapter, logger *slog.Logger) *music.UploadMusicService {
	if musicService == nil {
		return nil
	}
	return music.NewUploadMusicService(
		telegramAdapter,
		telegramAdapter,
		cfg.UploadedMusicDir,
		cfg.MusicAllowedFormats(),
		logger,
	)
}
