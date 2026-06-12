package music

import (
	"context"
	"log/slog"
)

func deleteChatMessages(
	ctx context.Context,
	deleter MessageDeleter,
	logger *slog.Logger,
	chatID int64,
	messageIDs []int,
	operation string,
) {
	if deleter == nil || len(messageIDs) == 0 {
		return
	}
	if err := deleter.DeleteMessages(ctx, chatID, messageIDs); err != nil {
		logger.WarnContext(ctx, "Не удалось удалить сообщения чата",
			"operation", operation,
			"chat_id", chatID,
			"count", len(messageIDs),
			"err", err,
		)
		return
	}
	logger.InfoContext(ctx, "Сообщения чата удалены",
		"operation", operation,
		"chat_id", chatID,
		"count", len(messageIDs),
	)
}
