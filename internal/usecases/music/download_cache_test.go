package music

import (
	"path/filepath"
	"testing"
)

func TestSafeCacheRelative(t *testing.T) {
	if _, ok := safeCacheRelative("../secret.mp3"); ok {
		t.Fatal("expected .. to be rejected")
	}
	if got, ok := safeCacheRelative("peer/track.mp3"); !ok || got != "peer/track.mp3" {
		t.Fatalf("unexpected result: %q %v", got, ok)
	}
}

func TestDownloadCacheRegistryListSorted(t *testing.T) {
	dir := t.TempDir()
	reg := newUserFileRegistry(dir, ".download_cache_registry.json")
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

func TestPathWithinDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "track.mp3")
	rel, ok := pathWithinDir(path, dir)
	if !ok || rel != "nested/track.mp3" {
		t.Fatalf("unexpected relative path: %q %v", rel, ok)
	}
}
