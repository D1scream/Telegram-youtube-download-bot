package prompt

import (
	"fmt"
	"os"
	"strings"
	"sync"
)

type File struct {
	path string

	mu      sync.Mutex
	content string
	modTime int64
}

func NewFile(path string) (*File, error) {
	f := &File{path: path}
	if _, err := f.Text(); err != nil {
		return nil, err
	}
	return f, nil
}

func (f *File) Text() (string, error) {
	info, err := os.Stat(f.path)
	if err != nil {
		return "", fmt.Errorf("prompt файл %q: %w", f.path, err)
	}
	modTime := info.ModTime().UnixNano()

	f.mu.Lock()
	defer f.mu.Unlock()

	if modTime == f.modTime {
		return f.content, nil
	}

	data, err := os.ReadFile(f.path)
	if err != nil {
		return "", fmt.Errorf("читать prompt файл %q: %w", f.path, err)
	}

	content := strings.TrimSpace(string(data))
	if content == "" {
		return "", fmt.Errorf("prompt файл %q пуст", f.path)
	}

	f.content = content
	f.modTime = modTime
	return f.content, nil
}
