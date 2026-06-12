package adapters

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"telegram-bot/internal/entities"
)

const (
	nexN2Pro            = "nex-agi/Nex-N2-Pro"
	maxAnalysisAttempts = 3
)

var ErrInvalidModelJSON = errors.New("invalid model json")

type SystemPromptSource interface {
	Text() (string, error)
}

type SiliconFlowAdapter struct {
	apiKey       string
	systemPrompt SystemPromptSource
	client       *http.Client
}

func NewSiliconFlowAdapter(apiKey string, systemPrompt SystemPromptSource) *SiliconFlowAdapter {
	return &SiliconFlowAdapter{
		apiKey:       apiKey,
		systemPrompt: systemPrompt,
		client:       &http.Client{Timeout: 5 * time.Minute},
	}
}

func (s *SiliconFlowAdapter) AnalyzeImage(ctx context.Context, data []byte, mime string) (entities.ImageAnalysis, error) {
	if mime == "" {
		mime = "image/jpeg"
	}

	systemPrompt, err := s.systemPrompt.Text()
	if err != nil {
		return entities.ImageAnalysis{}, fmt.Errorf("загрузить system prompt: %w", err)
	}

	reqBody, err := json.Marshal(map[string]any{
		"model": nexN2Pro,
		"messages": []map[string]any{
			{
				"role":    "system",
				"content": systemPrompt,
			},
			{
				"role": "user",
				"content": []map[string]any{
					{
						"type": "image_url",
						"image_url": map[string]string{
							"url": fmt.Sprintf("data:%s;base64,%s", mime, base64.StdEncoding.EncodeToString(data)),
						},
					},
				},
			},
		},
		"response_format": map[string]string{"type": "json_object"},
		"max_tokens":      2048,
	})
	if err != nil {
		return entities.ImageAnalysis{}, err
	}

	for attempt := 1; attempt <= maxAnalysisAttempts; attempt++ {
		content, err := s.complete(ctx, reqBody)
		if err != nil {
			return entities.ImageAnalysis{}, err
		}

		analysis, err := parseImageAnalysis(content)
		if err == nil {
			return analysis, nil
		}
		if !errors.Is(err, ErrInvalidModelJSON) || attempt == maxAnalysisAttempts {
			return entities.ImageAnalysis{}, err
		}
	}

	return entities.ImageAnalysis{}, ErrInvalidModelJSON
}

func (s *SiliconFlowAdapter) complete(ctx context.Context, reqBody []byte) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.siliconflow.com/v1/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("запрос к LLM: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("LLM: %s", body)
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("разобрать ответ LLM: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("пустой ответ LLM")
	}

	return parsed.Choices[0].Message.Content, nil
}

func parseImageAnalysis(content string) (entities.ImageAnalysis, error) {
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var analysis entities.ImageAnalysis
	if err := json.Unmarshal([]byte(content), &analysis); err != nil {
		return entities.ImageAnalysis{}, fmt.Errorf("%w: %w", ErrInvalidModelJSON, err)
	}
	return analysis, nil
}
