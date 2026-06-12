package music

import (
	"testing"

	"telegram-bot/internal/entities"
)

func track(name string, size int64) entities.Track {
	return entities.Track{Filename: name, Size: size}
}

func TestMergeWithLocalPriorityRespectsLimit(t *testing.T) {
	local := make([]entities.Track, 50)
	for i := range local {
		local[i] = track("local"+string(rune('a'+i%26))+".mp3", int64(i+1)<<20)
	}
	remote := make([]entities.Track, 20)
	for i := range remote {
		remote[i] = track("remote"+string(rune('a'+i%26))+".mp3", int64(i+100)<<20)
	}
	got := mergeWithLocalPriority(local, remote, 10)
	if len(got) != 10 {
		t.Fatalf("expected 10 tracks, got %d", len(got))
	}
}

func TestMergeWithLocalPriorityFillsWithRemote(t *testing.T) {
	local := []entities.Track{
		track("local1.mp3", 1<<20),
		track("local2.mp3", 2<<20),
		track("local3.mp3", 3<<20),
	}
	remote := []entities.Track{
		track("remote1.mp3", 4<<20),
		track("remote2.mp3", 5<<20),
		track("remote3.mp3", 6<<20),
		track("remote4.mp3", 7<<20),
		track("remote5.mp3", 8<<20),
		track("remote6.mp3", 9<<20),
		track("remote7.mp3", 10<<20),
	}
	got := mergeWithLocalPriority(local, remote, 10)
	if len(got) != 10 {
		t.Fatalf("expected 10 tracks, got %d", len(got))
	}
}
