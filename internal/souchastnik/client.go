package souchastnik

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type Config struct {
	URL            string
	TimeoutSeconds int
}

type Result struct {
	Code string `json:"code"`
}

type Client struct {
	url    string
	client *http.Client
}

func New(cfg Config) *Client {
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		url:    strings.TrimRight(cfg.URL, "/") + "/verdict",
		client: &http.Client{Timeout: timeout},
	}
}

func (c *Client) Check(ctx context.Context, text string) (Result, error) {
	body, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return Result{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("запрос к souchastnik: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("souchastnik вернул HTTP %d", resp.StatusCode)
	}

	var result Result
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return Result{}, fmt.Errorf("разобрать ответ souchastnik: %w", err)
	}
	if strings.TrimSpace(result.Code) == "" {
		return Result{}, fmt.Errorf("souchastnik вернул пустой код статьи")
	}
	return result, nil
}