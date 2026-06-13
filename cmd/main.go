package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/joho/godotenv"

	"telegram-bot/internal/adapters"
	"telegram-bot/internal/config"
	"telegram-bot/internal/prompt"
	"telegram-bot/internal/transport/telegram"
	"telegram-bot/internal/usecases/imageuk"
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

	photoAnalyzer := newPhotoAnalyzer(cfg, telegramAdapter, logger)
	musicService := newMusicService(cfg, telegramAdapter, logger)
	musicUploader := newMusicUploader(cfg, musicService, telegramAdapter, logger)
	myMusicService := music.NewMyMusicService(musicUploader, musicService, telegramAdapter, telegramAdapter, logger)
	telegramHandler := telegram.NewHandler(photoAnalyzer, musicService, musicUploader, myMusicService, telegramAdapter, logger)

	logger.Info("Telegram polling запущен")
	if err := telegramAdapter.StartPolling(ctx, telegramHandler.HandleMessage); err != nil {
		logger.Error("Telegram polling завершился с ошибкой", "err", err)
		os.Exit(1)
	}
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

func newMusicService(cfg *config.Config, telegramAdapter *adapters.TelegramAdapter, logger *slog.Logger) *music.SearchMusicService {
	if strings.TrimSpace(cfg.SlskdURL) == "" {
		logger.Info("Поиск музыки отключён (SLSKD_URL не задан)")
		return nil
	}

	searcher := adapters.NewSlskdAdapter(cfg.SlskdURL, cfg.SlskdAPIKey)
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
