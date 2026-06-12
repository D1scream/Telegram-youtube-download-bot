package music

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"telegram-bot/internal/adapters"
	"telegram-bot/internal/entities"
)

const (
	slskdDownloadsPrefix   = "/app/downloads"
	downloadWatchInterval  = 2 * time.Second
	downloadWatchTimeout   = 30 * time.Minute
	downloadFailedMessage  = "Не удалось скачать. Попробуйте другой трек из /find."
	downloadTimeoutMessage = "Скачивание не завершилось вовремя. Попробуйте другой трек из /find."
)

type TrackDownloader interface {
	EnqueueDownload(ctx context.Context, track entities.Track) error
}

type DownloadStatusChecker interface {
	GetDownloadStatus(ctx context.Context, track entities.Track) (adapters.DownloadTransferStatus, bool, error)
}

type DocumentMessenger interface {
	ReplyToChat(ctx context.Context, chatID int64, messageID int, text string) (int, error)
	SendDocument(ctx context.Context, chatID int64, filename string, file *os.File) (int, error)
	ReplyDocument(ctx context.Context, chatID int64, messageID int, filename string, file *os.File) (int, error)
}

type DownloadCompleteEvent struct {
	LocalFilename  string
	RemoteFilename string
	Username       string
	Filename       string
	Size           int64
}

func (s *SearchMusicService) Download(ctx context.Context, chatID int64, messageID int, index int, userID int64) {
	track, err := s.sessions.Get(chatID, 0, index, ErrNoSession, ErrIndexOutOfRange)
	if err != nil {
		if msg := sessionErrorMessage(index, err); msg != "" {
			if replyErr := s.replyToChat(ctx, chatID, messageID, msg); replyErr != nil {
				s.logger.ErrorContext(ctx, "Не удалось отправить сообщение об ошибке скачивания", "err", replyErr)
			}
		}
		return
	}

	s.trackUserMessage(chatID, messageID)
	if track.LocalPath != "" {
		s.logger.InfoContext(ctx, "Отправка кэшированного файла",
			"operation", "download_music",
			"chat_id", chatID,
			"index", index,
			"path", track.LocalPath,
		)
		pending := pendingDownload{chatID: chatID, messageID: messageID, userID: userID}
		if err := s.sendDownloadedFile(ctx, pending, track, ""); err != nil {
			s.logger.ErrorContext(ctx, "Не удалось отправить кэшированный файл",
				"operation", "download_music",
				"chat_id", chatID,
				"path", track.LocalPath,
				"err", err,
			)
		}
		return
	}

	s.pending.Register(track, chatID, messageID, userID)
	if err := s.downloader.EnqueueDownload(ctx, track); err != nil {
		s.pending.Unregister(track)
		s.logger.ErrorContext(ctx, "Не удалось поставить скачивание в очередь",
			"operation", "download_music",
			"chat_id", chatID,
			"index", index,
			"err", err,
		)
		if replyErr := s.replyToChat(ctx, chatID, messageID, "Не удалось начать скачивание"); replyErr != nil {
			s.logger.ErrorContext(ctx, "Не удалось отправить сообщение об ошибке скачивания", "err", replyErr)
		}
		return
	}

	s.logger.InfoContext(ctx, "Скачивание поставлено в очередь",
		"operation", "download_music",
		"chat_id", chatID,
		"message_id", messageID,
		"index", index,
		"username", track.Username,
		"filename", track.Filename,
	)

	if checker, ok := s.downloader.(DownloadStatusChecker); ok {
		go s.watchDownload(checker, track, chatID, messageID)
	}
}

func (s *SearchMusicService) watchDownload(checker DownloadStatusChecker, track entities.Track, chatID int64, messageID int) {
	ctx, cancel := context.WithTimeout(context.Background(), downloadWatchTimeout)
	defer cancel()

	ticker := time.NewTicker(downloadWatchInterval)
	defer ticker.Stop()

	for {
		status, found, err := checker.GetDownloadStatus(ctx, track)
		if err != nil {
			s.logger.WarnContext(ctx, "Не удалось проверить статус скачивания",
				"operation", "download_watch",
				"chat_id", chatID,
				"username", track.Username,
				"filename", track.Filename,
				"err", err,
			)
		} else if found {
			switch {
			case strings.Contains(status.State, "Succeeded"):
				s.deliverIfPending(context.Background(), track, chatID, messageID, "")
				return
			case isTerminalDownloadState(status.State):
				s.pending.Take(track)
				s.banFailedPeer(context.Background(), track, status.State, status.Exception)
				s.logger.ErrorContext(ctx, "Скачивание не удалось",
					"operation", "download_watch",
					"chat_id", chatID,
					"message_id", messageID,
					"username", track.Username,
					"filename", track.Filename,
					"size", track.Size,
					"state", status.State,
					"exception", status.Exception,
				)
				s.notifyDownloadFailed(context.Background(), chatID, messageID, downloadFailedMessage)
				return
			}
		}

		select {
		case <-ctx.Done():
			if _, ok := s.pending.Take(track); ok {
				state := ""
				if found {
					state = status.State
				}
				s.logger.WarnContext(ctx, "Таймаут ожидания скачивания",
					"operation", "download_watch",
					"chat_id", chatID,
					"message_id", messageID,
					"username", track.Username,
					"filename", track.Filename,
					"state", state,
				)
				s.notifyDownloadFailed(context.Background(), chatID, messageID, downloadTimeoutMessage)
			}
			return
		case <-ticker.C:
		}
	}
}

func isTerminalDownloadState(state string) bool {
	// slskd: Completed,Succeeded — успех; Completed,TimedOut/Errored/Cancelled/... — сбой
	return strings.Contains(state, "Completed") && !strings.Contains(state, "Succeeded")
}

func (s *SearchMusicService) HandleDownloadComplete(ctx context.Context, event DownloadCompleteEvent) error {
	track := eventTrack(event)
	pending, ok := s.pending.Take(track)
	if !ok {
		s.logger.InfoContext(ctx, "Webhook без ожидающего скачивания",
			"operation", "download_webhook",
			"username", track.Username,
			"filename", track.Filename,
			"size", track.Size,
		)
		return ErrDownloadNotFound
	}

	return s.sendDownloadedFile(ctx, pending, track, event.LocalFilename)
}

func (s *SearchMusicService) deliverIfPending(ctx context.Context, track entities.Track, chatID int64, messageID int, localFilename string) {
	pending, ok := s.pending.Take(track)
	if !ok {
		return
	}
	if err := s.sendDownloadedFile(ctx, pending, track, localFilename); err != nil {
		s.logger.ErrorContext(ctx, "Не удалось доставить скачанный файл",
			"operation", "download_deliver",
			"chat_id", chatID,
			"message_id", messageID,
			"username", track.Username,
			"filename", track.Filename,
			"err", err,
		)
	}
}

func (s *SearchMusicService) sendDownloadedFile(ctx context.Context, pending pendingDownload, track entities.Track, localFilename string) error {
	localPath, err := s.resolveTrackPath(track, localFilename)
	if err != nil {
		s.logger.ErrorContext(ctx, "Не удалось найти скачанный файл",
			"operation", "download_deliver",
			"path", localFilename,
			"username", track.Username,
			"filename", track.Filename,
			"err", err,
		)
		_ = s.replyToChat(ctx, pending.chatID, pending.messageID, "Файл скачан, но не найден на диске")
		return err
	}

	file, err := os.Open(localPath)
	if err != nil {
		s.logger.ErrorContext(ctx, "Не удалось открыть скачанный файл",
			"operation", "download_deliver",
			"path", localPath,
			"err", err,
		)
		_ = s.replyToChat(ctx, pending.chatID, pending.messageID, "Файл скачан, но не найден на диске")
		return err
	}
	defer file.Close()

	filename := strings.ReplaceAll(track.Filename, "\\", "/")
	sentID, err := s.messenger.SendDocument(ctx, pending.chatID, filename, file)
	if err != nil {
		s.logger.ErrorContext(ctx, "Не удалось отправить документ",
			"operation", "download_deliver",
			"chat_id", pending.chatID,
			"filename", filename,
			"err", err,
		)
		s.notifyDownloadFailed(ctx, pending.chatID, pending.messageID, "Файл скачан, но не удалось отправить в Telegram.")
		return err
	}

	oldIDs := s.sessions.ClearMessages(pending.chatID)
	s.deleteSessionMessages(ctx, pending.chatID, oldIDs)

	s.registerDownloadCache(ctx, pending.userID, localPath)

	s.logger.InfoContext(ctx, "Документ отправлен",
		"operation", "download_deliver",
		"chat_id", pending.chatID,
		"message_id", sentID,
		"filename", filename,
		"deleted_messages", len(oldIDs),
	)
	return nil
}

func (s *SearchMusicService) resolveTrackPath(track entities.Track, localFilename string) (string, error) {
	if track.LocalPath != "" {
		if _, err := os.Stat(track.LocalPath); err != nil {
			return "", fmt.Errorf("кэшированный файл не найден: %w", err)
		}
		return track.LocalPath, nil
	}
	return s.resolveDownloadPath(track, localFilename)
}

func (s *SearchMusicService) resolveDownloadPath(track entities.Track, localFilename string) (string, error) {
	if localFilename != "" {
		localPath := resolveLocalPath(localFilename, s.downloadsDir, slskdDownloadsPrefix)
		if _, err := os.Stat(localPath); err == nil {
			return localPath, nil
		}
	}
	return s.findDownloadedFile(track)
}

func (s *SearchMusicService) findDownloadedFile(track entities.Track) (string, error) {
	wantBase := strings.ToLower(path.Base(strings.ReplaceAll(track.Filename, "\\", "/")))
	var matches []string

	err := filepath.WalkDir(s.downloadsDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		info, err := d.Info()
		if err != nil || info.Size() != track.Size {
			return nil
		}
		if strings.ToLower(path.Base(p)) != wantBase {
			return nil
		}

		matches = append(matches, p)
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("файл не найден в %s", s.downloadsDir)
	}
	return matches[0], nil
}

func eventTrack(event DownloadCompleteEvent) entities.Track {
	filename := event.Filename
	if filename == "" {
		filename = event.RemoteFilename
	}
	return entities.Track{
		Filename: filename,
		Size:     event.Size,
		Username: event.Username,
	}
}

func resolveLocalPath(localFilename, downloadsDir, downloadsPrefix string) string {
	if localFilename == "" {
		return ""
	}

	relative := localFilename
	if downloadsPrefix != "" {
		if after, ok := strings.CutPrefix(localFilename, downloadsPrefix); ok {
			relative = strings.TrimPrefix(after, "/")
			relative = strings.TrimPrefix(relative, "\\")
		}
	}

	if downloadsDir == "" {
		return filepath.FromSlash(relative)
	}
	return filepath.Join(downloadsDir, filepath.FromSlash(relative))
}

func (s *SearchMusicService) notifyDownloadFailed(ctx context.Context, chatID int64, messageID int, text string) {
	if err := s.replyToChat(ctx, chatID, messageID, text); err != nil {
		s.logger.ErrorContext(ctx, "Не удалось уведомить об ошибке скачивания",
			"operation", "download_notify",
			"chat_id", chatID,
			"err", err,
		)
	}
}

func (s *SearchMusicService) banFailedPeer(ctx context.Context, track entities.Track, state, exception string) {
	if s.peerModerator == nil {
		return
	}
	if err := s.peerModerator.BanPeer(ctx, track.Username); err != nil {
		s.logger.WarnContext(ctx, "Не удалось добавить пира в blacklist slskd",
			"operation", "peer_ban",
			"username", track.Username,
			"state", state,
			"exception", exception,
			"err", err,
		)
		return
	}

	until := s.peerBanExpiry.Schedule(track.Username)
	s.logger.InfoContext(ctx, "Пир добавлен в blacklist slskd",
		"operation", "peer_ban",
		"username", track.Username,
		"until", until,
		"state", state,
		"exception", exception,
	)
}

func (s *SearchMusicService) runPeerBanCleanup() {
	ticker := time.NewTicker(peerBanCleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		if s.peerModerator == nil {
			continue
		}

		ctx := context.Background()
		for _, username := range s.peerBanExpiry.Expired(time.Now()) {
			if err := s.peerModerator.UnbanPeer(ctx, username); err != nil {
				s.logger.WarnContext(ctx, "Не удалось снять временный ban с пира",
					"operation", "peer_unban",
					"username", username,
					"err", err,
				)
				continue
			}
			s.logger.InfoContext(ctx, "Временный ban с пира снят",
				"operation", "peer_unban",
				"username", username,
			)
		}
	}
}

func sessionErrorMessage(index int, err error) string {
	switch {
	case errors.Is(err, ErrNoSession):
		return "Сначала выполните поиск: /find <запрос>"
	case errors.Is(err, ErrIndexOutOfRange):
		return fmt.Sprintf("Нет трека с номером %d. Выполните /find заново.", index)
	default:
		return ""
	}
}
