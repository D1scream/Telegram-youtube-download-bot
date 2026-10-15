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
	DownloadDir  string
	CookiesFile  string
	FileshareURL string
}

type Messenger interface {
	ReplyToChat(ctx context.Context, chatID int64, messageID int, text string) (int, error)
	ReplyAudio(ctx context.Context, chatID int64, messageID int, filename string, file *os.File) (int, error)
	ReplyVideo(ctx context.Context, chatID int64, messageID int, filename string, file *os.File) (int, error)
	DeleteMessage(ctx context.Context, chatID int64, messageID int) error
}

type fileDownloader func(ctx context.Context, pageURL string) (downloadResult, error)

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
	fileshare *fileshare
	messenger Messenger
	logger    *slog.Logger
}

func New(cfg Config, messenger Messenger, logger *slog.Logger) (*Service, error) {
	dl, err := newYtdlp(cfg)
	if err != nil {
		return nil, err
	}
	s := &Service{
		ytdlp:     dl,
		messenger: messenger,
		logger:    logger.With("component", "youtube_download"),
	}
	if cfg.FileshareURL != "" {
		if s.fileshare, err = startFileshare(filepath.Join(dl.outputDir, "share"), cfg.FileshareURL, s.logger); err != nil {
			return nil, fmt.Errorf("запустить раздачу файлов: %w", err)
		}
	}
	return s, nil
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
	result, err := job.download(workCtx, pageURL)
	if err != nil {
		s.logger.ErrorContext(workCtx, "yt-dlp ошибка", "url", pageURL, "err", err)
		s.reply(workCtx, job, fmt.Sprintf("Не удалось скачать: %s", err))
		return
	}
	path := result.path
	defer os.RemoveAll(filepath.Dir(path))

	if result.cookiesErr != nil {
		s.logger.WarnContext(workCtx, "YouTube отклонил cookies, файл скачан без них", "url", pageURL, "err", result.cookiesErr)
		s.reply(workCtx, job, "YouTube отклонил cookies, файл скачан без них. ")
	}

	file, err := os.Open(path)
	if err != nil {
		s.reply(workCtx, job, "Не удалось открыть файл для отправки")
		return
	}
	defer file.Close()

	if info, err := file.Stat(); err == nil && info.Size() > telegramMaxUploadBytes {
		s.sendFileshareLink(workCtx, job, path, info.Size())
		return
	}

	name := filepath.Base(path)
	if _, sendErr := job.send(workCtx, job.chatID, 0, name, file); sendErr != nil {
		s.logger.ErrorContext(workCtx, "Не удалось отправить файл YouTube", "path", path, "err", sendErr)
		s.reply(workCtx, job, "Скачано, но не удалось отправить в Telegram")
		return
	}

	s.deleteCommand(workCtx, job)
}

func (s *Service) sendFileshareLink(ctx context.Context, job downloadJob, path string, size int64) {
	sizeMB := float64(size) / (1024 * 1024)
	if s.fileshare == nil {
		s.reply(ctx, job, fmt.Sprintf("Файл слишком большой для Telegram (%.1f MB), а раздача файлов не настроена", sizeMB))
		return
	}
	link, err := s.fileshare.publish(path)
	if err != nil {
		s.logger.ErrorContext(ctx, "Не удалось выложить файл на раздачу", "path", path, "err", err)
		s.reply(ctx, job, fmt.Sprintf("Файл слишком большой для Telegram (%.1f MB), а выложить его на раздачу не удалось", sizeMB))
		return
	}

	if _, err := s.messenger.ReplyToChat(ctx, job.chatID, 0, link); err != nil {
		s.logger.ErrorContext(ctx, "Не удалось отправить ссылку YouTube", "link", link, "err", err)
		return
	}
	s.deleteCommand(ctx, job)
}

func (s *Service) reply(ctx context.Context, job downloadJob, text string) {
	if _, err := s.messenger.ReplyToChat(ctx, job.chatID, job.messageID, text); err != nil {
		s.logger.ErrorContext(ctx, "Не удалось отправить ответ YouTube", "err", err)
	}
}

func (s *Service) deleteCommand(ctx context.Context, job downloadJob) {
	if err := s.messenger.DeleteMessage(ctx, job.chatID, job.messageID); err != nil {
		s.logger.WarnContext(ctx, "Не удалось удалить команду YouTube", "err", err)
	}
}
