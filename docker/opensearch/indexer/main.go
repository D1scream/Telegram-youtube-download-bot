package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()

	ukPath := flag.String("uk", "ugolovnyykodeks04112019.txt", "путь к тексту УК РФ")
	configDir := flag.String("config-dir", "docker/opensearch", "директория с mapping.json и pipeline.json")
	opensearchURL := flag.String("opensearch-url", envOr("OPENSEARCH_URL", "http://localhost:9200"), "URL OpenSearch")
	embeddingsURL := flag.String("embeddings-url", envOr("EMBEDDINGS_URL", "http://localhost:8080"), "URL TEI")
	flag.Parse()

	ukFile, err := resolvePath(*ukPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	mappingPath := filepath.Join(*configDir, "mapping.json")
	pipelinePath := filepath.Join(*configDir, "pipeline.json")

	fmt.Printf("UK file: %s\n", ukFile)
	articles, err := parseFile(ukFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("Articles parsed: %d\n", len(articles))

	idx := newIndexer(indexConfig{
		OpenSearchURL: *opensearchURL,
		EmbeddingsURL: *embeddingsURL,
		MappingPath:   mappingPath,
		PipelinePath:  pipelinePath,
	})

	if err := idx.run(context.Background(), articles); err != nil {
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintln(os.Stderr, "Подсказка: docker compose -f docker/opensearch/docker-compose.yml up -d embeddings")
		os.Exit(1)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func resolvePath(path string) (string, error) {
	if filepath.IsAbs(path) {
		return path, nil
	}
	if cwd, err := os.Getwd(); err == nil {
		candidate := filepath.Join(cwd, path)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("файл не найден: %s", path)
	}
	return path, nil
}
