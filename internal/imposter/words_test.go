package imposter

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFilePick(t *testing.T) {
	path := filepath.Join(t.TempDir(), "words.txt")
	if err := os.WriteFile(path, []byte("# comment\nкошка\nсобака\nmouse\n\nлампа\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := LoadWordList(path)
	if err != nil {
		t.Fatal(err)
	}
	if f.Len() != 3 {
		t.Fatalf("len=%d", f.Len())
	}
	got, err := f.Pick(3)
	if err != nil || len(got) != 3 {
		t.Fatalf("got %v err=%v", got, err)
	}
	if _, err := f.Pick(4); err == nil {
		t.Fatal("ожидалась ошибка")
	}
}
