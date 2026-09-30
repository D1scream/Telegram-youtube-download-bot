package telegram

import (
	"slices"
	"sync"

	"github.com/go-telegram/bot/models"
)

const historySize = 5

type history struct {
	mu    sync.Mutex
	chats map[int64][]string
}

func newHistory() *history {
	return &history{chats: make(map[int64][]string)}
}

func (h *history) add(chatID int64, line string) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	previous := slices.Clone(h.chats[chatID])
	lines := append(slices.Clone(previous), line)
	h.chats[chatID] = lines[max(0, len(lines)-historySize):]
	return previous
}

func historyLine(msg *models.Message, text string) string {
	return senderName(msg) + ": " + text
}

func senderName(msg *models.Message) string {
	switch {
	case msg.From == nil:
		return msg.Chat.Title
	case msg.From.FirstName != "":
		return msg.From.FirstName
	default:
		return msg.From.Username
	}
}
