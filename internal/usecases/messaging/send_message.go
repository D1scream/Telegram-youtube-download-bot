package messaging

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

type MessageSender interface {
	SendToChat(ctx context.Context, chatID int64, message string) error
}

type SendMessageService struct {
	sender MessageSender
	logger *slog.Logger
}

func NewSendMessageService(sender MessageSender, logger *slog.Logger) *SendMessageService {
	return &SendMessageService{
		sender: sender,
		logger: logger.With("component", "send_message_service"),
	}
}

func (s *SendMessageService) SendTo(ctx context.Context, chatID int64, text string) error {
	text = strings.TrimSpace(text)
	if chatID == 0 {
		return ErrInvalidChatID
	}
	if text == "" {
		return ErrEmptyMessage
	}

	s.logger.InfoContext(ctx, "Отправка сообщения", "operation", "send_message", "chat_id", chatID)

	if err := s.sender.SendToChat(ctx, chatID, text); err != nil {
		return fmt.Errorf("отправить сообщение: %w", err)
	}

	s.logger.InfoContext(ctx, "Сообщение отправлено", "operation", "send_message", "chat_id", chatID)
	return nil
}
