package youtube

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	litterboxURL            = "https://litterbox.catbox.moe/resources/internals/api.php"
	litterboxRetention      = "72h"
	litterboxMaxUploadBytes = 1024 * 1024 * 1024
	fileshareUploadTimeout  = 30 * time.Minute
)

type litterbox struct {
	url    string
	client *http.Client
}

func newLitterbox() *litterbox {
	return &litterbox{url: litterboxURL, client: &http.Client{}}
}

func (l *litterbox) upload(ctx context.Context, path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return "", err
	}

	var head bytes.Buffer
	form := multipart.NewWriter(&head)
	if err := form.WriteField("reqtype", "fileupload"); err != nil {
		return "", err
	}
	if err := form.WriteField("time", litterboxRetention); err != nil {
		return "", err
	}
	if _, err := form.CreateFormFile("fileToUpload", filepath.Base(path)); err != nil {
		return "", err
	}
	prefix := bytes.Clone(head.Bytes())
	head.Reset()
	if err := form.Close(); err != nil {
		return "", err
	}
	suffix := head.Bytes()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, l.url, io.MultiReader(
		bytes.NewReader(prefix),
		file,
		bytes.NewReader(suffix),
	))
	if err != nil {
		return "", err
	}
	req.ContentLength = int64(len(prefix)) + info.Size() + int64(len(suffix))
	req.Header.Set("Content-Type", form.FormDataContentType())

	resp, err := l.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("litterbox: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return "", fmt.Errorf("litterbox: прочитать ответ: %w", err)
	}
	text := strings.TrimSpace(string(body))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("litterbox: статус %d: %s", resp.StatusCode, text)
	}
	if !strings.HasPrefix(text, "https://") {
		return "", fmt.Errorf("litterbox: неожиданный ответ: %s", text)
	}
	return text, nil
}
