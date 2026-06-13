package music

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-telegram/bot/models"

)

func TestAllowedExtension(t *testing.T) {
	formats := []string{".mp3", ".flac"}
	if _, err := allowedExtension("song.flac", formats); err != nil {
		t.Fatalf("expected flac to be allowed: %v", err)
	}
	if _, err := allowedExtension("song.txt", formats); err == nil {
		t.Fatal("expected txt to be rejected")
	}
}

func TestUniquePath(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "track.mp3")
	if err := os.WriteFile(first, []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := uniquePath(dir, "track.mp3")
	if got == first {
		t.Fatalf("expected alternate path, got %s", got)
	}
	if filepath.Base(got) != "track (1).mp3" {
		t.Fatalf("unexpected filename: %s", got)
	}
}

func TestSafeUploadBasename(t *testing.T) {
	if _, ok := safeUploadBasename("../secret.mp3"); ok {
		t.Fatal("expected path traversal to be rejected")
	}
	if got, ok := safeUploadBasename("track.mp3"); !ok || got != "track.mp3" {
		t.Fatalf("unexpected result: %q %v", got, ok)
	}
}

func TestUploadRegistryOwnsOnlyAddedUser(t *testing.T) {
	dir := t.TempDir()
	reg := newUserFileRegistry(dir, ".upload_registry.json")
	if err := reg.add(42, "song.mp3"); err != nil {
		t.Fatal(err)
	}
	if !reg.owns(42, "song.mp3") {
		t.Fatal("expected owner to have access")
	}
	if reg.owns(7, "song.mp3") {
		t.Fatal("other user must not have access")
	}
	if _, err := reg.remove(42, "song.mp3"); err != nil {
		t.Fatal(err)
	}
	if reg.owns(42, "song.mp3") {
		t.Fatal("expected entry to be removed")
	}
}

func TestUploadRegistryListSorted(t *testing.T) {
	dir := t.TempDir()
	reg := newUserFileRegistry(dir, ".upload_registry.json")
	for _, name := range []string{"z.mp3", "a.mp3"} {
		if err := reg.add(1, name); err != nil {
			t.Fatal(err)
		}
	}
	got := reg.list(1)
	if len(got) != 2 || got[0] != "a.mp3" || got[1] != "z.mp3" {
		t.Fatalf("unexpected list: %#v", got)
	}
}

func TestUploadFetchErrorMessageTooBig(t *testing.T) {
	msg := uploadFetchErrorMessage(fmt.Errorf("bad request, Bad Request: file is too big"))
	if !strings.Contains(msg, "слишком большой") {
		t.Fatalf("unexpected message: %s", msg)
	}
}

func TestAudioFilename(t *testing.T) {
	name := audioFilename(&models.Audio{
		Performer: "Artist",
		Title:     "Song",
		MimeType:  "audio/mpeg",
	})
	if name != "Artist - Song.mp3" {
		t.Fatalf("unexpected name: %s", name)
	}
}
