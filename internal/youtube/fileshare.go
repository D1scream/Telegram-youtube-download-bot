package youtube

import (
	"crypto/rand"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

// Файлы больше лимита Telegram бот раздаёт сам по прямой ссылке и удаляет через fileshareRetention.
const (
	fileshareListenAddr = ":8090"
	fileshareRetention  = 72 * time.Hour
	fileshareCleanEvery = time.Hour
)

type fileshare struct {
	dir     string
	baseURL string
}

func startFileshare(dir, baseURL string, logger *slog.Logger) (*fileshare, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	f := &fileshare{dir: dir, baseURL: strings.TrimSuffix(baseURL, "/")}
	go func() {
		if err := http.ListenAndServe(fileshareListenAddr, f); err != nil {
			logger.Error("Раздача файлов остановилась", "err", err)
		}
	}()
	go func() {
		for {
			f.clean()
			time.Sleep(fileshareCleanEvery)
		}
	}()
	return f, nil
}

// publish переносит файл в папку раздачи и возвращает прямую ссылку на него.
// publish переносит файл в раздачу; возвращает ссылку и новый путь файла.
func (f *fileshare) publish(path string) (string, string, error) {
	token := rand.Text()
	if err := os.Mkdir(filepath.Join(f.dir, token), 0o755); err != nil {
		return "", "", err
	}
	name := linkName(filepath.Base(path))
	dst := filepath.Join(f.dir, token, name)
	if err := os.Rename(path, dst); err != nil {
		return "", "", err
	}
	return f.baseURL + "/" + token + "/" + name, dst, nil
}

// linkName делает имя файла читаемым в ссылке: пробелы -> _, остальные символы кроме букв, цифр, . и - убираются.
func linkName(name string) string {
	name = strings.Map(func(r rune) rune {
		switch {
		case r == ' ' || r == '_':
			return '_'
		case r == '.' || r == '-' || unicode.IsLetter(r) || unicode.IsDigit(r):
			return r
		}
		return -1
	}, name)
	for strings.Contains(name, "__") {
		name = strings.ReplaceAll(name, "__", "_")
	}
	return strings.ReplaceAll(name, "_.", ".")
}

func (f *fileshare) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(r.URL.Path, "/") {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": path.Base(r.URL.Path)}))
	w.Header().Set("Content-Type", "application/octet-stream")
	http.FileServer(http.Dir(f.dir)).ServeHTTP(w, r)
}

func (f *fileshare) clean() {
	items, _ := os.ReadDir(f.dir)
	for _, item := range items {
		if info, err := item.Info(); err == nil && time.Since(info.ModTime()) > fileshareRetention {
			os.RemoveAll(filepath.Join(f.dir, item.Name()))
		}
	}
}
