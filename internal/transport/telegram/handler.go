package telegram

import (
	"context"
	"log/slog"
	"strings"

	"github.com/go-telegram/bot/models"

	"telegram-bot/internal/imposter"
	"telegram-bot/internal/souchastnik"
	"telegram-bot/internal/youtube"
)

type Handler struct {
	youtube  *youtube.Service
	checker  *souchastnik.Client
	tg       *Bot
	imposter *imposterController
	history  *history
	logger   *slog.Logger
}

func NewHandler(yt *youtube.Service, checker *souchastnik.Client, tg *Bot, bank *imposter.WordList, store *imposter.Store, ownerID int64, logger *slog.Logger) *Handler {
	return &Handler{
		youtube:  yt,
		checker:  checker,
		tg:       tg,
		imposter: newImposterController(tg, bank, store, ownerID, logger),
		history:  newHistory(),
		logger:   logger.With("component", "telegram_handler"),
	}
}

func (h *Handler) HandleCallback(ctx context.Context, query *models.CallbackQuery) {
	h.imposter.handleCallback(ctx, query)
}

func (h *Handler) HandleMessage(ctx context.Context, msg *models.Message) {
	cmd, args, _ := strings.Cut(messageCommandLine(msg), " ")
	switch {
	case isCommand(cmd, "help"):
		h.handleHelp(ctx, msg)
	case isCommand(cmd, "ytm"):
		h.handleYtm(ctx, msg, args)
	case isCommand(cmd, "ytv"):
		h.handleYtv(ctx, msg, args)
	case isCommand(cmd, "check"):
		h.handleCheck(ctx, msg, args)
	case isCommand(cmd, "start"):
		h.handleStart(ctx, msg)
	case isCommand(cmd, "imposter"):
		h.imposter.handleCommand(ctx, msg, "imposter")
	case isCommand(cmd, "imposter_settings"):
		h.imposter.handleCommand(ctx, msg, "imposter_settings")
	case isCommand(cmd, "imposter_stop"):
		h.imposter.handleCommand(ctx, msg, "imposter_stop")
	case isCommand(cmd, "imposter_ban"):
		h.imposter.handleBan(ctx, msg, args, true)
	case isCommand(cmd, "imposter_unban"):
		h.imposter.handleBan(ctx, msg, args, false)
	case isCommand(cmd, "topic"):
		h.imposter.handleTopic(ctx, msg, args)
	case isCommand(cmd, "word"):
		h.imposter.handleWord(ctx, msg, args)
	default:
		h.handlePassiveCheck(ctx, msg, messageCommandLine(msg))
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
/ytv <URL> - видео с YouTube
/check <текст> - проверка текста
/imposter - игра "Импостер". Команды /imposter_settings, /imposter_stop, /word`

func (h *Handler) handleHelp(ctx context.Context, msg *models.Message) {
	if _, err := h.tg.ReplyToChat(ctx, msg.Chat.ID, msg.ID, helpMessage); err != nil {
		h.logger.ErrorContext(ctx, "Не удалось отправить /help", "err", err)
	}
}

func (h *Handler) handleStart(ctx context.Context, msg *models.Message) {
	if msg.Chat.Type != models.ChatTypePrivate {
		return
	}
	h.reply(ctx, msg, "Вы зарегистрированы для игры в \"Импостер\".\n\n"+imposterHelp)
}

func (h *Handler) handleCheck(ctx context.Context, msg *models.Message, text string) {
	if h.checker == nil {
		h.reply(ctx, msg, "Проверка текста недоступна (SOUCHASTNIK_URL не настроен)")
		return
	}
	if strings.TrimSpace(text) == "" {
		h.reply(ctx, msg, "Использование: /check <текст>")
		return
	}

	result, err := h.checker.Check(ctx, text, nil)
	if err != nil {
		h.logger.ErrorContext(ctx, "Не удалось проверить текст", "err", err)
		h.reply(ctx, msg, "Не удалось проверить текст")
		return
	}
	if result.Code == "none" {
		h.reply(ctx, msg, "Состав не обнаружен")
		return
	}
	h.logViolation(ctx, msg, text, result)
	h.reply(ctx, msg, violationMessage(result))
}

func (h *Handler) handlePassiveCheck(ctx context.Context, msg *models.Message, text string) {
	if h.checker == nil || text == "" {
		return
	}

	previous := h.history.add(msg.Chat.ID, historyLine(msg, text))
	result, err := h.checker.Check(ctx, text, previous)
	if err != nil {
		h.logger.ErrorContext(ctx, "Не удалось проверить входящее сообщение", "err", err)
		return
	}
	if result.Code != "none" {
		h.logViolation(ctx, msg, text, result)
		h.reply(ctx, msg, violationMessage(result))
	}
}

func (h *Handler) logViolation(ctx context.Context, msg *models.Message, text string, result souchastnik.Result) {
	var fromID int64
	var fromUsername string
	if msg.From != nil {
		fromID = msg.From.ID
		fromUsername = msg.From.Username
	}
	h.logger.InfoContext(ctx, "Сработала проверка текста",
		"chat_id", msg.Chat.ID,
		"message_id", msg.ID,
		"from_id", fromID,
		"from_username", fromUsername,
		"code", result.Code,
		"text", text,
	)
}

func violationMessage(result souchastnik.Result) string {
	if result.Description == "" {
		return "Возможная статья: " + result.Code
	}
	return result.Description
}

func (h *Handler) reply(ctx context.Context, msg *models.Message, text string) {
	if _, err := h.tg.ReplyToChat(ctx, msg.Chat.ID, msg.ID, text); err != nil {
		h.logger.ErrorContext(ctx, "Не удалось отправить ответ", "err", err)
	}
}

func (h *Handler) handleYtm(ctx context.Context, msg *models.Message, url string) {
	if h.youtube == nil {
		if _, err := h.tg.ReplyToChat(ctx, msg.Chat.ID, msg.ID, "YouTube недоступен (yt-dlp не настроен)"); err != nil {
			h.logger.ErrorContext(ctx, "Не удалось отправить ответ ytm", "err", err)
		}
		return
	}
	h.youtube.DownloadMusic(ctx, msg.Chat.ID, msg.ID, url)
}

func (h *Handler) handleYtv(ctx context.Context, msg *models.Message, url string) {
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
