package main

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
)

var articleHeader = regexp.MustCompile(`^Статья ([\d.]+)\.\s*(.+)$`)

type article struct {
	Number  string
	Content string
}

func parseFile(path string) ([]article, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("открыть файл: %w", err)
	}
	defer f.Close()

	var articles []article
	var number, title string
	var body []string

	flush := func() {
		text := strings.TrimSpace(strings.Join(body, "\n"))
		if number == "" || text == "" {
			return
		}
		articles = append(articles, article{
			Number:  number,
			Content: title + "\n" + text,
		})
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if m := articleHeader.FindStringSubmatch(line); m != nil {
			flush()
			number = m[1]
			title = strings.TrimSpace(m[2])
			body = nil
			continue
		}
		if number == "" {
			continue
		}
		if line == "" && len(body) == 0 {
			continue
		}
		body = append(body, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("читать файл: %w", err)
	}

	flush()
	return articles, nil
}
