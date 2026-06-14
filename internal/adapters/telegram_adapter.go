package adapters

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

const (
	pollTimeout     = time.Minute
	downloadTimeout = 2 * time.Minute
)

type MessageHandler func(ctx context.Context, msg *models.Message)

type TelegramAdapter struct {
	bot        *bot.Bot
	onMessage  MessageHandler
	httpClient *http.Client
}

func NewTelegramAdapter(botToken string) (*TelegramAdapter, error) {
	adapter := &TelegramAdapter{
		httpClient: &http.Client{Timeout: downloadTimeout},
	}

	opts := []bot.Option{
		bot.WithSkipGetMe(),
		bot.WithHTTPClient(pollTimeout, &http.Client{Timeout: pollTimeout + 10*time.Second}),
		bot.WithAllowedUpdates(bot.AllowedUpdates{
			models.AllowedUpdateMessage,
			models.AllowedUpdateChannelPost,
		}),
		bot.WithDefaultHandler(adapter.handleUpdate()),
	}

	b, err := bot.New(botToken, opts...)
	if err != nil {
		return nil, fmt.Errorf("создать Telegram bot: %w", err)
	}

	adapter.bot = b
	return adapter, nil
}

func (t *TelegramAdapter) handleUpdate() bot.HandlerFunc {
	return func(ctx context.Context, _ *bot.Bot, update *models.Update) {
		if t.onMessage == nil {
			return
		}

		msg := update.Message
		if msg == nil {
			msg = update.ChannelPost
		}
		if msg == nil {
			return
		}

		t.onMessage(ctx, msg)
	}
}

func (t *TelegramAdapter) StartPolling(ctx context.Context, handler MessageHandler) error {
	t.onMessage = handler
	if _, err := t.bot.DeleteWebhook(ctx, &bot.DeleteWebhookParams{DropPendingUpdates: true}); err != nil {
		return fmt.Errorf("сбросить очередь Telegram: %w", err)
	}
	t.bot.Start(ctx)
	return nil
}

func (t *TelegramAdapter) FetchFile(ctx context.Context, fileID string) ([]byte, error) {
	file, err := t.bot.GetFile(ctx, &bot.GetFileParams{FileID: fileID})
	if err != nil {
		return nil, fmt.Errorf("получить файл Telegram: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.bot.FileDownloadLink(file), nil)
	if err != nil {
		return nil, fmt.Errorf("создать запрос загрузки: %w", err)
	}

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("скачать файл: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("скачать файл: статус %s", resp.Status)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("прочитать файл: %w", err)
	}
	return data, nil
}

func (t *TelegramAdapter) FetchPhoto(ctx context.Context, msg *models.Message) ([]byte, string, error) {
	if len(msg.Photo) == 0 {
		return nil, "", fmt.Errorf("в сообщении нет фото")
	}

	fileID := msg.Photo[len(msg.Photo)-1].FileID
	data, err := t.FetchFile(ctx, fileID)
	if err != nil {
		return nil, "", err
	}
	return data, "image/jpeg", nil
}

func (t *TelegramAdapter) SendToChat(ctx context.Context, chatID int64, message string) error {
	_, err := t.sendMessage(ctx, &bot.SendMessageParams{
		ChatID: chatID,
		Text:   message,
	})
	return err
}

func (t *TelegramAdapter) ReplyToChat(ctx context.Context, chatID int64, messageID int, message string) (int, error) {
	return t.sendMessage(ctx, &bot.SendMessageParams{
		ChatID: chatID,
		Text:   message,
		ReplyParameters: &models.ReplyParameters{
			MessageID:                messageID,
			AllowSendingWithoutReply: true,
		},
	})
}

func (t *TelegramAdapter) SendDocument(ctx context.Context, chatID int64, filename string, file *os.File) (int, error) {
	msg, err := t.bot.SendDocument(ctx, &bot.SendDocumentParams{
		ChatID: chatID,
		Document: &models.InputFileUpload{
			Filename: filename,
			Data:     file,
		},
	})
	if err != nil {
		return 0, fmt.Errorf("отправить документ Telegram: %w", err)
	}
	return msg.ID, nil
}

func (t *TelegramAdapter) ReplyDocument(ctx context.Context, chatID int64, messageID int, filename string, file *os.File) (int, error) {
	msg, err := t.bot.SendDocument(ctx, &bot.SendDocumentParams{
		ChatID: chatID,
		Document: &models.InputFileUpload{
			Filename: filename,
			Data:     file,
		},
		ReplyParameters: &models.ReplyParameters{
			MessageID:                messageID,
			AllowSendingWithoutReply: true,
		},
	})
	if err != nil {
		return 0, fmt.Errorf("отправить документ Telegram: %w", err)
	}
	return msg.ID, nil
}

func (t *TelegramAdapter) ReplyVideo(ctx context.Context, chatID int64, messageID int, filename string, file *os.File) (int, error) {
	msg, err := t.bot.SendVideo(ctx, &bot.SendVideoParams{
		ChatID: chatID,
		Video: &models.InputFileUpload{
			Filename: filename,
			Data:     file,
		},
		SupportsStreaming: true,
		ReplyParameters: &models.ReplyParameters{
			MessageID:                messageID,
			AllowSendingWithoutReply: true,
		},
	})
	if err != nil {
		return 0, fmt.Errorf("отправить видео Telegram: %w", err)
	}
	return msg.ID, nil
}

func (t *TelegramAdapter) ReplyAudio(ctx context.Context, chatID int64, messageID int, filename string, file *os.File) (int, error) {
	msg, err := t.bot.SendAudio(ctx, &bot.SendAudioParams{
		ChatID: chatID,
		Audio: &models.InputFileUpload{
			Filename: filename,
			Data:     file,
		},
		ReplyParameters: &models.ReplyParameters{
			MessageID:                messageID,
			AllowSendingWithoutReply: true,
		},
	})
	if err != nil {
		return 0, fmt.Errorf("отправить аудио Telegram: %w", err)
	}
	return msg.ID, nil
}

func (t *TelegramAdapter) DeleteMessages(ctx context.Context, chatID int64, messageIDs []int) error {
	var lastErr error
	deleted := 0

	for _, id := range messageIDs {
		if id == 0 {
			continue
		}
		if _, err := t.bot.DeleteMessage(ctx, &bot.DeleteMessageParams{
			ChatID:    chatID,
			MessageID: id,
		}); err != nil {
			lastErr = fmt.Errorf("удалить сообщение Telegram %d: %w", id, err)
			continue
		}
		deleted++
	}

	if lastErr != nil && deleted == 0 {
		return lastErr
	}
	return nil
}

func (t *TelegramAdapter) sendMessage(ctx context.Context, params *bot.SendMessageParams) (int, error) {
	msg, err := t.bot.SendMessage(ctx, params)
	if err != nil {
		return 0, fmt.Errorf("отправить сообщение Telegram: %w", err)
	}
	return msg.ID, nil
}
