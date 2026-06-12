package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "uk.txt")
	content := "Преамбула\n\n" +
		"Статья 158. Кража\n" +
		"1. Кража, то есть тайное хищение.\n" +
		"\n" +
		"Статья 159. Мошенничество\n" +
		"1. Мошенничество, то есть хищение.\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	articles, err := parseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(articles) != 2 {
		t.Fatalf("expected 2 articles, got %d", len(articles))
	}
	if articles[0].Number != "158" {
		t.Fatalf("unexpected number: %s", articles[0].Number)
	}
	if articles[0].Content != "Кража\n1. Кража, то есть тайное хищение." {
		t.Fatalf("unexpected content: %q", articles[0].Content)
	}
}
