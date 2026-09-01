package youtube

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	telegramMaxUploadBytes = 50 * 1024 * 1024
	downloadTimeout        = 31 * time.Minute
)

type Config struct {
	Bin            string
	DownloadDir    string
	CookiesFile    string
	CookiesBrowser string
}

type Messenger interface {
	ReplyToChat(ctx context.Context, chatID int64, messageID int, text string) (int, error)
	ReplyAudio(ctx context.Context, chatID int64, messageID int, filename string, file *os.File) (int, error)
	ReplyVideo(ctx context.Context, chatID int64, messageID int, filename string, file *os.File) (int, error)
}

type fileDownloader func(ctx context.Context, pageURL string) (path string, err error)

type fileSender func(ctx context.Context, chatID int64, messageID int, filename string, file *os.File) (int, error)

type downloadJob struct {
	chatID    int64
	messageID int
	rawURL    string
	download  fileDownloader
	send      fileSender
}

type Service struct {
	ytdlp     *ytdlp
	messenger Messenger
	logger    *slog.Logger
}

func New(cfg Config, messenger Messenger, logger *slog.Logger) (*Service, error) {
	dl, err := newYtdlp(cfg)
	if err != nil {
		return nil, err
	}
	return &Service{
		ytdlp:     dl,
		messenger: messenger,
		logger:    logger.With("component", "youtube_download"),
	}, nil
}

func (s *Service) DownloadMusic(ctx context.Context, chatID int64, messageID int, rawURL string) {
	go s.runDownload(ctx, downloadJob{
		chatID:    chatID,
		messageID: messageID,
		rawURL:    rawURL,
		download:  s.ytdlp.downloadAudio,
		send:      s.messenger.ReplyAudio,
	})
}

func (s *Service) DownloadVideo(ctx context.Context, chatID int64, messageID int, rawURL string) {
	go s.runDownload(ctx, downloadJob{
		chatID:    chatID,
		messageID: messageID,
		rawURL:    rawURL,
		download:  s.ytdlp.downloadVideo,
		send:      s.messenger.ReplyVideo,
	})
}

func (s *Service) runDownload(ctx context.Context, job downloadJob) {
	workCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), downloadTimeout)
	defer cancel()

	pageURL := strings.TrimSpace(job.rawURL)
	path, err := job.download(workCtx, pageURL)
	if err != nil {
		s.logger.ErrorContext(workCtx, "yt-dlp ошибка", "url", pageURL, "err", err)
		if _, replyErr := s.messenger.ReplyToChat(
			workCtx,
			job.chatID,
			job.messageID,
			fmt.Sprintf("Не удалось скачать: %s", err),
		); replyErr != nil {
			s.logger.ErrorContext(workCtx, "Не удалось отправить ответ YouTube", "err", replyErr)
		}
		return
	}
	defer os.RemoveAll(filepath.Dir(path))

	info, err := os.Stat(path)
	if err != nil {
		if _, replyErr := s.messenger.ReplyToChat(
			workCtx,
			job.chatID,
			job.messageID,
			"Файл скачан, но не найден на диске",
		); replyErr != nil {
			s.logger.ErrorContext(workCtx, "Не удалось отправить ответ YouTube", "err", replyErr)
		}
		return
	}

	if info.Size() > telegramMaxUploadBytes {
		reply := fmt.Sprintf(
			"Файл слишком большой для Telegram (%.1f MB, лимит 50 MB)",
			float64(info.Size())/(1024*1024),
		)
		if _, replyErr := s.messenger.ReplyToChat(workCtx, job.chatID, job.messageID, reply); replyErr != nil {
			s.logger.ErrorContext(workCtx, "Не удалось отправить ответ YouTube", "err", replyErr)
		}
		return
	}

	file, err := os.Open(path)
	if err != nil {
		if _, replyErr := s.messenger.ReplyToChat(
			workCtx,
			job.chatID,
			job.messageID,
			"Не удалось открыть файл для отправки",
		); replyErr != nil {
			s.logger.ErrorContext(workCtx, "Не удалось отправить ответ YouTube", "err", replyErr)
		}
		return
	}
	defer file.Close()

	name := filepath.Base(path)
	if _, sendErr := job.send(workCtx, job.chatID, job.messageID, name, file); sendErr != nil {
		s.logger.ErrorContext(workCtx, "Не удалось отправить файл YouTube", "path", path, "err", sendErr)
		if _, replyErr := s.messenger.ReplyToChat(
			workCtx,
			job.chatID,
			job.messageID,
			"Скачано, но не удалось отправить в Telegram",
		); replyErr != nil {
			s.logger.ErrorContext(workCtx, "Не удалось отправить ответ YouTube", "err", replyErr)
		}
	}
}
