package imposter

import (
	"bufio"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	minWordRunes = 3
	maxWordRunes = 20
)

// ListDownloaded - значение Settings.List для скачанного списка слов.
const ListDownloaded = "downloaded"

// WordList - список слов из файла.
type WordList struct {
	words []string
}

func LoadWordList(path string) (*WordList, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("открыть файл слов: %w", err)
	}
	defer f.Close()

	var words []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") && validWord(line) {
			words = append(words, strings.ToLower(line))
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("прочитать файл слов: %w", err)
	}
	if len(words) == 0 {
		return nil, errors.New("файл слов пуст")
	}
	return &WordList{words: words}, nil
}

func (l *WordList) Len() int { return len(l.words) }

// Pick возвращает n разных случайных слов.
func (l *WordList) Pick(n int) ([]string, error) { return PickWords(l.words, n) }

// PickWords возвращает n разных случайных слов из words.
func PickWords(words []string, n int) ([]string, error) {
	if n > len(words) {
		return nil, fmt.Errorf("в списке слов только %d, нужно %d", len(words), n)
	}
	picked := make([]string, 0, n)
	for _, i := range rand.Perm(len(words))[:n] {
		picked = append(picked, words[i])
	}
	return picked, nil
}

// validWord пропускает только одно русское слово разумной длины.
func validWord(w string) bool {
	n := utf8.RuneCountInString(w)
	if n < minWordRunes || n > maxWordRunes {
		return false
	}
	for _, r := range w {
		if !unicode.Is(unicode.Cyrillic, r) && r != '-' {
			return false
		}
	}
	return true
}
