package music

import (
	"fmt"
	"strings"
	"testing"
)

func TestFormatMyMusicReply(t *testing.T) {
	got, ok := FormatMyMusicReply([]string{"a.mp3", "peer/b.flac"}, 1)
	if !ok || !strings.Contains(got, "1. a.mp3") || !strings.Contains(got, "/delete2") || !strings.Contains(got, "Страница 1/1") {
		t.Fatalf("unexpected reply: %q %v", got, ok)
	}
}

func TestFormatMyMusicReplyPagination(t *testing.T) {
	files := make([]string, 15)
	for i := range files {
		files[i] = fmt.Sprintf("track%d.mp3", i+1)
	}
	page1, ok := FormatMyMusicReply(files, 1)
	if !ok || !strings.Contains(page1, "1. track1.mp3") || !strings.Contains(page1, "10. track10.mp3") || strings.Contains(page1, "11. track11.mp3") {
		t.Fatalf("unexpected page 1: %s", page1)
	}
	if !strings.Contains(page1, "/mymusic 2") {
		t.Fatalf("expected next page link: %s", page1)
	}

	page2, ok := FormatMyMusicReply(files, 2)
	if !ok || !strings.Contains(page2, "11. track11.mp3") || !strings.Contains(page2, "/mymusic 1") {
		t.Fatalf("unexpected page 2: %s", page2)
	}
}
