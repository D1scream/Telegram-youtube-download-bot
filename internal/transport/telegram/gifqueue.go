package telegram

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// GIFQueue выдаёт гифки из каталога по очереди, у каждого чата своя позиция,
// и помнит последний BPM чата (для /agif). Каталог читается при каждом
// запросе (по имени файла), поэтому новые гифки подхватываются без
// перезапуска. Состояние сохраняется в json-файл.
type GIFQueue struct {
	mu    sync.Mutex
	dir   string
	path  string
	chats map[int64]*gifChat
}

type gifChat struct {
	Next int     `json:"next"`          // номер следующей гифки
	BPM  float64 `json:"bpm,omitempty"` // последний BPM
}

func OpenGIFQueue(dir, path string) (*GIFQueue, error) {
	q := &GIFQueue{dir: dir, path: path, chats: make(map[int64]*gifChat)}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return q, nil
	}
	if err != nil {
		return nil, fmt.Errorf("прочитать очередь гифок: %w", err)
	}
	if err := json.Unmarshal(data, &q.chats); err == nil {
		return q, nil
	}
	// прежний формат: только позиция очереди без BPM ({"чат": номер})
	var old map[int64]int
	if err := json.Unmarshal(data, &old); err != nil {
		return nil, fmt.Errorf("разобрать очередь гифок %s: %w", path, err)
	}
	q.chats = make(map[int64]*gifChat, len(old))
	for id, next := range old {
		q.chats[id] = &gifChat{Next: next}
	}
	return q, nil
}

// list возвращает файлы гифок каталога по имени.
func (q *GIFQueue) list() ([]string, error) {
	entries, err := os.ReadDir(q.dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("прочитать каталог гифок: %w", err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".gif") {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("нет гифок в %s", q.dir)
	}
	slices.Sort(names)
	return names, nil
}

// next возвращает путь к гифке для чата и запоминает bpm как последний BPM
// чата. Пустой name — очередная гифка, очередь сдвигается; иначе — гифка по
// имени (gifTitle, точное или по началу), очередь не трогается.
func (q *GIFQueue) next(chatID int64, bpm float64, name string) (string, error) {
	names, err := q.list()
	if err != nil {
		return "", err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	c := q.chat(chatID)
	var file string
	if name == "" {
		i := c.Next % len(names)
		c.Next, file = i+1, names[i]
	} else if file, err = findGIF(names, name); err != nil {
		return "", err
	}
	c.BPM = bpm
	if err := q.save(); err != nil {
		return "", err
	}
	return filepath.Join(q.dir, file), nil
}

// findGIF ищет гифку по короткому имени: сначала точное совпадение, потом
// единственное, начинающееся с name.
func findGIF(names []string, name string) (string, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	var prefix []string
	for _, n := range names {
		t := strings.ToLower(gifTitle(n))
		if t == name {
			return n, nil
		}
		if strings.HasPrefix(t, name) {
			prefix = append(prefix, n)
		}
	}
	switch len(prefix) {
	case 1:
		return prefix[0], nil
	case 0:
		return "", fmt.Errorf("нет гифки «%s». Список: /agif_list", name)
	default:
		titles := make([]string, len(prefix))
		for i, n := range prefix {
			titles[i] = gifTitle(n)
		}
		return "", fmt.Errorf("под «%s» подходят: %s", name, strings.Join(titles, ", "))
	}
}

// gifTitle — короткое имя гифки для списка и выбора: без расширения и метки
// темпа («skeleton-base-110.gif» -> «skeleton»).
func gifTitle(file string) string {
	stem := strings.TrimSuffix(file, filepath.Ext(file))
	if loc := baseBPMName.FindStringIndex(stem); loc != nil {
		stem = stem[:loc[0]] + stem[loc[1]:]
	}
	return stem
}

// lastBPM — BPM последней гифки чата, 0 если её ещё не было.
func (q *GIFQueue) lastBPM(chatID int64) float64 {
	q.mu.Lock()
	defer q.mu.Unlock()
	if c := q.chats[chatID]; c != nil {
		return c.BPM
	}
	return 0
}

func (q *GIFQueue) chat(chatID int64) *gifChat {
	c := q.chats[chatID]
	if c == nil {
		c = &gifChat{}
		q.chats[chatID] = c
	}
	return c
}

func (q *GIFQueue) save() error {
	data, err := json.Marshal(q.chats)
	if err != nil {
		return fmt.Errorf("сериализовать очередь гифок: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(q.path), 0o755); err != nil {
		return fmt.Errorf("создать каталог очереди гифок: %w", err)
	}
	tmp := q.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("записать очередь гифок: %w", err)
	}
	if err := os.Rename(tmp, q.path); err != nil {
		return fmt.Errorf("сохранить очередь гифок: %w", err)
	}
	return nil
}
