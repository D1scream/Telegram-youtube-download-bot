package music

import (
	"context"
	"os"
	"time"
)

const sessionCleanupInterval = time.Minute

type MessageDeleter interface {
	DeleteMessages(ctx context.Context, chatID int64, messageIDs []int) error
}

func (s *SearchMusicService) runSessionCleanup() {
	ticker := time.NewTicker(sessionCleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		for chatID, messageIDs := range s.sessions.ExpireDue(time.Now()) {
			deleteChatMessages(context.Background(), s.messageDeleter, s.logger, chatID, messageIDs, "session_cleanup")
		}
	}
}

func (s *SearchMusicService) deleteSessionMessages(ctx context.Context, chatID int64, messageIDs []int) {
	deleteChatMessages(ctx, s.messageDeleter, s.logger, chatID, messageIDs, "session_cleanup")
}

func (s *SearchMusicService) trackUserMessage(chatID int64, messageID int) {
	s.sessions.AddMessage(chatID, messageID)
}

func (s *SearchMusicService) replyToChat(ctx context.Context, chatID int64, replyTo int, text string) error {
	for _, chunk := range splitTelegramMessages(text, telegramMaxMessageLen-64) {
		sentID, err := s.messenger.ReplyToChat(ctx, chatID, replyTo, chunk)
		if err != nil {
			return err
		}
		s.sessions.AddMessage(chatID, sentID)
	}
	return nil
}

func (s *SearchMusicService) replyDocument(ctx context.Context, chatID int64, replyTo int, filename string, file *os.File) error {
	sentID, err := s.messenger.ReplyDocument(ctx, chatID, replyTo, filename, file)
	if err != nil {
		return err
	}
	s.sessions.AddMessage(chatID, sentID)
	return nil
}
