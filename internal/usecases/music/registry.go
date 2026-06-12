package music

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
)

type userFileRegistry struct {
	path string
	mu   sync.Mutex
	data map[string][]string
}

func newUserFileRegistry(dir, filename string) *userFileRegistry {
	return &userFileRegistry{
		path: filepath.Join(dir, filename),
		data: make(map[string][]string),
	}
}

func (r *userFileRegistry) load() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	raw, err := os.ReadFile(r.path)
	if err != nil {
		if os.IsNotExist(err) {
			r.data = make(map[string][]string)
			return nil
		}
		return err
	}

	data := make(map[string][]string)
	if err := json.Unmarshal(raw, &data); err != nil {
		return err
	}
	r.data = data
	return nil
}

func (r *userFileRegistry) saveLocked() error {
	raw, err := json.MarshalIndent(r.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, r.path)
}

func (r *userFileRegistry) add(userID int64, name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := userKey(userID)
	files := r.data[key]
	for _, existing := range files {
		if existing == name {
			return r.saveLocked()
		}
	}
	r.data[key] = append(files, name)
	return r.saveLocked()
}

func (r *userFileRegistry) remove(userID int64, name string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := userKey(userID)
	files := r.data[key]
	for i, existing := range files {
		if existing != name {
			continue
		}
		r.data[key] = append(files[:i], files[i+1:]...)
		if len(r.data[key]) == 0 {
			delete(r.data, key)
		}
		return true, r.saveLocked()
	}
	return false, nil
}

func (r *userFileRegistry) list(userID int64) []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	files := append([]string(nil), r.data[userKey(userID)]...)
	slices.Sort(files)
	return files
}

func (r *userFileRegistry) owns(userID int64, name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, existing := range r.data[userKey(userID)] {
		if existing == name {
			return true
		}
	}
	return false
}

func userKey(userID int64) string {
	return strconv.FormatInt(userID, 10)
}

func safeUploadBasename(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return "", false
	}
	if strings.ContainsAny(name, `/\`) {
		return "", false
	}
	return filepath.Base(name), true
}
