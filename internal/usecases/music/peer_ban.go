package music

import (
	"context"
	"strings"
	"sync"
	"time"

	"telegram-bot/internal/entities"
)

const peerBanDuration = 7 * 24 * time.Hour

const peerBanCleanupInterval = time.Hour

type PeerModerator interface {
	GetBlacklistedPeers(ctx context.Context) (map[string]struct{}, error)
	BanPeer(ctx context.Context, username string) error
	UnbanPeer(ctx context.Context, username string) error
}

type peerBanExpiryStore struct {
	mu    sync.Mutex
	until map[string]time.Time
}

func newPeerBanExpiryStore() *peerBanExpiryStore {
	return &peerBanExpiryStore{until: make(map[string]time.Time)}
}

func (p *peerBanExpiryStore) Schedule(username string) time.Time {
	username = normalizePeerUsername(username)
	until := time.Now().Add(peerBanDuration)

	p.mu.Lock()
	defer p.mu.Unlock()
	p.until[username] = until
	return until
}

func (p *peerBanExpiryStore) Expired(now time.Time) []string {
	p.mu.Lock()
	defer p.mu.Unlock()

	expired := make([]string, 0)
	for username, until := range p.until {
		if now.After(until) {
			expired = append(expired, username)
			delete(p.until, username)
		}
	}
	return expired
}

func normalizePeerUsername(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}

func filterBannedPeers(tracks []entities.Track, banned map[string]struct{}) []entities.Track {
	if len(banned) == 0 {
		return tracks
	}

	filtered := make([]entities.Track, 0, len(tracks))
	for _, track := range tracks {
		if _, ok := banned[normalizePeerUsername(track.Username)]; ok {
			continue
		}
		filtered = append(filtered, track)
	}
	return filtered
}
