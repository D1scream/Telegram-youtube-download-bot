package main

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

type teiClient struct {
	baseURL string
	client  *http.Client
}

func newTEI(baseURL string) *teiClient {
	return &teiClient{
		baseURL: baseURL,
		client:  &http.Client{Timeout: 60 * time.Second},
	}
}

func (t *teiClient) checkHealth(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.baseURL+"/health", nil)
	if err != nil {
		return err
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("TEI health: %s", body)
	}
	return nil
}

func (t *teiClient) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	inputs := make([]string, len(texts))
	for i, text := range texts {
		inputs[i] = truncateRunes(text, embedMaxLen)
	}

	body, err := json.Marshal(map[string]any{"inputs": inputs})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.baseURL+"/embed", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
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
	if len(vectors) != len(texts) {
		return nil, fmt.Errorf("TEI: expected %d vectors, got %d", len(texts), len(vectors))
	}
	return vectors, nil
}

func truncateRunes(text string, maxLen int) string {
	if utf8.RuneCountInString(text) <= maxLen {
		return text
	}
	runes := []rune(text)
	return string(runes[:maxLen])
}
