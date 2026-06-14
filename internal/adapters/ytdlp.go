package adapters

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const ytdlpDownloadTimeout = 30 * time.Minute

type YtdlpConfig struct {
	BinPath        string
	CookiesFile    string
	CookiesBrowser string
	OutputDir      string
}

type YtdlpAdapter struct {
	cfg YtdlpConfig
}

func NewYtdlpAdapter(cfg YtdlpConfig) (*YtdlpAdapter, error) {
	bin, err := resolveYtdlpBin(cfg.BinPath)
	if err != nil {
		return nil, err
	}
	outDir := strings.TrimSpace(cfg.OutputDir)
	if outDir == "" {
		outDir = "yt_downloads"
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, fmt.Errorf("создать каталог yt-dlp: %w", err)
	}
	cfg.BinPath = bin
	cfg.OutputDir = outDir
	return &YtdlpAdapter{cfg: cfg}, nil
}

func resolveYtdlpBin(bin string) (string, error) {
	bin = strings.TrimSpace(bin)
	if bin == "" {
		bin = "yt-dlp"
	}
	if strings.Contains(bin, "/") {
		if _, err := os.Stat(bin); err == nil {
			return bin, nil
		}
		if path, err := exec.LookPath("yt-dlp"); err == nil {
			return path, nil
		}
		return "", fmt.Errorf("yt-dlp не найден (%s)", bin)
	}
	path, err := exec.LookPath(bin)
	if err != nil {
		return "", fmt.Errorf("yt-dlp не найден (%s): %w", bin, err)
	}
	return path, nil
}

func (y *YtdlpAdapter) DownloadAudio(ctx context.Context, pageURL string) (string, error) {
	return y.download(ctx, pageURL, "bestaudio", "%(title)s [audio].%(ext)s", "")
}

func (y *YtdlpAdapter) DownloadVideo(ctx context.Context, pageURL string) (string, error) {
	return y.download(ctx, pageURL, "bestvideo+bestaudio/best", "%(title)s [video].%(ext)s", "mkv")
}

func (y *YtdlpAdapter) download(ctx context.Context, pageURL, format, outputTemplate, mergeFormat string) (string, error) {
	workDir, err := os.MkdirTemp(y.cfg.OutputDir, "job-*")
	if err != nil {
		return "", err
	}

	outPattern := filepath.Join(workDir, outputTemplate)
	args := []string{
		"--no-playlist",
		"--no-warnings",
		"-f", format,
		"-o", outPattern,
		"--print", "after_move:filepath",
	}
	if mergeFormat != "" {
		args = append(args, "--merge-output-format", mergeFormat)
	}
	if err := y.appendCookieArgs(&args, workDir); err != nil {
		return "", err
	}
	args = append(args, pageURL)

	ctx, cancel := context.WithTimeout(ctx, ytdlpDownloadTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, y.cfg.BinPath, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("yt-dlp: %s", msg)
	}

	path := strings.TrimSpace(string(out))
	lines := strings.Split(path, "\n")
	path = strings.TrimSpace(lines[len(lines)-1])
	if path == "" {
		return "", fmt.Errorf("yt-dlp: пустой путь к файлу")
	}
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("yt-dlp: файл не найден: %w", err)
	}
	return path, nil
}

func (y *YtdlpAdapter) appendCookieArgs(args *[]string, workDir string) error {
	if file := strings.TrimSpace(y.cfg.CookiesFile); file != "" {
		if _, err := os.Stat(file); err == nil {
			writable := filepath.Join(workDir, "cookies.txt")
			if err := copyFile(file, writable); err != nil {
				return fmt.Errorf("скопировать cookies: %w", err)
			}
			*args = append(*args, "--cookies", writable)
			return nil
		}
	}
	if browser := strings.TrimSpace(y.cfg.CookiesBrowser); browser != "" {
		*args = append(*args, "--cookies-from-browser", browser)
	}
	return nil
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
