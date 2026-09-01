package main

import (
	"context"
	"log/slog"
	"strings"

	"github.com/go-telegram/bot/models"

	"telegram-bot/internal/telegram"
	"telegram-bot/internal/youtube"
)

type handler struct {
	youtube *youtube.Service
	tg      *telegram.Bot
	logger  *slog.Logger
}

func newHandler(yt *youtube.Service, tg *telegram.Bot, logger *slog.Logger) *handler {
	return &handler{
		youtube: yt,
		tg:      tg,
		logger:  logger.With("component", "telegram_handler"),
	}
}

func (h *handler) handleMessage(ctx context.Context, msg *models.Message) {
	cmd, args, _ := strings.Cut(messageCommandLine(msg), " ")
	switch {
	case isCommand(cmd, "help"):
		h.handleHelp(ctx, msg)
	case isCommand(cmd, "ytm"):
		h.handleYtm(ctx, msg, args)
	case isCommand(cmd, "ytv"):
		h.handleYtv(ctx, msg, args)
	}
}

func messageCommandLine(msg *models.Message) string {
	if text := strings.TrimSpace(msg.Text); text != "" {
		return text
	}
	return strings.TrimSpace(msg.Caption)
}

const helpMessage = `Команды
/ytm <URL> - аудио с YouTube
/ytv <URL> - видео с YouTube`

func (h *handler) handleHelp(ctx context.Context, msg *models.Message) {
	if _, err := h.tg.ReplyToChat(ctx, msg.Chat.ID, msg.ID, helpMessage); err != nil {
		h.logger.ErrorContext(ctx, "Не удалось отправить /help", "err", err)
	}
}

func (h *handler) handleYtm(ctx context.Context, msg *models.Message, url string) {
	if h.youtube == nil {
		if _, err := h.tg.ReplyToChat(ctx, msg.Chat.ID, msg.ID, "YouTube недоступен (yt-dlp не настроен)"); err != nil {
			h.logger.ErrorContext(ctx, "Не удалось отправить ответ ytm", "err", err)
		}
		return
	}
	h.youtube.DownloadMusic(ctx, msg.Chat.ID, msg.ID, url)
}

func (h *handler) handleYtv(ctx context.Context, msg *models.Message, url string) {
	if h.youtube == nil {
		if _, err := h.tg.ReplyToChat(ctx, msg.Chat.ID, msg.ID, "YouTube недоступен (yt-dlp не настроен)"); err != nil {
			h.logger.ErrorContext(ctx, "Не удалось отправить ответ ytv", "err", err)
		}
		return
	}
	h.youtube.DownloadVideo(ctx, msg.Chat.ID, msg.ID, url)
}

func isCommand(token, name string) bool {
	return token == "/"+name || strings.HasPrefix(token, "/"+name+"@")
}
