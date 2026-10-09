package imposter

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"
)

const (
	TopicPrefix = "topic:"

	MaxTopics        = 20
	MaxTopicWords    = 500
	MaxTopicNameLen  = 30
	topicNameBadChar = ":\n\r\t"
)

var (
	ErrBadTopicName  = errors.New("название темы: до 30 символов, без двоеточия и переносов строк")
	ErrNoTopic       = errors.New("такой темы нет")
	ErrTooManyTopics = errors.New("слишком много тем")
	ErrTopicFull     = errors.New("в теме слишком много слов")
)

// TopicList возвращает значение Settings.List для темы.
func TopicList(name string) string { return TopicPrefix + name }

// TopicName выделяет название темы из значения Settings.List.
func TopicName(list string) (string, bool) {
	return strings.CutPrefix(list, TopicPrefix)
}

type chatState struct {
	Settings *Settings `json:"settings,omitempty"`
}

// Store хранит настройки чатов, общие темы и общий чёрный список в json-файле.
// Все изменения сразу сохраняются на диск.
type Store struct {
	mu        sync.Mutex
	path      string
	chats     map[int64]*chatState
	topics    map[string][]string
	blacklist map[int64]string
}

type storeFile struct {
	Chats     map[int64]*chatState `json:"chats"`
	Topics    map[string][]string  `json:"topics,omitempty"`
	Blacklist map[int64]string     `json:"blacklist,omitempty"`
}

// OpenStore читает состояние из файла; если файла нет, начинает с пустого.
// Повреждённый файл не перезаписывается, возвращается ошибка.
func OpenStore(path string) (*Store, error) {
	s := &Store{
		path:      path,
		chats:     make(map[int64]*chatState),
		topics:    make(map[string][]string),
		blacklist: make(map[int64]string),
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("прочитать состояние игры: %w", err)
	}
	var f storeFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("разобрать состояние игры %s: %w", path, err)
	}
	if f.Chats != nil {
		s.chats = f.Chats
	}
	if f.Topics != nil {
		s.topics = f.Topics
	}
	if f.Blacklist != nil {
		s.blacklist = f.Blacklist
	}
	return s, nil
}

func (s *Store) chat(id int64) *chatState {
	c := s.chats[id]
	if c == nil {
		c = &chatState{}
		s.chats[id] = c
	}
	return c
}

func (s *Store) save() error {
	data, err := json.MarshalIndent(storeFile{Chats: s.chats, Topics: s.topics, Blacklist: s.blacklist}, "", "  ")
	if err != nil {
		return fmt.Errorf("сериализовать состояние игры: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("создать каталог состояния игры: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("записать состояние игры: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("сохранить состояние игры: %w", err)
	}
	return nil
}

// findTopic ищет тему без учёта регистра и возвращает её сохранённое название.
func (s *Store) findTopic(name string) (string, bool) {
	for key := range s.topics {
		if strings.EqualFold(key, name) {
			return key, true
		}
	}
	return "", false
}

// Settings возвращает настройки чата или def. Выбранная, но уже удалённая тема заменяется скачанным списком.
func (s *Store) Settings(chatID int64, def Settings) Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.chats[chatID]
	if c == nil || c.Settings == nil {
		return def
	}
	out := *c.Settings
	out.Secret = false
	out.Rounds = min(max(out.Rounds, 1), MaxRounds)
	if name, ok := TopicName(out.List); ok {
		if key, found := s.findTopic(name); found {
			out.List = TopicList(key)
		} else {
			out.List = ListDownloaded
		}
	}
	if out.List == "" {
		out.List = def.List
	}
	if out.Tie == "" {
		out.Tie = def.Tie
	}
	return out
}

func (s *Store) SetSettings(chatID int64, settings Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	settings.Secret = false
	s.chat(chatID).Settings = &settings
	return s.save()
}

// TopicNames возвращает названия тем по алфавиту.
func (s *Store) TopicNames() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	names := make([]string, 0, len(s.topics))
	for name := range s.topics {
		names = append(names, name)
	}
	slices.SortFunc(names, func(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) })
	return names
}

// TopicWords возвращает копию слов темы.
func (s *Store) TopicWords(name string) ([]string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key, ok := s.findTopic(name)
	if !ok {
		return nil, false
	}
	return slices.Clone(s.topics[key]), true
}

func validTopicName(name string) bool {
	n := utf8.RuneCountInString(name)
	return n > 0 && n <= MaxTopicNameLen && !strings.ContainsAny(name, topicNameBadChar) && name == strings.TrimSpace(name)
}

// AddTopicWords добавляет слова в тему, создавая её при необходимости. Возвращает число новых слов.
func (s *Store) AddTopicWords(name string, words []string) (int, error) {
	name = strings.TrimSpace(name)
	if !validTopicName(name) {
		return 0, ErrBadTopicName
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	key, exists := s.findTopic(name)
	if !exists {
		if len(s.topics) >= MaxTopics {
			return 0, ErrTooManyTopics
		}
		key = name
	}
	have := slices.Clone(s.topics[key])
	added := 0
	for _, w := range words {
		if slices.Contains(have, w) {
			continue
		}
		if len(have) >= MaxTopicWords {
			return 0, ErrTopicFull
		}
		have = append(have, w)
		added++
	}
	s.topics[key] = have
	return added, s.save()
}

// RemoveTopicWords удаляет слова из темы. Возвращает число удалённых слов.
func (s *Store) RemoveTopicWords(name string, words []string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key, ok := s.findTopic(name)
	if !ok {
		return 0, ErrNoTopic
	}
	before := len(s.topics[key])
	s.topics[key] = slices.DeleteFunc(slices.Clone(s.topics[key]), func(w string) bool { return slices.Contains(words, w) })
	return before - len(s.topics[key]), s.save()
}

func (s *Store) DeleteTopic(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key, ok := s.findTopic(name)
	if !ok {
		return ErrNoTopic
	}
	delete(s.topics, key)
	return s.save()
}

// Ban добавляет игрока в общий чёрный список. Имя нужно только для вывода списка.
func (s *Store) Ban(id int64, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.blacklist[id] = name
	return s.save()
}

// Unban убирает игрока из чёрного списка. Возвращает false, если его там не было.
func (s *Store) Unban(id int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.blacklist[id]; !ok {
		return false, nil
	}
	delete(s.blacklist, id)
	return true, s.save()
}

func (s *Store) Banned(id int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.blacklist[id]
	return ok
}

// Blacklist возвращает копию чёрного списка: id игрока -> имя.
func (s *Store) Blacklist() map[int64]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return maps.Clone(s.blacklist)
}
