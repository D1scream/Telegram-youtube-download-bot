package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"telegram-bot/internal/msglog"
)

const deleteBatchSize = 100

func (t *Bot) SetMessageLog(w *msglog.Writer, logger *slog.Logger) {
	t.msgLog = w
	t.logger = logger.With("component", "message_log")
}

func (t *Bot) record(msg *models.Message) {
	if t.msgLog == nil || msg == nil {
		return
	}
	if err := t.msgLog.Append(logEntry(msg)); err != nil {
		t.logger.Error("Не удалось записать сообщение в лог", "err", err)
	}
}

func logEntry(msg *models.Message) msglog.Entry {
	e := msglog.Entry{
		Time:      time.Unix(int64(msg.Date), 0).UTC(),
		ChatID:    msg.Chat.ID,
		Chat:      chatName(msg.Chat),
		MessageID: msg.ID,
		Kind:      messageKind(msg),
		Text:      msg.Text,
	}
	if e.Text == "" {
		e.Text = msg.Caption
	}
	switch {
	case msg.From != nil:
		e.FromID = msg.From.ID
		e.From = msg.From.Username
		e.Name = msg.From.FirstName
	case msg.SenderChat != nil:
		e.FromID = msg.SenderChat.ID
		e.From = msg.SenderChat.Username
		e.Name = msg.SenderChat.Title
	}
	return e
}

func chatName(chat models.Chat) string {
	switch {
	case chat.Title != "":
		return chat.Title
	case chat.Username != "":
		return "@" + chat.Username
	default:
		return chat.FirstName
	}
}

func messageKind(msg *models.Message) string {
	switch {
	case msg.Text != "":
		return "text"
	case len(msg.Photo) > 0:
		return "photo"
	case msg.Video != nil:
		return "video"
	case msg.Animation != nil:
		return "animation"
	case msg.Audio != nil:
		return "audio"
	case msg.Voice != nil:
		return "voice"
	case msg.VideoNote != nil:
		return "video_note"
	case msg.Document != nil:
		return "document"
	case msg.Sticker != nil:
		return "sticker"
	case msg.Poll != nil:
		return "poll"
	case msg.Location != nil:
		return "location"
	case msg.Contact != nil:
		return "contact"
	default:
		return "other"
	}
}

func (t *Bot) DeleteMessages(ctx context.Context, chatID int64, ids []int) (deleted []int, errs []error) {
	for start := 0; start < len(ids); start += deleteBatchSize {
		batch := ids[start:min(start+deleteBatchSize, len(ids))]
		_, err := t.client.DeleteMessages(ctx, &bot.DeleteMessagesParams{ChatID: chatID, MessageIDs: batch})
		if err == nil {
			deleted = append(deleted, batch...)
			continue
		}
		for _, id := range batch {
			if err := t.DeleteMessage(ctx, chatID, id); err != nil {
				errs = append(errs, fmt.Errorf("сообщение %d: %w", id, err))
				continue
			}
			deleted = append(deleted, id)
		}
	}
	return deleted, errs
}
