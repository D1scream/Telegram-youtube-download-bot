package music

import (
	"strconv"
	"strings"
	"sync"

	"telegram-bot/internal/entities"
)

type pendingDownload struct {
	chatID    int64
	messageID int
	userID    int64
}

type pendingStore struct {
	mu      sync.Mutex
	pending map[string]pendingDownload
}

func newPendingStore() *pendingStore {
	return &pendingStore{pending: make(map[string]pendingDownload)}
}

func pendingKey(track entities.Track) string {
	return track.Username + "\x00" + normalizePath(track.Filename) + "\x00" + strconv.FormatInt(track.Size, 10)
}

func (p *pendingStore) Register(track entities.Track, chatID int64, messageID int, userID int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pending[pendingKey(track)] = pendingDownload{chatID: chatID, messageID: messageID, userID: userID}
}

func (p *pendingStore) Take(track entities.Track) (pendingDownload, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	key := pendingKey(track)
	download, ok := p.pending[key]
	if ok {
		delete(p.pending, key)
	}
	return download, ok
}

func (p *pendingStore) Unregister(track entities.Track) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.pending, pendingKey(track))
}

func normalizePath(path string) string {
	return strings.ToLower(strings.ReplaceAll(path, "\\", "/"))
}
