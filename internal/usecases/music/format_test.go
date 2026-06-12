package music

import (
	"strings"
	"testing"

	"telegram-bot/internal/entities"
)

func TestTruncateDisplayPath(t *testing.T) {
	long := strings.Repeat("a", 200) + ".mp3"
	got := truncateDisplayPath(long)
	if len([]rune(got)) != maxDisplayPathLen {
		t.Fatalf("expected len %d, got %d", maxDisplayPathLen, len([]rune(got)))
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("expected ellipsis suffix: %q", got)
	}
}

func TestSplitTelegramMessagesByTracks(t *testing.T) {
	track := formatTrack(1, entities.Track{Filename: "song.mp3", Size: 5 << 20})
	chunks := splitTelegramMessages(strings.Repeat(track+"\n\n", 40), 500)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	for _, chunk := range chunks {
		if len(chunk) > 500 {
			t.Fatalf("chunk exceeds limit: %d", len(chunk))
		}
	}
}

func TestSplitTelegramMessagesShort(t *testing.T) {
	text := "hello"
	got := splitTelegramMessages(text, 4096)
	if len(got) != 1 || got[0] != text {
		t.Fatalf("unexpected chunks: %#v", got)
	}
}
