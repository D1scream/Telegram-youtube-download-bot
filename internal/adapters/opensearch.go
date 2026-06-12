package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"telegram-bot/internal/entities"
)

type OpenSearchAdapter struct {
	baseURL        string
	index          string
	embedder       *EmbeddingsAdapter
	knnK           int
	searchPipeline string
	client         *http.Client
}

func NewOpenSearchAdapter(baseURL, index string, embedder *EmbeddingsAdapter, knnK int, searchPipeline string) *OpenSearchAdapter {
	if knnK < 1 {
		knnK = 20
	}
	return &OpenSearchAdapter{
		baseURL:        strings.TrimRight(baseURL, "/"),
		index:          index,
		embedder:       embedder,
		knnK:           knnK,
		searchPipeline: strings.TrimSpace(searchPipeline),
		client:         &http.Client{Timeout: 15 * time.Second},
	}
}

func (o *OpenSearchAdapter) Search(ctx context.Context, query string, limit int) ([]entities.ScoredArticle, error) {
	vec, err := o.embedder.Embed(ctx, query)
	if err != nil {
		return nil, err
	}

	k := o.knnK
	if k < limit {
		k = limit
	}

	payload := map[string]any{
		"size": limit * 5,
		"query": map[string]any{
			"hybrid": map[string]any{
				"queries": []any{
					map[string]any{
						"match": map[string]any{
							"content": query,
						},
					},
					map[string]any{
						"knn": map[string]any{
							"embedding": map[string]any{
								"vector": vec,
								"k":      k,
							},
						},
					},
				},
			},
		},
		"_source": []string{"article", "content"},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/%s/_search", o.baseURL, o.index)
	if o.searchPipeline != "" {
		url += "?search_pipeline=" + o.searchPipeline
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("opensearch search: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("opensearch: %s", respBody)
	}

	var parsed struct {
		Hits struct {
			Hits []struct {
				Score  float64 `json:"_score"`
				Source struct {
					Article string `json:"article"`
					Content string `json:"content"`
				} `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("разобрать ответ opensearch: %w", err)
	}

	best := make(map[string]entities.ScoredArticle, len(parsed.Hits.Hits))
	order := make([]string, 0, len(parsed.Hits.Hits))
	for _, hit := range parsed.Hits.Hits {
		id := hit.Source.Article
		article, ok := best[id]
		if !ok {
			order = append(order, id)
			article = entities.ScoredArticle{
				Article: articleFromContent(id, hit.Source.Content),
			}
		}
		if hit.Score > article.Score {
			article.Score = hit.Score
		}
		best[id] = article
	}

	result := make([]entities.ScoredArticle, 0, limit)
	for _, id := range order {
		result = append(result, best[id])
		if len(result) >= limit {
			break
		}
	}
	return result, nil
}

func articleFromContent(number, content string) entities.Article {
	title, text, _ := strings.Cut(content, "\n")
	return entities.Article{
		Number: number,
		Title:  title,
		Text:   text,
	}
}
