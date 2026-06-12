package music

import (
	"sync"
	"time"
)

const sessionTTL = 30 * time.Minute

type chatSession[T any] struct {
	ownerID    int64
	items      []T
	messageIDs []int
	createdAt  time.Time
}

type chatSessionStore[T any] struct {
	mu       sync.Mutex
	sessions map[int64]chatSession[T]
}

func newChatSessionStore[T any]() *chatSessionStore[T] {
	return &chatSessionStore[T]{sessions: make(map[int64]chatSession[T])}
}

func (s *chatSessionStore[T]) Set(chatID, ownerID int64, items []T) []int {
	s.mu.Lock()
	defer s.mu.Unlock()

	var oldIDs []int
	if old, ok := s.sessions[chatID]; ok {
		oldIDs = append([]int(nil), old.messageIDs...)
	}

	copied := append([]T(nil), items...)
	s.sessions[chatID] = chatSession[T]{
		ownerID:   ownerID,
		items:     copied,
		createdAt: time.Now(),
	}
	return oldIDs
}

func (s *chatSessionStore[T]) AddMessage(chatID int64, messageID int) {
	if messageID == 0 {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[chatID]
	if !ok {
		return
	}
	session.messageIDs = append(session.messageIDs, messageID)
	s.sessions[chatID] = session
}

func (s *chatSessionStore[T]) ClearMessages(chatID int64) []int {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[chatID]
	if !ok {
		return nil
	}

	oldIDs := append([]int(nil), session.messageIDs...)
	session.messageIDs = nil
	s.sessions[chatID] = session
	return oldIDs
}

func (s *chatSessionStore[T]) Reset(chatID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, chatID)
}

func (s *chatSessionStore[T]) Get(chatID, ownerID int64, index int, errSession, errRange error) (T, error) {
	var zero T

	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[chatID]
	if !ok || time.Since(session.createdAt) > sessionTTL {
		return zero, errSession
	}
	if session.ownerID != 0 && session.ownerID != ownerID {
		return zero, errSession
	}
	if index < 1 || index > len(session.items) {
		return zero, errRange
	}
	return session.items[index-1], nil
}

func (s *chatSessionStore[T]) ExpireDue(now time.Time) map[int64][]int {
	s.mu.Lock()
	defer s.mu.Unlock()

	expired := make(map[int64][]int)
	for chatID, session := range s.sessions {
		if now.Sub(session.createdAt) > sessionTTL {
			expired[chatID] = append([]int(nil), session.messageIDs...)
			delete(s.sessions, chatID)
		}
	}
	return expired
}
