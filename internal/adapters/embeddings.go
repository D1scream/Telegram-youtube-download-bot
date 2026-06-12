package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
	"unicode/utf8"
)

const embedMaxLen = 1500

type EmbeddingsAdapter struct {
	baseURL string
	client  *http.Client
}

func NewEmbeddingsAdapter(baseURL string) *EmbeddingsAdapter {
	return &EmbeddingsAdapter{
		baseURL: baseURL,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (e *EmbeddingsAdapter) Embed(ctx context.Context, text string) ([]float32, error) {
	text = truncateRunes(text, embedMaxLen)

	body, err := json.Marshal(map[string]any{"inputs": []string{text}})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/embed", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("TEI embed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("TEI: %s", respBody)
	}

	var vectors [][]float32
	if err := json.Unmarshal(respBody, &vectors); err != nil {
		return nil, fmt.Errorf("разобрать ответ TEI: %w", err)
	}
	if len(vectors) == 0 {
		return nil, fmt.Errorf("TEI: пустой ответ")
	}
	return vectors[0], nil
}

func truncateRunes(text string, maxLen int) string {
	if utf8.RuneCountInString(text) <= maxLen {
		return text
	}
	runes := []rune(text)
	return string(runes[:maxLen])
}
