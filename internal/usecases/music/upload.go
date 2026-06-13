package music

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-telegram/bot/models"
)

const telegramBotMaxFileBytes = 20 << 20 // лимит Bot API на скачивание файла

type FileFetcher interface {
	FetchFile(ctx context.Context, fileID string) ([]byte, error)
}

type Messenger interface {
	ReplyToChat(ctx context.Context, chatID int64, messageID int, text string) (int, error)
}

type UploadMusicService struct {
	fetcher        FileFetcher
	messenger      Messenger
	uploadDir      string
	allowedFormats []string
	registry       *userFileRegistry
	logger         *slog.Logger
}

func NewUploadMusicService(
	fetcher FileFetcher,
	messenger Messenger,
	uploadDir string,
	allowedFormats []string,
	logger *slog.Logger,
) *UploadMusicService {
	uploadDir = strings.TrimSpace(uploadDir)
	service := &UploadMusicService{
		fetcher:        fetcher,
		messenger:      messenger,
		uploadDir:      uploadDir,
		allowedFormats: allowedFormats,
		registry:       newUserFileRegistry(uploadDir, ".upload_registry.json"),
		logger:         logger.With("component", "upload_music_service"),
	}
	if err := service.registry.load(); err != nil {
		logger.Warn("Не удалось загрузить реестр загрузок", "err", err)
	}
	return service
}

func (s *UploadMusicService) Upload(ctx context.Context, msg *models.Message) {
	fileID, filename, fileSize, ok := attachmentFromMessage(msg)
	if !ok {
		return
	}

	if fileSize > telegramBotMaxFileBytes {
		s.logger.InfoContext(ctx, "Файл слишком большой для загрузки",
			"operation", "upload_music",
			"filename", filename,
			"bytes", fileSize,
			"limit", telegramBotMaxFileBytes,
		)
		reply := fmt.Sprintf(
			"Файл слишком большой (%s). Telegram позволяет боту скачивать до %s.",
			formatSize(fileSize),
			formatSize(telegramBotMaxFileBytes),
		)
		if _, replyErr := s.messenger.ReplyToChat(ctx, msg.Chat.ID, msg.ID, reply); replyErr != nil {
			s.logger.ErrorContext(ctx, "Не удалось отправить ответ о размере файла", "err", replyErr)
		}
		return
	}

	ext, err := allowedExtension(filename, s.allowedFormats)
	if err != nil {
		s.logger.InfoContext(ctx, "Неподдерживаемый формат загрузки",
			"operation", "upload_music",
			"filename", filename,
			"err", err,
		)
		if _, replyErr := s.messenger.ReplyToChat(ctx, msg.Chat.ID, msg.ID, "Поддерживаются только аудиофайлы: "+formatList(s.allowedFormats)); replyErr != nil {
			s.logger.ErrorContext(ctx, "Не удалось отправить ответ о формате", "err", replyErr)
		}
		return
	}
	filename = strings.TrimSuffix(filepath.Base(filename), ext) + ext

	data, err := s.fetcher.FetchFile(ctx, fileID)
	if err != nil {
		s.logger.ErrorContext(ctx, "Не удалось скачать файл из Telegram",
			"operation", "upload_music",
			"filename", filename,
			"err", err,
		)
		if _, replyErr := s.messenger.ReplyToChat(ctx, msg.Chat.ID, msg.ID, uploadFetchErrorMessage(err)); replyErr != nil {
			s.logger.ErrorContext(ctx, "Не удалось отправить ответ об ошибке загрузки", "err", replyErr)
		}
		return
	}

	if err := os.MkdirAll(s.uploadDir, 0o755); err != nil {
		s.logger.ErrorContext(ctx, "Не удалось создать директорию загрузок", "dir", s.uploadDir, "err", err)
		return
	}

	destPath := uniquePath(s.uploadDir, filename)
	if err := os.WriteFile(destPath, data, 0o644); err != nil {
		s.logger.ErrorContext(ctx, "Не удалось сохранить файл",
			"operation", "upload_music",
			"path", destPath,
			"err", err,
		)
		if _, replyErr := s.messenger.ReplyToChat(ctx, msg.Chat.ID, msg.ID, "Не удалось сохранить файл"); replyErr != nil {
			s.logger.ErrorContext(ctx, "Не удалось отправить ответ об ошибке сохранения", "err", replyErr)
		}
		return
	}

	basename := filepath.Base(destPath)
	if msg.From != nil {
		if err := s.registry.add(msg.From.ID, basename); err != nil {
			s.logger.WarnContext(ctx, "Не удалось записать владельца загрузки",
				"operation", "upload_music",
				"user_id", msg.From.ID,
				"filename", basename,
				"err", err,
			)
		}
	}

	s.logger.InfoContext(ctx, "Трек загружен",
		"operation", "upload_music",
		"chat_id", msg.Chat.ID,
		"user_id", userID(msg.From),
		"path", destPath,
		"bytes", len(data),
	)

	reply := fmt.Sprintf(
		"Сохранено: %s\nФайл доступен в /find\n/mymusic — ваши файлы",
		basename,
	)
	if _, err := s.messenger.ReplyToChat(ctx, msg.Chat.ID, msg.ID, reply); err != nil {
		s.logger.ErrorContext(ctx, "Не удалось отправить подтверждение загрузки", "err", err)
	}
}

func (s *UploadMusicService) ListUserFiles(userID int64) []string {
	return s.registry.list(userID)
}

func (s *UploadMusicService) deleteOwnedFile(userID int64, basename string) error {
	name, ok := safeUploadBasename(basename)
	if !ok || !s.registry.owns(userID, name) {
		return ErrNoMyMusicSession
	}

	path := filepath.Join(s.uploadDir, name)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	if _, err := s.registry.remove(userID, name); err != nil {
		s.logger.WarnContext(context.Background(), "Файл удалён, но реестр загрузок не обновлён",
			"operation", "delete_upload",
			"user_id", userID,
			"filename", name,
			"err", err,
		)
	}
	return nil
}

func userID(from *models.User) int64 {
	if from == nil {
		return 0
	}
	return from.ID
}

func attachmentFromMessage(msg *models.Message) (fileID, filename string, fileSize int64, ok bool) {
	switch {
	case msg.Document != nil:
		return msg.Document.FileID, msg.Document.FileName, int64(msg.Document.FileSize), true
	case msg.Audio != nil:
		name := msg.Audio.FileName
		if name == "" {
			name = audioFilename(msg.Audio)
		}
		return msg.Audio.FileID, name, int64(msg.Audio.FileSize), true
	default:
		return "", "", 0, false
	}
}

func uploadFetchErrorMessage(err error) string {
	if err == nil {
		return "Не удалось загрузить файл"
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "file is too big"), strings.Contains(msg, "too big"):
		return fmt.Sprintf(
			"Файл слишком большой. Telegram позволяет боту скачивать до %s.",
			formatSize(telegramBotMaxFileBytes),
		)
	default:
		return "Не удалось загрузить файл"
	}
}

func audioFilename(audio *models.Audio) string {
	parts := make([]string, 0, 2)
	if performer := strings.TrimSpace(audio.Performer); performer != "" {
		parts = append(parts, performer)
	}
	if title := strings.TrimSpace(audio.Title); title != "" {
		parts = append(parts, title)
	}
	if len(parts) == 0 {
		return "audio.mp3"
	}
	name := strings.Join(parts, " - ")
	if ext := strings.ToLower(filepath.Ext(name)); ext == "" {
		if mime := strings.ToLower(audio.MimeType); strings.Contains(mime, "mpeg") {
			name += ".mp3"
		} else if strings.Contains(mime, "ogg") {
			name += ".ogg"
		} else if strings.Contains(mime, "flac") {
			name += ".flac"
		} else if strings.Contains(mime, "webm") {
			name += ".webm"
		} else {
			name += ".mp3"
		}
	}
	return name
}

func allowedExtension(filename string, formats []string) (string, error) {
	ext := strings.ToLower(filepath.Ext(filepath.Base(filename)))
	if ext == "" {
		return "", fmt.Errorf("нет расширения")
	}
	for _, format := range formats {
		if ext == format {
			return ext, nil
		}
	}
	return "", fmt.Errorf("формат %s не разрешён", ext)
}

func formatList(formats []string) string {
	parts := make([]string, len(formats))
	for i, format := range formats {
		parts[i] = strings.TrimPrefix(format, ".")
	}
	return strings.Join(parts, ", ")
}

func uniquePath(dir, name string) string {
	path := filepath.Join(dir, name)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return path
	}

	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 1; ; i++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s (%d)%s", base, i, ext))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}
