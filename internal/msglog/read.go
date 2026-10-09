package msglog

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const maxLineBytes = 1 << 20

type Filter struct {
	ChatID         int64
	From           string
	Contains       string
	IDs            []int
	Last           int
	IncludeDeleted bool
}

func (f Filter) match(e Entry) bool {
	if f.ChatID != 0 && e.ChatID != f.ChatID {
		return false
	}
	if f.From != "" && !matchFrom(e, f.From) {
		return false
	}
	if f.Contains != "" && !strings.Contains(strings.ToLower(e.Text), strings.ToLower(f.Contains)) {
		return false
	}
	if len(f.IDs) > 0 && !slices.Contains(f.IDs, e.MessageID) {
		return false
	}
	return f.IncludeDeleted || !e.Deleted
}

func matchFrom(e Entry, from string) bool {
	from = strings.TrimPrefix(from, "@")
	return fmt.Sprint(e.FromID) == from || strings.EqualFold(e.From, from) || strings.EqualFold(e.Name, from)
}

type messageKey struct {
	chatID int64
	id     int
}

type deletion struct {
	Time       time.Time `json:"time"`
	ChatID     int64     `json:"chat_id"`
	MessageIDs []int     `json:"message_ids"`
}

func Read(dir string, f Filter) ([]Entry, error) {
	deleted, err := readDeleted(dir)
	if err != nil {
		return nil, err
	}
	segments, err := listSegments(dir)
	if err != nil {
		return nil, err
	}
	var result []Entry
	for i := len(segments) - 1; i >= 0; i-- {
		found, err := readSegment(segments[i], f, deleted)
		if err != nil {
			return nil, err
		}
		result = append(found, result...)
		if f.Last > 0 && len(result) >= f.Last {
			return result[len(result)-f.Last:], nil
		}
	}
	return result, nil
}

func readSegment(path string, f Filter, deleted map[messageKey]bool) ([]Entry, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("открыть сегмент лога: %w", err)
	}
	defer file.Close()

	var found []Entry
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), maxLineBytes)
	for scanner.Scan() {
		var e Entry
		if json.Unmarshal(scanner.Bytes(), &e) != nil {
			continue
		}
		e.Deleted = deleted[messageKey{e.ChatID, e.MessageID}]
		if f.match(e) {
			found = append(found, e)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("прочитать сегмент лога %s: %w", filepath.Base(path), err)
	}
	return found, nil
}

func readDeleted(dir string) (map[messageKey]bool, error) {
	deleted := make(map[messageKey]bool)
	file, err := os.Open(filepath.Join(dir, deletedFile))
	if err != nil {
		if os.IsNotExist(err) {
			return deleted, nil
		}
		return nil, fmt.Errorf("открыть список удалённых: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), maxLineBytes)
	for scanner.Scan() {
		var d deletion
		if json.Unmarshal(scanner.Bytes(), &d) != nil {
			continue
		}
		for _, id := range d.MessageIDs {
			deleted[messageKey{d.ChatID, id}] = true
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("прочитать список удалённых: %w", err)
	}
	return deleted, nil
}

func MarkDeleted(dir string, chatID int64, ids []int) error {
	if len(ids) == 0 {
		return nil
	}
	line, err := json.Marshal(deletion{Time: time.Now().UTC(), ChatID: chatID, MessageIDs: ids})
	if err != nil {
		return fmt.Errorf("сериализовать список удалённых: %w", err)
	}
	file, err := os.OpenFile(filepath.Join(dir, deletedFile), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("открыть список удалённых: %w", err)
	}
	defer file.Close()
	if _, err := file.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("записать список удалённых: %w", err)
	}
	return nil
}
