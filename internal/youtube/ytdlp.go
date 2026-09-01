package youtube

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

type ytdlp struct {
	bin            string
	cookiesFile    string
	cookiesBrowser string
	outputDir      string
}

func newYtdlp(cfg Config) (*ytdlp, error) {
	bin, err := resolveYtdlpBin(cfg.Bin)
	if err != nil {
		return nil, err
	}
	outDir := strings.TrimSpace(cfg.DownloadDir)
	if outDir == "" {
		outDir = "yt_downloads"
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, fmt.Errorf("создать каталог yt-dlp: %w", err)
	}
	return &ytdlp{
		bin:            bin,
		cookiesFile:    cfg.CookiesFile,
		cookiesBrowser: cfg.CookiesBrowser,
		outputDir:      outDir,
	}, nil
}

func resolveYtdlpBin(bin string) (string, error) {
	bin = strings.TrimSpace(bin)
	if bin == "" {
		bin = "yt-dlp"
	}
	if looksLikePath(bin) {
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

func looksLikePath(bin string) bool {
	return filepath.IsAbs(bin) || strings.ContainsAny(bin, `/\`)
}

func (y *ytdlp) downloadAudio(ctx context.Context, pageURL string) (string, error) {
	return y.download(ctx, pageURL, "bestaudio", "%(title)s [audio].%(ext)s", "")
}

func (y *ytdlp) downloadVideo(ctx context.Context, pageURL string) (string, error) {
	return y.download(ctx, pageURL, "bestvideo+bestaudio/best", "%(title)s [video].%(ext)s", "mkv")
}

func (y *ytdlp) download(ctx context.Context, pageURL, format, outputTemplate, mergeFormat string) (string, error) {
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
	cookieArgs, err := y.cookieArgs(workDir)
	if err != nil {
		return "", err
	}
	args = append(args, cookieArgs...)
	args = append(args, pageURL)

	ctx, cancel := context.WithTimeout(ctx, ytdlpDownloadTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, y.bin, args...)
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

func (y *ytdlp) cookieArgs(workDir string) ([]string, error) {
	if file := strings.TrimSpace(y.cookiesFile); file != "" {
		if _, err := os.Stat(file); err == nil {
			writable := filepath.Join(workDir, "cookies.txt")
			if err := copyFile(file, writable); err != nil {
				return nil, fmt.Errorf("скопировать cookies: %w", err)
			}
			return []string{"--cookies", writable}, nil
		}
	}
	if browser := strings.TrimSpace(y.cookiesBrowser); browser != "" {
		return []string{"--cookies-from-browser", browser}, nil
	}
	return nil, nil
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
