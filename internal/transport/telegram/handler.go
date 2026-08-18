package telegram

import (
	"context"
	"log/slog"
	"strings"

	"github.com/go-telegram/bot/models"
)

type PhotoAnalyzer interface {
	AnalyzePhoto(ctx context.Context, msg *models.Message)
}

type YoutubeHandler interface {
	DownloadMusic(ctx context.Context, chatID int64, messageID int, url string)
	DownloadVideo(ctx context.Context, chatID int64, messageID int, url string)
}

type Messenger interface {
	ReplyToChat(ctx context.Context, chatID int64, messageID int, text string) (int, error)
}

type Handler struct {
	photos    PhotoAnalyzer
	youtube   YoutubeHandler
	messenger Messenger
	logger    *slog.Logger
}

func NewHandler(
	photos PhotoAnalyzer,
	youtube YoutubeHandler,
	messenger Messenger,
	logger *slog.Logger,
) *Handler {
	return &Handler{
		photos:    photos,
		youtube:   youtube,
		messenger: messenger,
		logger:    logger.With("component", "telegram_handler"),
	}
}

func (h *Handler) HandleMessage(ctx context.Context, msg *models.Message) {
	if h.handleCommand(ctx, msg) {
		return
	}
	if len(msg.Photo) == 0 || h.photos == nil {
		return
	}
	h.photos.AnalyzePhoto(ctx, msg)
}

func (h *Handler) handleCommand(ctx context.Context, msg *models.Message) bool {
	cmd, args, _ := strings.Cut(messageCommandLine(msg), " ")
	switch {
	case isCommand(cmd, "help"):
		h.handleHelp(ctx, msg)
		return true
	case isCommand(cmd, "ytm"):
		h.handleYtm(ctx, msg, args)
		return true
	case isCommand(cmd, "ytv"):
		h.handleYtv(ctx, msg, args)
		return true
	default:
		return false
	}
}

func messageCommandLine(msg *models.Message) string {
	if text := strings.TrimSpace(msg.Text); text != "" {
		return text
	}
	return strings.TrimSpace(msg.Caption)
}

const helpMessage = `Команды
/ytm <URL> — аудио с YouTube
/ytv <URL> — видео с YouTube (mkv)

Отправьте фото — анализ изображения (если настроен OpenSearch)`

func (h *Handler) handleHelp(ctx context.Context, msg *models.Message) {
	if _, err := h.messenger.ReplyToChat(ctx, msg.Chat.ID, msg.ID, helpMessage); err != nil {
		h.logger.ErrorContext(ctx, "Не удалось отправить /help", "err", err)
	}
}

func (h *Handler) handleYtm(ctx context.Context, msg *models.Message, url string) {
	if h.youtube == nil {
		if _, err := h.messenger.ReplyToChat(ctx, msg.Chat.ID, msg.ID, "YouTube недоступен (yt-dlp не настроен)"); err != nil {
			h.logger.ErrorContext(ctx, "Не удалось отправить ответ ytm", "err", err)
		}
		return
	}
	h.youtube.DownloadMusic(ctx, msg.Chat.ID, msg.ID, url)
}

func (h *Handler) handleYtv(ctx context.Context, msg *models.Message, url string) {
	if h.youtube == nil {
		if _, err := h.messenger.ReplyToChat(ctx, msg.Chat.ID, msg.ID, "YouTube недоступен (yt-dlp не настроен)"); err != nil {
			h.logger.ErrorContext(ctx, "Не удалось отправить ответ ytv", "err", err)
		}
		return
	}
	h.youtube.DownloadVideo(ctx, msg.Chat.ID, msg.ID, url)
}

func isCommand(token, name string) bool {
	return token == "/"+name || strings.HasPrefix(token, "/"+name+"@")
}
