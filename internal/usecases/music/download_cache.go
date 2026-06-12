package music

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (s *SearchMusicService) ListUserCacheFiles(userID int64) []string {
	return s.cacheRegistry.list(userID)
}

func (s *SearchMusicService) deleteOwnedCacheFile(userID int64, relative string) error {
	path, ok := safeCacheRelative(relative)
	if !ok || !s.cacheRegistry.owns(userID, path) {
		return ErrNoMyMusicSession
	}

	fullPath, ok := resolveCachePath(s.downloadsDir, path)
	if !ok {
		return fmt.Errorf("некорректный путь к файлу")
	}

	if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	if _, err := s.cacheRegistry.remove(userID, path); err != nil {
		s.logger.WarnContext(context.Background(), "Файл удалён из кэша, но реестр не обновлён",
			"operation", "delete_cache",
			"user_id", userID,
			"filename", path,
			"err", err,
		)
	}
	return nil
}

func (s *SearchMusicService) registerDownloadCache(ctx context.Context, userID int64, localPath string) {
	if userID == 0 || s.cacheRegistry == nil || s.downloadsDir == "" {
		return
	}

	relative, ok := pathWithinDir(localPath, s.downloadsDir)
	if !ok {
		return
	}

	if err := s.cacheRegistry.add(userID, relative); err != nil {
		s.logger.WarnContext(ctx, "Не удалось записать файл в реестр кэша",
			"operation", "download_cache",
			"user_id", userID,
			"path", relative,
			"err", err,
		)
	}
}

func safeCacheRelative(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", false
	}
	name = filepath.ToSlash(filepath.Clean(name))
	if name == "." || name == ".." || strings.HasPrefix(name, "../") || strings.Contains(name, "/../") {
		return "", false
	}
	if strings.HasPrefix(name, "/") || strings.Contains(name, ":") {
		return "", false
	}
	return name, true
}

func resolveCachePath(downloadsDir, relative string) (string, bool) {
	relative, ok := safeCacheRelative(relative)
	if !ok {
		return "", false
	}
	full := filepath.Join(downloadsDir, filepath.FromSlash(relative))
	if _, ok := pathWithinDir(full, downloadsDir); !ok {
		return "", false
	}
	return full, true
}

func pathWithinDir(path, dir string) (string, bool) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", false
	}

	absPath = filepath.Clean(absPath)
	absDir = filepath.Clean(absDir)

	rel, err := filepath.Rel(absDir, absPath)
	if err != nil {
		return "", false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}
