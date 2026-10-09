package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"telegram-bot/internal/msglog"
)

const pollTimeout = time.Minute

type MessageHandler func(ctx context.Context, msg *models.Message)

type CallbackHandler func(ctx context.Context, query *models.CallbackQuery)

type Bot struct {
	client     *bot.Bot
	onMessage  MessageHandler
	onCallback CallbackHandler
	msgLog     *msglog.Writer
	logger     *slog.Logger
}

func New(token string) (*Bot, error) {
	t := &Bot{}
	opts := []bot.Option{
		bot.WithSkipGetMe(),
		bot.WithHTTPClient(pollTimeout, &http.Client{Timeout: pollTimeout + 10*time.Second}),
		bot.WithAllowedUpdates(bot.AllowedUpdates{
			models.AllowedUpdateMessage,
			models.AllowedUpdateChannelPost,
			models.AllowedUpdateCallbackQuery,
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
	if update.CallbackQuery != nil {
		if t.onCallback != nil {
			t.onCallback(ctx, update.CallbackQuery)
		}
		return
	}
	msg := update.Message
	if msg == nil {
		msg = update.ChannelPost
	}
	if msg == nil {
		return
	}
	t.record(msg)
	if t.onMessage != nil {
		t.onMessage(ctx, msg)
	}
}

func (t *Bot) Start(ctx context.Context, handler MessageHandler, callbacks CallbackHandler) error {
	t.onMessage = handler
	t.onCallback = callbacks
	if _, err := t.client.DeleteWebhook(ctx, &bot.DeleteWebhookParams{DropPendingUpdates: true}); err != nil {
		return fmt.Errorf("сбросить очередь Telegram: %w", err)
	}
	t.client.Start(ctx)
	return nil
}

func replyTo(messageID int) *models.ReplyParameters {
	if messageID == 0 {
		return nil
	}
	return &models.ReplyParameters{MessageID: messageID, AllowSendingWithoutReply: true}
}

func (t *Bot) ReplyToChat(ctx context.Context, chatID int64, messageID int, message string) (int, error) {
	return t.sendMessage(ctx, &bot.SendMessageParams{
		ChatID:          chatID,
		Text:            message,
		ReplyParameters: replyTo(messageID),
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
		ReplyParameters:   replyTo(messageID),
	})
	if err != nil {
		return 0, fmt.Errorf("отправить видео Telegram: %w", err)
	}
	t.record(msg)
	return msg.ID, nil
}

func (t *Bot) ReplyAudio(ctx context.Context, chatID int64, messageID int, filename string, file *os.File) (int, error) {
	msg, err := t.client.SendAudio(ctx, &bot.SendAudioParams{
		ChatID: chatID,
		Audio: &models.InputFileUpload{
			Filename: filename,
			Data:     file,
		},
		ReplyParameters: replyTo(messageID),
	})
	if err != nil {
		return 0, fmt.Errorf("отправить аудио Telegram: %w", err)
	}
	t.record(msg)
	return msg.ID, nil
}

func (t *Bot) SendHTML(ctx context.Context, chatID int64, text string, markup *models.InlineKeyboardMarkup) (int, error) {
	params := &bot.SendMessageParams{ChatID: chatID, Text: text, ParseMode: models.ParseModeHTML}
	if markup != nil {
		params.ReplyMarkup = markup
	}
	return t.sendMessage(ctx, params)
}

func (t *Bot) EditHTML(ctx context.Context, chatID int64, messageID int, text string, markup *models.InlineKeyboardMarkup) error {
	params := &bot.EditMessageTextParams{ChatID: chatID, MessageID: messageID, Text: text, ParseMode: models.ParseModeHTML}
	if markup != nil {
		params.ReplyMarkup = markup
	}
	if _, err := t.client.EditMessageText(ctx, params); err != nil && !strings.Contains(err.Error(), "message is not modified") {
		return fmt.Errorf("изменить сообщение Telegram: %w", err)
	}
	return nil
}

func (t *Bot) AnswerCallback(ctx context.Context, queryID, text string, alert bool) error {
	_, err := t.client.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
		CallbackQueryID: queryID,
		Text:            text,
		ShowAlert:       alert,
	})
	if err != nil {
		return fmt.Errorf("ответить на callback Telegram: %w", err)
	}
	return nil
}

func (t *Bot) DeleteMessage(ctx context.Context, chatID int64, messageID int) error {
	_, err := t.client.DeleteMessage(ctx, &bot.DeleteMessageParams{
		ChatID:    chatID,
		MessageID: messageID,
	})
	if err != nil {
		return fmt.Errorf("удалить сообщение Telegram: %w", err)
	}
	return nil
}

func (t *Bot) sendMessage(ctx context.Context, params *bot.SendMessageParams) (int, error) {
	msg, err := t.client.SendMessage(ctx, params)
	if err != nil {
		return 0, fmt.Errorf("отправить сообщение Telegram: %w", err)
	}
	t.record(msg)
	return msg.ID, nil
}
