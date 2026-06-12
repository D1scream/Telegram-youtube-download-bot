package music

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/go-telegram/bot/models"
)

const (
	fileSourceUpload = "upload"
	fileSourceCache  = "cache"
)

type userMusicFile struct {
	path   string
	source string
}

type MyMusicService struct {
	upload         *UploadMusicService
	search         *SearchMusicService
	sessions       *chatSessionStore[userMusicFile]
	messenger      Messenger
	messageDeleter MessageDeleter
	logger         *slog.Logger
}

func NewMyMusicService(
	upload *UploadMusicService,
	search *SearchMusicService,
	messenger Messenger,
	messageDeleter MessageDeleter,
	logger *slog.Logger,
) *MyMusicService {
	if upload == nil && search == nil {
		return nil
	}

	service := &MyMusicService{
		upload:         upload,
		search:         search,
		sessions:       newChatSessionStore[userMusicFile](),
		messenger:      messenger,
		messageDeleter: messageDeleter,
		logger:         logger.With("component", "my_music_service"),
	}
	go service.runSessionCleanup()
	return service
}

func (s *MyMusicService) List(ctx context.Context, msg *models.Message, page int) {
	if msg.From == nil {
		if _, err := s.messenger.ReplyToChat(ctx, msg.Chat.ID, msg.ID, "Не удалось определить пользователя"); err != nil {
			s.logger.ErrorContext(ctx, "Не удалось отправить ответ о mymusic", "err", err)
		}
		return
	}

	uploads := s.listUploads(msg.From.ID)
	cache := s.listCache(msg.From.ID)
	if len(uploads) == 0 && len(cache) == 0 {
		if _, err := s.messenger.ReplyToChat(ctx, msg.Chat.ID, msg.ID, "У вас нет файлов.\n/upload — загрузить\n/find + /download — скачать"); err != nil {
			s.logger.ErrorContext(ctx, "Не удалось отправить пустой ответ mymusic", "err", err)
		}
		return
	}

	files := buildUserMusicFiles(uploads, cache)
	reply, valid := FormatMyMusicReply(userMusicFileNames(files), page)
	if !valid {
		if _, err := s.messenger.ReplyToChat(ctx, msg.Chat.ID, msg.ID, reply); err != nil {
			s.logger.ErrorContext(ctx, "Не удалось отправить ответ о странице mymusic", "err", err)
		}
		return
	}

	oldIDs := s.sessions.Set(msg.Chat.ID, msg.From.ID, files)
	deleteChatMessages(ctx, s.messageDeleter, s.logger, msg.Chat.ID, oldIDs, "my_music_cleanup")
	s.sessions.AddMessage(msg.Chat.ID, msg.ID)

	sentID, err := s.messenger.ReplyToChat(ctx, msg.Chat.ID, msg.ID, reply)
	if err != nil {
		s.logger.ErrorContext(ctx, "Не удалось отправить список файлов", "err", err)
		return
	}
	s.sessions.AddMessage(msg.Chat.ID, sentID)
}

func (s *MyMusicService) Delete(ctx context.Context, chatID int64, messageID int, index int, userID int64) {
	file, err := s.sessions.Get(chatID, userID, index, ErrNoMyMusicSession, ErrIndexOutOfRange)
	if err != nil {
		if msg := myMusicDeleteErrorMessage(index, err); msg != "" {
			if _, replyErr := s.messenger.ReplyToChat(ctx, chatID, messageID, msg); replyErr != nil {
				s.logger.ErrorContext(ctx, "Не удалось отправить сообщение об ошибке удаления", "err", replyErr)
			}
		}
		return
	}

	if err := s.deleteFile(userID, file); err != nil {
		s.logger.ErrorContext(ctx, "Не удалось удалить файл",
			"operation", "delete_my_music",
			"chat_id", chatID,
			"user_id", userID,
			"index", index,
			"path", file.path,
			"source", file.source,
			"err", err,
		)
		if _, replyErr := s.messenger.ReplyToChat(ctx, chatID, messageID, "Не удалось удалить файл"); replyErr != nil {
			s.logger.ErrorContext(ctx, "Не удалось отправить ответ об ошибке удаления", "err", replyErr)
		}
		return
	}

	oldIDs := s.sessions.ClearMessages(chatID)
	deleteChatMessages(ctx, s.messageDeleter, s.logger, chatID, append(oldIDs, messageID), "my_music_cleanup")
	s.sessions.Reset(chatID)

	s.logger.InfoContext(ctx, "Файл пользователя удалён",
		"operation", "delete_my_music",
		"chat_id", chatID,
		"user_id", userID,
		"index", index,
		"path", file.path,
		"source", file.source,
	)
}

func (s *MyMusicService) listUploads(userID int64) []string {
	if s.upload == nil {
		return nil
	}
	return s.upload.ListUserFiles(userID)
}

func (s *MyMusicService) listCache(userID int64) []string {
	if s.search == nil {
		return nil
	}
	return s.search.ListUserCacheFiles(userID)
}

func (s *MyMusicService) deleteFile(userID int64, file userMusicFile) error {
	switch file.source {
	case fileSourceUpload:
		if s.upload == nil {
			return ErrNoMyMusicSession
		}
		return s.upload.deleteOwnedFile(userID, file.path)
	case fileSourceCache:
		if s.search == nil {
			return ErrNoMyMusicSession
		}
		return s.search.deleteOwnedCacheFile(userID, file.path)
	default:
		return ErrNoMyMusicSession
	}
}

func (s *MyMusicService) runSessionCleanup() {
	ticker := time.NewTicker(sessionCleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		for chatID, messageIDs := range s.sessions.ExpireDue(time.Now()) {
			deleteChatMessages(context.Background(), s.messageDeleter, s.logger, chatID, messageIDs, "my_music_cleanup")
		}
	}
}

func buildUserMusicFiles(uploads, cache []string) []userMusicFile {
	files := make([]userMusicFile, 0, len(uploads)+len(cache))
	for _, name := range uploads {
		files = append(files, userMusicFile{path: name, source: fileSourceUpload})
	}
	for _, name := range cache {
		files = append(files, userMusicFile{path: name, source: fileSourceCache})
	}
	return files
}

func userMusicFileNames(files []userMusicFile) []string {
	names := make([]string, len(files))
	for i, file := range files {
		names[i] = file.path
	}
	return names
}

func myMusicDeleteErrorMessage(index int, err error) string {
	switch {
	case errors.Is(err, ErrNoMyMusicSession):
		return "Сначала выполните /mymusic"
	case errors.Is(err, ErrIndexOutOfRange):
		return fmt.Sprintf("Нет файла с номером %d. Выполните /mymusic заново.", index)
	default:
		return ""
	}
}
