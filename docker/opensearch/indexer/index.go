package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	defaultIndexName    = "uk_rf"
	defaultPipelineName = "uk_rf-hybrid"
	defaultBatchSize    = 20
)

type indexConfig struct {
	OpenSearchURL string
	EmbeddingsURL string
	IndexName     string
	PipelineName  string
	MappingPath   string
	PipelinePath  string
	BatchSize     int
}

type indexer struct {
	cfg      indexConfig
	tei      *teiClient
	client   *http.Client
}

func newIndexer(cfg indexConfig) *indexer {
	if cfg.IndexName == "" {
		cfg.IndexName = defaultIndexName
	}
	if cfg.PipelineName == "" {
		cfg.PipelineName = defaultPipelineName
	}
	if cfg.BatchSize < 1 {
		cfg.BatchSize = defaultBatchSize
	}
	return &indexer{
		cfg:    cfg,
		tei:    newTEI(cfg.EmbeddingsURL),
		client: &http.Client{Timeout: 60 * time.Second},
	}
}

func (i *indexer) run(ctx context.Context, articles []article) error {
	if err := i.tei.checkHealth(ctx); err != nil {
		return fmt.Errorf("TEI недоступен: %w", err)
	}

	mapping, err := os.ReadFile(i.cfg.MappingPath)
	if err != nil {
		return fmt.Errorf("прочитать mapping: %w", err)
	}
	pipeline, err := os.ReadFile(i.cfg.PipelinePath)
	if err != nil {
		return fmt.Errorf("прочитать pipeline: %w", err)
	}

	base := strings.TrimRight(i.cfg.OpenSearchURL, "/")
	_ = i.request(ctx, http.MethodDelete, base+"/"+i.cfg.IndexName, nil)

	if err := i.request(ctx, http.MethodPut, base+"/"+i.cfg.IndexName, mapping); err != nil {
		return fmt.Errorf("создать индекс: %w", err)
	}
	if err := i.request(ctx, http.MethodPut, base+"/_search/pipeline/"+i.cfg.PipelineName, pipeline); err != nil {
		return fmt.Errorf("создать pipeline: %w", err)
	}

	for start := 0; start < len(articles); start += i.cfg.BatchSize {
		end := start + i.cfg.BatchSize
		if end > len(articles) {
			end = len(articles)
		}
		batch := articles[start:end]

		inputs := make([]string, len(batch))
		for j, a := range batch {
			inputs[j] = a.Content
		}
		vectors, err := i.tei.embedBatch(ctx, inputs)
		if err != nil {
			return fmt.Errorf("эмбеддинги batch %d-%d: %w", start, end, err)
		}

		var bulk bytes.Buffer
		for j, a := range batch {
			meta, _ := json.Marshal(map[string]any{
				"index": map[string]any{
					"_index": i.cfg.IndexName,
					"_id":    a.Number,
				},
			})
			doc, _ := json.Marshal(map[string]any{
				"article":   a.Number,
				"content":   a.Content,
				"embedding": vectors[j],
			})
			bulk.Write(meta)
			bulk.WriteByte('\n')
			bulk.Write(doc)
			bulk.WriteByte('\n')
		}

		if err := i.bulk(ctx, base+"/_bulk", bulk.Bytes()); err != nil {
			return fmt.Errorf("bulk batch %d-%d: %w", start, end, err)
		}
		fmt.Printf("Indexed %d/%d\n", end, len(articles))
	}

	if err := i.request(ctx, http.MethodPost, base+"/"+i.cfg.IndexName+"/_refresh", nil); err != nil {
		return fmt.Errorf("refresh: %w", err)
	}

	count, err := i.documentCount(ctx, base+"/"+i.cfg.IndexName+"/_count")
	if err != nil {
		return err
	}
	fmt.Printf("Done. Documents in index: %d\n", count)
	return nil
}

func (i *indexer) request(ctx context.Context, method, url string, body []byte) error {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
	}

	resp, err := i.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s: %s", method, url, respBody)
	}
	return nil
}

func (i *indexer) bulk(ctx context.Context, url string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-ndjson; charset=utf-8")

	resp, err := i.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("bulk: %s", respBody)
	}

	var parsed struct {
		Errors bool `json:"errors"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return fmt.Errorf("разобрать bulk ответ: %w", err)
	}
	if parsed.Errors {
		return fmt.Errorf("bulk errors: %s", respBody)
	}
	return nil
}

func (i *indexer) documentCount(ctx context.Context, url string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}

	resp, err := i.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}
	if resp.StatusCode >= 300 {
		return 0, fmt.Errorf("_count: %s", respBody)
	}

	var parsed struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return 0, fmt.Errorf("разобрать _count: %w", err)
	}
	return parsed.Count, nil
}
