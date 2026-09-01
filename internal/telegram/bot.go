package telegram

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

const pollTimeout = time.Minute

type MessageHandler func(ctx context.Context, msg *models.Message)

type Bot struct {
	client    *bot.Bot
	onMessage MessageHandler
}

func New(token string) (*Bot, error) {
	t := &Bot{}
	opts := []bot.Option{
		bot.WithSkipGetMe(),
		bot.WithHTTPClient(pollTimeout, &http.Client{Timeout: pollTimeout + 10*time.Second}),
		bot.WithAllowedUpdates(bot.AllowedUpdates{
			models.AllowedUpdateMessage,
			models.AllowedUpdateChannelPost,
		}),
		bot.WithDefaultHandler(t.handleUpdate),
	}
	client, err := bot.New(token, opts...)
	if err != nil {
		return nil, fmt.Errorf("создать Telegram bot: %w", err)
	}
	t.client = client
	return t, nil
}

func (t *Bot) handleUpdate(ctx context.Context, _ *bot.Bot, update *models.Update) {
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

func (t *Bot) Start(ctx context.Context, handler MessageHandler) error {
	t.onMessage = handler
	if _, err := t.client.DeleteWebhook(ctx, &bot.DeleteWebhookParams{DropPendingUpdates: true}); err != nil {
		return fmt.Errorf("сбросить очередь Telegram: %w", err)
	}
	t.client.Start(ctx)
	return nil
}

func (t *Bot) ReplyToChat(ctx context.Context, chatID int64, messageID int, message string) (int, error) {
	return t.sendMessage(ctx, &bot.SendMessageParams{
		ChatID: chatID,
		Text:   message,
		ReplyParameters: &models.ReplyParameters{
			MessageID:                messageID,
			AllowSendingWithoutReply: true,
		},
	})
}

func (t *Bot) ReplyVideo(ctx context.Context, chatID int64, messageID int, filename string, file *os.File) (int, error) {
	msg, err := t.client.SendVideo(ctx, &bot.SendVideoParams{
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

func (t *Bot) ReplyAudio(ctx context.Context, chatID int64, messageID int, filename string, file *os.File) (int, error) {
	msg, err := t.client.SendAudio(ctx, &bot.SendAudioParams{
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

func (t *Bot) sendMessage(ctx context.Context, params *bot.SendMessageParams) (int, error) {
	msg, err := t.client.SendMessage(ctx, params)
	if err != nil {
		return 0, fmt.Errorf("отправить сообщение Telegram: %w", err)
	}
	return msg.ID, nil
}
