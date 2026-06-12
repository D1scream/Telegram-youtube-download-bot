package music

import (
	"errors"
	"testing"
)

func TestMyMusicSessionGetFile(t *testing.T) {
	store := newChatSessionStore[userMusicFile]()
	files := []userMusicFile{
		{path: "a.mp3", source: fileSourceUpload},
		{path: "cache/b.flac", source: fileSourceCache},
	}
	store.Set(1, 42, files)

	got, err := store.Get(1, 42, 2, ErrNoMyMusicSession, ErrIndexOutOfRange)
	if err != nil || got.path != "cache/b.flac" || got.source != fileSourceCache {
		t.Fatalf("unexpected file: %#v %v", got, err)
	}

	_, err = store.Get(1, 99, 1, ErrNoMyMusicSession, ErrIndexOutOfRange)
	if !errors.Is(err, ErrNoMyMusicSession) {
		t.Fatalf("expected ownership error, got %v", err)
	}

	_, err = store.Get(1, 42, 9, ErrNoMyMusicSession, ErrIndexOutOfRange)
	if !errors.Is(err, ErrIndexOutOfRange) {
		t.Fatalf("expected range error, got %v", err)
	}
}
