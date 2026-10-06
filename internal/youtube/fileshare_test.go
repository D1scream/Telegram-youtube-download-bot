package youtube

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestLitterboxUpload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clip [video].mkv")
	if err := os.WriteFile(path, []byte("video bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength <= 0 {
			t.Errorf("ContentLength = %d, want known size", r.ContentLength)
		}
		if got := r.FormValue("reqtype"); got != "fileupload" {
			t.Errorf("reqtype = %q", got)
		}
		if got := r.FormValue("time"); got != litterboxRetention {
			t.Errorf("time = %q", got)
		}
		file, header, err := r.FormFile("fileToUpload")
		if err != nil {
			t.Fatalf("fileToUpload: %v", err)
		}
		defer file.Close()
		data, _ := io.ReadAll(file)
		if header.Filename != "clip [video].mkv" || string(data) != "video bytes" {
			t.Errorf("file = %q %q", header.Filename, data)
		}
		_, _ = w.Write([]byte("https://litter.catbox.moe/abc123.mkv\n"))
	}))
	defer server.Close()

	link, err := (&litterbox{url: server.URL, client: server.Client()}).upload(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if link != "https://litter.catbox.moe/abc123.mkv" {
		t.Fatalf("link = %q", link)
	}
}

func TestLitterboxUploadRejectsUnexpectedResponse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.mp3")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("File too large"))
	}))
	defer server.Close()

	if _, err := (&litterbox{url: server.URL, client: server.Client()}).upload(context.Background(), path); err == nil {
		t.Fatal("expected error for non-URL response")
	}
}
