package youtube

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const telegramMaxUploadBytes = 50 * 1024 * 1024

type mediaDelivery int

const (
	mediaAudio mediaDelivery = 1
	mediaVideo mediaDelivery = 2
)

type YtdlpDownloader interface {
	DownloadAudio(ctx context.Context, pageURL string) (string, error)
	DownloadVideo(ctx context.Context, pageURL string) (string, error)
}

type Messenger interface {
	ReplyToChat(ctx context.Context, chatID int64, messageID int, text string) (int, error)
	ReplyAudio(ctx context.Context, chatID int64, messageID int, filename string, file *os.File) (int, error)
	ReplyVideo(ctx context.Context, chatID int64, messageID int, filename string, file *os.File) (int, error)
}

type MessageDeleter interface {
	DeleteMessages(ctx context.Context, chatID int64, messageIDs []int) error
}

type DownloadService struct {
	downloader YtdlpDownloader
	messenger  Messenger
	deleter    MessageDeleter
	logger     *slog.Logger
}

func NewDownloadService(
	downloader YtdlpDownloader,
	messenger Messenger,
	deleter MessageDeleter,
	logger *slog.Logger,
) *DownloadService {
	return &DownloadService{
		downloader: downloader,
		messenger:  messenger,
		deleter:    deleter,
		logger:     logger.With("component", "youtube_download"),
	}
}

func (s *DownloadService) DownloadMusic(ctx context.Context, chatID int64, messageID int, rawURL string) {
	s.startDownload(ctx, chatID, messageID, rawURL, "аудио", mediaAudio, s.downloader.DownloadAudio)
}

func (s *DownloadService) DownloadVideo(ctx context.Context, chatID int64, messageID int, rawURL string) {
	s.startDownload(ctx, chatID, messageID, rawURL, "видео", mediaVideo, s.downloader.DownloadVideo)
}

func (s *DownloadService) startDownload(
	ctx context.Context,
	chatID int64,
	messageID int,
	rawURL string,
	kind string,
	delivery mediaDelivery,
	download func(context.Context, string) (string, error),
) {
	pageURL, err := normalizeYouTubeURL(rawURL)
	if err != nil {
		s.reply(ctx, chatID, messageID, err.Error())
		return
	}

	statusID, err := s.messenger.ReplyToChat(ctx, chatID, messageID, fmt.Sprintf("Скачиваю %s…", kind))
	if err != nil {
		s.logger.ErrorContext(ctx, "Не удалось отправить статус yt-dlp", "err", err)
	}

	go func() {
		workCtx, cancel := context.WithTimeout(context.Background(), 31*time.Minute)
		defer cancel()
		defer s.deleteStatus(workCtx, chatID, statusID)

		path, err := download(workCtx, pageURL)
		if err != nil {
			s.logger.ErrorContext(workCtx, "yt-dlp ошибка", "url", pageURL, "kind", kind, "err", err)
			s.reply(workCtx, chatID, messageID, fmt.Sprintf("Не удалось скачать: %s", formatYtdlpError(err)))
			return
		}
		defer os.RemoveAll(filepath.Dir(path))

		info, err := os.Stat(path)
		if err != nil {
			s.reply(workCtx, chatID, messageID, "Файл скачан, но не найден на диске")
			return
		}
		if info.Size() > telegramMaxUploadBytes {
			s.reply(workCtx, chatID, messageID, fmt.Sprintf(
				"Файл слишком большой для Telegram (%.1f MB, лимит 50 MB)",
				float64(info.Size())/(1024*1024),
			))
			return
		}

		file, err := os.Open(path)
		if err != nil {
			s.reply(workCtx, chatID, messageID, "Не удалось открыть файл для отправки")
			return
		}
		defer file.Close()

		name := filepath.Base(path)
		var sendErr error
		switch delivery {
		case mediaVideo:
			_, sendErr = s.messenger.ReplyVideo(workCtx, chatID, messageID, name, file)
		case mediaAudio:
			_, sendErr = s.messenger.ReplyAudio(workCtx, chatID, messageID, name, file)
		}
		if sendErr != nil {
			s.logger.ErrorContext(workCtx, "Не удалось отправить файл YouTube", "path", path, "delivery", delivery, "err", sendErr)
			s.reply(workCtx, chatID, messageID, "Скачано, но не удалось отправить в Telegram")
		}
	}()
}

func (s *DownloadService) reply(ctx context.Context, chatID int64, messageID int, text string) {
	if _, err := s.messenger.ReplyToChat(ctx, chatID, messageID, text); err != nil {
		s.logger.ErrorContext(ctx, "Не удалось отправить ответ YouTube", "err", err)
	}
}

func (s *DownloadService) deleteStatus(ctx context.Context, chatID int64, statusID int) {
	if statusID == 0 || s.deleter == nil {
		return
	}
	if err := s.deleter.DeleteMessages(ctx, chatID, []int{statusID}); err != nil {
		s.logger.WarnContext(ctx, "Не удалось удалить статус yt-dlp", "message_id", statusID, "err", err)
	}
}

func formatYtdlpError(err error) string {
	msg := strings.TrimSpace(err.Error())
	msg = strings.TrimPrefix(msg, "yt-dlp: ")
	return msg
}

func normalizeYouTubeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("Укажите ссылку: /ytm <URL> или /ytv <URL>")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("Некорректная ссылка")
	}
	host := strings.ToLower(strings.TrimPrefix(u.Host, "www."))
	switch host {
	case "youtube.com", "m.youtube.com", "music.youtube.com", "youtu.be":
		return raw, nil
	default:
		return "", fmt.Errorf("Поддерживаются только ссылки YouTube")
	}
}
