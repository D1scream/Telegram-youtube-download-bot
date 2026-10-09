// Package msglog хранит лог сообщений чатов в виде jsonl-сегментов с ограничением общего размера.
package msglog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	segmentPrefix = "messages-"
	segmentSuffix = ".jsonl"
	deletedFile   = "deleted.jsonl"
	segmentsInCap = 16
)

type Entry struct {
	Time      time.Time `json:"time"`
	ChatID    int64     `json:"chat_id"`
	Chat      string    `json:"chat,omitempty"`
	MessageID int       `json:"message_id"`
	FromID    int64     `json:"from_id,omitempty"`
	From      string    `json:"from,omitempty"`
	Name      string    `json:"name,omitempty"`
	Kind      string    `json:"kind,omitempty"`
	Text      string    `json:"text,omitempty"`
	Deleted   bool      `json:"-"`
}

type Writer struct {
	mu       sync.Mutex
	dir      string
	maxBytes int64
	segBytes int64
	file     *os.File
	size     int64
}

func Open(dir string, maxBytes int64) (*Writer, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("размер лога сообщений должен быть больше нуля")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("создать папку лога сообщений: %w", err)
	}
	w := &Writer{dir: dir, maxBytes: maxBytes, segBytes: max(maxBytes/segmentsInCap, 1)}
	segments, err := listSegments(dir)
	if err != nil {
		return nil, err
	}
	if len(segments) > 0 {
		last := segments[len(segments)-1]
		if info, err := os.Stat(last); err == nil && info.Size() < w.segBytes {
			if err := w.openSegment(last); err != nil {
				return nil, err
			}
			return w, nil
		}
	}
	if err := w.rotate(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *Writer) Append(e Entry) error {
	line, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("сериализовать запись лога: %w", err)
	}
	line = append(line, '\n')

	w.mu.Lock()
	defer w.mu.Unlock()
	if w.size > 0 && w.size+int64(len(line)) > w.segBytes {
		if err := w.rotate(); err != nil {
			return err
		}
	}
	n, err := w.file.Write(line)
	w.size += int64(n)
	if err != nil {
		return fmt.Errorf("записать лог сообщений: %w", err)
	}
	return nil
}

func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	return w.file.Close()
}

func (w *Writer) openSegment(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("открыть сегмент лога: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return fmt.Errorf("открыть сегмент лога: %w", err)
	}
	if w.file != nil {
		w.file.Close()
	}
	w.file = f
	w.size = info.Size()
	return nil
}

func (w *Writer) rotate() error {
	name := segmentPrefix + time.Now().UTC().Format("20060102T150405.000000000") + segmentSuffix
	if err := w.openSegment(filepath.Join(w.dir, name)); err != nil {
		return err
	}
	return w.prune()
}

func (w *Writer) prune() error {
	segments, err := listSegments(w.dir)
	if err != nil {
		return err
	}
	sizes := make([]int64, len(segments))
	var total int64
	for i, path := range segments {
		if info, err := os.Stat(path); err == nil {
			sizes[i] = info.Size()
			total += sizes[i]
		}
	}
	current := w.file.Name()
	for i, path := range segments {
		if total <= w.maxBytes || path == current {
			break
		}
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("удалить старый сегмент лога: %w", err)
		}
		total -= sizes[i]
	}
	return nil
}

func listSegments(dir string) ([]string, error) {
	items, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("прочитать папку лога сообщений: %w", err)
	}
	var segments []string
	for _, item := range items {
		name := item.Name()
		if !item.IsDir() && strings.HasPrefix(name, segmentPrefix) && strings.HasSuffix(name, segmentSuffix) {
			segments = append(segments, filepath.Join(dir, name))
		}
	}
	slices.Sort(segments)
	return segments, nil
}
