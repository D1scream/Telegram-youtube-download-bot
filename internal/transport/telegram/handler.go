package telegram

import (
	"context"
	"log/slog"
	"strconv"
	"strings"

	"github.com/go-telegram/bot/models"
)

type PhotoAnalyzer interface {
	AnalyzePhoto(ctx context.Context, msg *models.Message)
}

type MusicHandler interface {
	Find(ctx context.Context, chatID int64, messageID int, query string)
	Download(ctx context.Context, chatID int64, messageID int, index int, userID int64)
}

type MusicUploader interface {
	Upload(ctx context.Context, msg *models.Message)
}

type MyMusicHandler interface {
	List(ctx context.Context, msg *models.Message, page int)
	Delete(ctx context.Context, chatID int64, messageID int, index int, userID int64)
}

type YoutubeHandler interface {
	DownloadMusic(ctx context.Context, chatID int64, messageID int, url string)
	DownloadVideo(ctx context.Context, chatID int64, messageID int, url string)
}

type Messenger interface {
	ReplyToChat(ctx context.Context, chatID int64, messageID int, text string) (int, error)
}

type Handler struct {
	photos      PhotoAnalyzer
	music       MusicHandler
	musicUpload MusicUploader
	myMusic     MyMusicHandler
	youtube     YoutubeHandler
	messenger   Messenger
	logger      *slog.Logger
}

func NewHandler(
	photos PhotoAnalyzer,
	music MusicHandler,
	musicUpload MusicUploader,
	myMusic MyMusicHandler,
	youtube YoutubeHandler,
	messenger Messenger,
	logger *slog.Logger,
) *Handler {
	return &Handler{
		photos:      photos,
		music:       music,
		musicUpload: musicUpload,
		myMusic:     myMusic,
		youtube:     youtube,
		messenger:   messenger,
		logger:      logger.With("component", "telegram_handler"),
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
	case isCommand(cmd, "find"):
		h.handleFind(ctx, msg, args)
		return true
	case isCommand(cmd, "ytm"):
		h.handleYtm(ctx, msg, args)
		return true
	case isCommand(cmd, "ytv"):
		h.handleYtv(ctx, msg, args)
		return true
	case isCommand(cmd, "upload"):
		h.handleUpload(ctx, msg)
		return true
	case isCommand(cmd, "mymusic"):
		h.handleMyMusic(ctx, msg, args)
		return true
	case isDeleteCommand(cmd):
		h.handleDelete(ctx, msg, cmd, args)
		return true
	case isDownloadCommand(cmd):
		h.handleDownload(ctx, msg, cmd, args)
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

func hasMusicAttachment(msg *models.Message) bool {
	return msg.Document != nil || msg.Audio != nil
}

const helpMessage = `Команды
/find <запрос> — поиск музыки
/downloadN — скачать трек N из последнего /find
/upload — загрузить свою музыку, указать файл в сообщении
/mymusic [страница] — ваши файлы
/deleteN — удалить файл N из /mymusic
/ytm <URL> — аудио с YouTube
/ytv <URL> — видео с YouTube (mkv)

Поиск (/find)
Поиск полнотекстовый, по подстрокам слов
Если ничего не найдено, бот повторит запрос с более мягкими фильтрами

Аргументы запроса:
*слово — неполное слово (*ichael → Michael)
-слово — исключить из результатов (-remix)
Пример: /find *ichael -live

Советы:
• [C] — файл на сервере бота, без Soulseek`

func (h *Handler) handleHelp(ctx context.Context, msg *models.Message) {
	if _, err := h.messenger.ReplyToChat(ctx, msg.Chat.ID, msg.ID, helpMessage); err != nil {
		h.logger.ErrorContext(ctx, "Не удалось отправить /help", "err", err)
	}
}

func (h *Handler) handleMyMusic(ctx context.Context, msg *models.Message, pageArg string) {
	if h.myMusic == nil {
		if _, err := h.messenger.ReplyToChat(ctx, msg.Chat.ID, msg.ID, "Список файлов недоступен"); err != nil {
			h.logger.ErrorContext(ctx, "Не удалось отправить сообщение о недоступности mymusic", "err", err)
		}
		return
	}

	page, ok := parseMyMusicPage(pageArg)
	if !ok {
		if _, err := h.messenger.ReplyToChat(ctx, msg.Chat.ID, msg.ID, "Укажите номер страницы: /mymusic 2"); err != nil {
			h.logger.ErrorContext(ctx, "Не удалось отправить подсказку по mymusic", "err", err)
		}
		return
	}
	h.myMusic.List(ctx, msg, page)
}

func parseMyMusicPage(args string) (int, bool) {
	args = strings.TrimSpace(args)
	if args == "" {
		return 1, true
	}
	page, err := strconv.Atoi(args)
	return page, err == nil && page >= 1
}

func (h *Handler) handleDelete(ctx context.Context, msg *models.Message, cmd, args string) {
	index, ok := parseDeleteIndex(cmd, args)
	if !ok {
		h.logger.InfoContext(ctx, "Некорректный аргумент /delete", "cmd", cmd, "args", args)
		return
	}
	if h.myMusic == nil {
		if _, err := h.messenger.ReplyToChat(ctx, msg.Chat.ID, msg.ID, "Удаление недоступно"); err != nil {
			h.logger.ErrorContext(ctx, "Не удалось отправить сообщение о недоступности удаления", "err", err)
		}
		return
	}

	var userID int64
	if msg.From != nil {
		userID = msg.From.ID
	}
	h.myMusic.Delete(ctx, msg.Chat.ID, msg.ID, index, userID)
}

func (h *Handler) handleUpload(ctx context.Context, msg *models.Message) {
	if h.musicUpload == nil {
		if _, err := h.messenger.ReplyToChat(ctx, msg.Chat.ID, msg.ID, "Загрузка музыки недоступна"); err != nil {
			h.logger.ErrorContext(ctx, "Не удалось отправить сообщение о недоступности загрузки", "err", err)
		}
		return
	}
	if !hasMusicAttachment(msg) {
		if _, err := h.messenger.ReplyToChat(ctx, msg.Chat.ID, msg.ID, "Прикрепите аудиофайл к команде /upload"); err != nil {
			h.logger.ErrorContext(ctx, "Не удалось отправить подсказку по загрузке", "err", err)
		}
		return
	}
	h.musicUpload.Upload(ctx, msg)
}

func (h *Handler) handleFind(ctx context.Context, msg *models.Message, query string) {
	if h.music == nil {
		if _, err := h.messenger.ReplyToChat(ctx, msg.Chat.ID, msg.ID, "Поиск музыки недоступен"); err != nil {
			h.logger.ErrorContext(ctx, "Не удалось отправить сообщение о недоступности поиска", "err", err)
		}
		return
	}
	h.music.Find(ctx, msg.Chat.ID, msg.ID, query)
}

func (h *Handler) handleDownload(ctx context.Context, msg *models.Message, cmd, args string) {
	index, ok := parseDownloadIndex(cmd, args)
	if !ok {
		h.logger.InfoContext(ctx, "Некорректный аргумент /download", "cmd", cmd, "args", args)
		return
	}
	if h.music == nil {
		if _, err := h.messenger.ReplyToChat(ctx, msg.Chat.ID, msg.ID, "Поиск музыки недоступен"); err != nil {
			h.logger.ErrorContext(ctx, "Не удалось отправить сообщение о недоступности поиска", "err", err)
		}
		return
	}
	var userID int64
	if msg.From != nil {
		userID = msg.From.ID
	}
	h.music.Download(ctx, msg.Chat.ID, msg.ID, index, userID)
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

func isDownloadCommand(cmd string) bool {
	if _, ok := parseDownloadIndex(cmd, ""); ok {
		return true
	}
	name, _, _ := strings.Cut(strings.TrimPrefix(cmd, "/"), "@")
	return name == "download"
}

func isDeleteCommand(cmd string) bool {
	if _, ok := parseDeleteIndex(cmd, ""); ok {
		return true
	}
	name, _, _ := strings.Cut(strings.TrimPrefix(cmd, "/"), "@")
	return name == "delete"
}

func parseDownloadIndex(cmd, args string) (int, bool) {
	return parseIndexedCommand(cmd, args, "download")
}

func parseDeleteIndex(cmd, args string) (int, bool) {
	return parseIndexedCommand(cmd, args, "delete")
}

func parseIndexedCommand(cmd, args, prefix string) (int, bool) {
	name, _, _ := strings.Cut(strings.TrimPrefix(cmd, "/"), "@")
	if !strings.HasPrefix(name, prefix) {
		return 0, false
	}
	if suffix := strings.TrimPrefix(name, prefix); suffix != "" {
		index, err := strconv.Atoi(suffix)
		return index, err == nil && index >= 1
	}
	index, err := strconv.Atoi(strings.TrimSpace(args))
	return index, err == nil && index >= 1
}
