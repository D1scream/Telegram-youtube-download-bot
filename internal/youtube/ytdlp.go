package youtube

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const ytdlpDownloadTimeout = 30 * time.Minute

type ytdlp struct {
	cookiesFile string
	outputDir   string
}

func newYtdlp(cfg Config) (*ytdlp, error) {
	outDir := strings.TrimSpace(cfg.DownloadDir)
	if outDir == "" {
		outDir = "yt_downloads"
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, fmt.Errorf("создать каталог yt-dlp: %w", err)
	}
	return &ytdlp{cookiesFile: cfg.CookiesFile, outputDir: outDir}, nil
}

type downloadResult struct {
	path       string
	cookiesErr error
}

func (y *ytdlp) downloadAudio(ctx context.Context, pageURL string) (downloadResult, error) {
	return y.download(ctx, pageURL, "bestaudio", "%(title)s [audio].%(ext)s",
		"--extract-audio", "--audio-format", "mp3", "--audio-quality", "192K")
}

func (y *ytdlp) downloadVideo(ctx context.Context, pageURL string) (downloadResult, error) {
	return y.download(ctx, pageURL, "bestvideo+bestaudio/best", "%(title)s [video].%(ext)s",
		"--merge-output-format", "mkv")
}

func (y *ytdlp) download(ctx context.Context, pageURL, format, outputTemplate string, extraArgs ...string) (downloadResult, error) {
	args := []string{
		"--no-playlist",
		"--no-warnings",
		"-f", format,
		"--print", "after_move:filepath",
	}
	args = append(args, extraArgs...)
	if url, section, ok := strings.Cut(pageURL, " "); ok {
		pageURL = url
		args = append(args, "--download-sections", "*"+strings.TrimSpace(section), "--force-keyframes-at-cuts")
	}

	ctx, cancel := context.WithTimeout(ctx, ytdlpDownloadTimeout)
	defer cancel()

	if !y.hasCookies() {
		path, err := y.attempt(ctx, args, outputTemplate, pageURL, false)
		return downloadResult{path: path}, err
	}
	path, err := y.attempt(ctx, args, outputTemplate, pageURL, true)
	if err == nil {
		return downloadResult{path: path}, nil
	}
	path, plainErr := y.attempt(ctx, args, outputTemplate, pageURL, false)
	if plainErr != nil {
		return downloadResult{}, err
	}
	return downloadResult{path: path, cookiesErr: err}, nil
}

func (y *ytdlp) attempt(ctx context.Context, args []string, outputTemplate, pageURL string, withCookies bool) (string, error) {
	workDir, err := os.MkdirTemp(y.outputDir, "job-*")
	if err != nil {
		return "", err
	}
	keep := false
	defer func() {
		if !keep {
			os.RemoveAll(workDir)
		}
	}()

	args = slices.Clone(args)
	if withCookies {
		cookieArgs, err := y.cookieArgs(workDir)
		if err != nil {
			return "", err
		}
		args = append(args, cookieArgs...)
	}
	args = append(args, "-o", filepath.Join(workDir, outputTemplate), pageURL)

	cmd := exec.CommandContext(ctx, "yt-dlp", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return "", fmt.Errorf("yt-dlp: %w", err)
		}
		return "", fmt.Errorf("yt-dlp: %s: %w", msg, err)
	}

	path := lastNonEmptyLine(string(out))
	if path == "" {
		return "", fmt.Errorf("yt-dlp: пустой путь к файлу")
	}
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("yt-dlp: файл не найден: %w", err)
	}
	keep = true
	return path, nil
}

func (y *ytdlp) hasCookies() bool {
	_, err := os.Stat(y.cookiesFile)
	return err == nil
}

func (y *ytdlp) cookieArgs(workDir string) ([]string, error) {
	writable := filepath.Join(workDir, "cookies.txt")
	if err := copyFile(y.cookiesFile, writable); err != nil {
		return nil, fmt.Errorf("скопировать cookies: %w", err)
	}
	return []string{"--cookies", writable}, nil
}

func lastNonEmptyLine(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
