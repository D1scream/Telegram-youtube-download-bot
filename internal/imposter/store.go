package imposter

import (
	"encoding/json"
	"errors"
	"fmt"
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
	ErrTooManyTopics = errors.New("слишком много тем в чате")
	ErrTopicFull     = errors.New("в теме слишком много слов")
)

// TopicList возвращает значение Settings.List для темы.
func TopicList(name string) string { return TopicPrefix + name }

// TopicName выделяет название темы из значения Settings.List.
func TopicName(list string) (string, bool) {
	return strings.CutPrefix(list, TopicPrefix)
}

type chatState struct {
	Settings *Settings           `json:"settings,omitempty"`
	Topics   map[string][]string `json:"topics,omitempty"`
}

// Store хранит настройки чатов и их темы в json-файле. Все изменения сразу сохраняются на диск.
type Store struct {
	mu    sync.Mutex
	path  string
	chats map[int64]*chatState
}

type storeFile struct {
	Chats map[int64]*chatState `json:"chats"`
}

// OpenStore читает состояние из файла; если файла нет, начинает с пустого.
// Повреждённый файл не перезаписывается, возвращается ошибка.
func OpenStore(path string) (*Store, error) {
	s := &Store{path: path, chats: make(map[int64]*chatState)}
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
	data, err := json.MarshalIndent(storeFile{Chats: s.chats}, "", "  ")
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
func (c *chatState) findTopic(name string) (string, bool) {
	for key := range c.Topics {
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
		if key, found := c.findTopic(name); found {
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

// TopicNames возвращает названия тем чата по алфавиту.
func (s *Store) TopicNames(chatID int64) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.topicNames(chatID)
}

func (s *Store) topicNames(chatID int64) []string {
	c := s.chats[chatID]
	if c == nil {
		return nil
	}
	names := make([]string, 0, len(c.Topics))
	for name := range c.Topics {
		names = append(names, name)
	}
	slices.SortFunc(names, func(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) })
	return names
}

// TopicWords возвращает копию слов темы.
func (s *Store) TopicWords(chatID int64, name string) ([]string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.chats[chatID]
	if c == nil {
		return nil, false
	}
	key, ok := c.findTopic(name)
	if !ok {
		return nil, false
	}
	return slices.Clone(c.Topics[key]), true
}

func validTopicName(name string) bool {
	n := utf8.RuneCountInString(name)
	return n > 0 && n <= MaxTopicNameLen && !strings.ContainsAny(name, topicNameBadChar) && name == strings.TrimSpace(name)
}

// AddTopicWords добавляет слова в тему, создавая её при необходимости. Возвращает число новых слов.
func (s *Store) AddTopicWords(chatID int64, name string, words []string) (int, error) {
	name = strings.TrimSpace(name)
	if !validTopicName(name) {
		return 0, ErrBadTopicName
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	c := s.chat(chatID)
	key, exists := c.findTopic(name)
	if !exists {
		if len(c.Topics) >= MaxTopics {
			return 0, ErrTooManyTopics
		}
		key = name
	}
	have := slices.Clone(c.Topics[key])
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
	if c.Topics == nil {
		c.Topics = make(map[string][]string)
	}
	c.Topics[key] = have
	return added, s.save()
}

// RemoveTopicWords удаляет слова из темы. Возвращает число удалённых слов.
func (s *Store) RemoveTopicWords(chatID int64, name string, words []string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.chats[chatID]
	if c == nil {
		return 0, ErrNoTopic
	}
	key, ok := c.findTopic(name)
	if !ok {
		return 0, ErrNoTopic
	}
	before := len(c.Topics[key])
	c.Topics[key] = slices.DeleteFunc(slices.Clone(c.Topics[key]), func(w string) bool { return slices.Contains(words, w) })
	return before - len(c.Topics[key]), s.save()
}

func (s *Store) DeleteTopic(chatID int64, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.chats[chatID]
	if c == nil {
		return ErrNoTopic
	}
	key, ok := c.findTopic(name)
	if !ok {
		return ErrNoTopic
	}
	delete(c.Topics, key)
	return s.save()
}
