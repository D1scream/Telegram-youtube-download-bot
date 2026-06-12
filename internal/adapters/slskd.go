package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"telegram-bot/internal/entities"
)

const (
	slskdPollInterval      = 500 * time.Millisecond
	slskdHTTPTimeout       = 60 * time.Second
	slskdMinUploadSpeedBps = 512 * 1024 * 8 // 512 KB/s, slskd API — bit/s
)

type SlskdAdapter struct {
	baseURL       string
	apiKey        string
	webhookURL    string
	webhookSecret string
	client        *http.Client
}

func NewSlskdAdapter(baseURL, apiKey, webhookURL, webhookSecret string) *SlskdAdapter {
	return &SlskdAdapter{
		baseURL:       strings.TrimRight(baseURL, "/"),
		apiKey:        strings.TrimSpace(apiKey),
		webhookURL:    strings.TrimSpace(webhookURL),
		webhookSecret: strings.TrimSpace(webhookSecret),
		client:        &http.Client{Timeout: slskdHTTPTimeout},
	}
}

type DownloadTransferStatus struct {
	State     string
	Exception string
}

func (s *SlskdAdapter) GetDownloadStatus(ctx context.Context, track entities.Track) (DownloadTransferStatus, bool, error) {
	path := fmt.Sprintf("/api/v0/transfers/downloads/%s", url.PathEscape(track.Username))
	var response slskdUserDownloads
	if err := s.doJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return DownloadTransferStatus{}, false, fmt.Errorf("slskd get download status: %w", err)
	}

	wantPath := normalizeSlskdPath(track.Filename)
	for _, dir := range response.Directories {
		for _, file := range dir.Files {
			if file.Size != track.Size {
				continue
			}
			if normalizeSlskdPath(file.Filename) != wantPath {
				continue
			}
			return DownloadTransferStatus{
				State:     file.State,
				Exception: file.Exception,
			}, true, nil
		}
	}
	return DownloadTransferStatus{}, false, nil
}

func (s *SlskdAdapter) EnqueueDownload(ctx context.Context, track entities.Track) error {
	body, err := json.Marshal([]map[string]any{
		{
			"filename": track.Filename,
			"size":     track.Size,
		},
	})
	if err != nil {
		return err
	}

	path := fmt.Sprintf("/api/v0/transfers/downloads/%s", url.PathEscape(track.Username))
	if err := s.doJSON(ctx, http.MethodPost, path, body, nil, http.StatusCreated); err != nil {
		return fmt.Errorf("slskd enqueue download: %w", err)
	}
	return nil
}

func (s *SlskdAdapter) Search(ctx context.Context, query string, fileLimit int) ([]entities.Track, error) {
	if fileLimit < 1 {
		fileLimit = 1
	}

	searchID, err := s.startSearch(ctx, query, fileLimit)
	if err != nil {
		return nil, err
	}

	return s.waitForResults(ctx, searchID)
}

func (s *SlskdAdapter) startSearch(ctx context.Context, query string, fileLimit int) (string, error) {
	body, err := json.Marshal(map[string]any{
		"searchText":             query,
		"fileLimit":              fileLimit,
		"minimumPeerUploadSpeed": slskdMinUploadSpeedBps,
		"filterResponses":        true,
	})
	if err != nil {
		return "", err
	}

	var started slskdSearch
	if err := s.doJSON(ctx, http.MethodPost, "/api/v0/searches", body, &started); err != nil {
		return "", fmt.Errorf("slskd start search: %w", err)
	}
	if started.ID == "" {
		return "", fmt.Errorf("slskd: пустой id поиска")
	}
	return started.ID, nil
}

func (s *SlskdAdapter) waitForResults(ctx context.Context, searchID string) ([]entities.Track, error) {
	ticker := time.NewTicker(slskdPollInterval)
	defer ticker.Stop()

	searchPath := fmt.Sprintf("/api/v0/searches/%s?includeResponses=true", url.PathEscape(searchID))

	for {
		var result slskdSearch
		if err := s.doJSON(ctx, http.MethodGet, searchPath, nil, &result); err != nil {
			return nil, fmt.Errorf("slskd get search: %w", err)
		}
		if result.IsComplete {
			return tracksFromSearch(result), nil
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func tracksFromSearch(result slskdSearch) []entities.Track {
	tracks := make([]entities.Track, 0, result.FileCount)
	for _, response := range result.Responses {
		for _, file := range response.Files {
			tracks = append(tracks, entities.Track{
				Filename:          file.Filename,
				Size:              file.Size,
				Extension:         file.Extension,
				Length:            file.Length,
				BitRate:           file.BitRate,
				BitDepth:          file.BitDepth,
				SampleRate:        file.SampleRate,
				Username:          response.Username,
				QueueLength:       response.QueueLength,
				UploadSpeed:       response.UploadSpeed,
				HasFreeUploadSlot: response.HasFreeUploadSlot,
			})
		}
	}
	return tracks
}

func (s *SlskdAdapter) doJSON(ctx context.Context, method, path string, body []byte, out any, expectedStatus ...int) error {
	if len(expectedStatus) == 0 {
		expectedStatus = []int{http.StatusOK}
	}
	var reader io.Reader
	if len(body) > 0 {
		reader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, s.baseURL+path, reader)
	if err != nil {
		return err
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	if s.apiKey != "" {
		req.Header.Set("X-API-Key", s.apiKey)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if !statusAllowed(resp.StatusCode, expectedStatus) {
		return fmt.Errorf("status %s: %s", resp.Status, respBody)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("разобрать ответ slskd: %w", err)
	}
	return nil
}

func statusAllowed(code int, expected []int) bool {
	for _, want := range expected {
		if code == want {
			return true
		}
	}
	return false
}

type slskdSearch struct {
	ID         string          `json:"id"`
	IsComplete bool            `json:"isComplete"`
	FileCount  int             `json:"fileCount"`
	Responses  []slskdResponse `json:"responses"`
}

type slskdResponse struct {
	Username          string      `json:"username"`
	QueueLength       int         `json:"queueLength"`
	UploadSpeed       int         `json:"uploadSpeed"`
	HasFreeUploadSlot bool        `json:"hasFreeUploadSlot"`
	Files             []slskdFile `json:"files"`
}

type slskdFile struct {
	Filename   string `json:"filename"`
	Size       int64  `json:"size"`
	Extension  string `json:"extension"`
	Length     int    `json:"length"`
	BitRate    int    `json:"bitRate"`
	BitDepth   int    `json:"bitDepth"`
	SampleRate int    `json:"sampleRate"`
}

type slskdUserDownloads struct {
	Username    string             `json:"username"`
	Directories []slskdDownloadDir `json:"directories"`
}

type slskdDownloadDir struct {
	Files []slskdDownloadFile `json:"files"`
}

type slskdDownloadFile struct {
	Filename  string `json:"filename"`
	Size      int64  `json:"size"`
	State     string `json:"state"`
	Exception string `json:"exception"`
}

type slskdOptions struct {
	Transfers struct {
		Groups struct {
			Blacklisted struct {
				Members []string `json:"members"`
			} `json:"blacklisted"`
		} `json:"groups"`
	} `json:"transfers"`
	Integrations struct {
		Webhooks struct {
			TelegramBot map[string]any `json:"telegram_bot"`
		} `json:"webhooks"`
	} `json:"integrations"`
}

func normalizeSlskdPath(path string) string {
	return strings.ToLower(strings.ReplaceAll(path, "\\", "/"))
}
