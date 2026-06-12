package music

import (
	"os"
	"path/filepath"
	"testing"

	"telegram-bot/internal/entities"
)

func TestEnrichLocalTrackUsesRealMP3(t *testing.T) {
	path := filepath.Join(`C:\Sync\Music\GameOst`, "02. NiKiNiT - Through The Stars (Flying Version).mp3")
	if _, err := os.Stat(path); err != nil {
		t.Skip("local sample mp3 not available")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	track := enrichLocalTrack(entities.Track{
		LocalPath: path,
		Size:      info.Size(),
	})
	if track.Length < 130 || track.Length > 140 {
		t.Fatalf("expected ~2:16 duration, got %d sec", track.Length)
	}
	if track.BitRate != 320 {
		t.Fatalf("expected 320 kbps, got %d", track.BitRate)
	}
	if track.SampleRate != 44100 {
		t.Fatalf("expected 44100 Hz, got %d", track.SampleRate)
	}
}
