package music

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"telegram-bot/internal/entities"
)

const localCachedUsername = "__cached__"

type localSearchRoot struct {
	absPath string
	label   string
}

func buildLocalSearchRoots(dirs []string) []localSearchRoot {
	roots := make([]localSearchRoot, 0, len(dirs))
	for _, dir := range dirs {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		abs, err := filepath.Abs(dir)
		if err != nil {
			continue
		}
		if _, err := os.Stat(abs); err != nil {
			continue
		}
		roots = append(roots, localSearchRoot{
			absPath: abs,
			label:   filepath.Base(abs),
		})
	}
	return roots
}

func searchLocalTracks(query string, dirs []string, limit int, formats []string) ([]entities.Track, error) {
	tokens := queryTokens(query)
	if len(tokens) == 0 {
		return nil, nil
	}
	if limit < 1 {
		limit = 1
	}

	formatSet := allowedFormatSet(formats)
	roots := buildLocalSearchRoots(dirs)
	if len(roots) == 0 {
		return nil, nil
	}

	var tracks []entities.Track
	for _, root := range roots {
		err := filepath.WalkDir(root.absPath, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if len(tracks) >= limit {
				return fs.SkipAll
			}

			ext := strings.ToLower(filepath.Ext(path))
			if len(formatSet) > 0 {
				if _, ok := formatSet[ext]; !ok {
					return nil
				}
			}

			rel, err := filepath.Rel(root.absPath, path)
			if err != nil {
				return nil
			}
			displayPath := root.label + "/" + filepath.ToSlash(rel)
			if !pathMatchesQuery(displayPath, tokens) {
				return nil
			}

			info, err := d.Info()
			if err != nil {
				return nil
			}

			tracks = append(tracks, enrichLocalTrack(entities.Track{
				Filename:  displayPath,
				Size:      info.Size(),
				Extension: ext,
				Username:  localCachedUsername,
				LocalPath: path,
			}))
			return nil
		})
		if err != nil {
			return tracks, err
		}
	}

	sortLocalTracks(tracks, tokens)
	if len(tracks) > limit {
		tracks = tracks[:limit]
	}
	return tracks, nil
}

func queryTokens(query string) []string {
	parts := strings.Fields(strings.ToLower(query))
	return slices.DeleteFunc(parts, func(s string) bool { return s == "" })
}

func allowedFormatSet(formats []string) map[string]struct{} {
	set := make(map[string]struct{}, len(formats))
	for _, format := range formats {
		ext := strings.ToLower(format)
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		set[ext] = struct{}{}
	}
	return set
}

func pathMatchesQuery(path string, tokens []string) bool {
	lower := strings.ToLower(path)
	for _, token := range tokens {
		if !strings.Contains(lower, token) {
			return false
		}
	}
	return true
}

func sortLocalTracks(tracks []entities.Track, tokens []string) {
	slices.SortFunc(tracks, func(a, b entities.Track) int {
		aScore := localMatchScore(a.Filename, tokens)
		bScore := localMatchScore(b.Filename, tokens)
		if aScore != bScore {
			return bScore - aScore
		}
		if len(a.Filename) != len(b.Filename) {
			return len(a.Filename) - len(b.Filename)
		}
		return strings.Compare(a.Filename, b.Filename)
	})
}

func localMatchScore(path string, tokens []string) int {
	lower := strings.ToLower(path)
	base := strings.ToLower(filepath.Base(path))
	score := 0
	for _, token := range tokens {
		if strings.Contains(base, token) {
			score += 2
		} else if strings.Contains(lower, token) {
			score++
		}
	}
	return score
}

func mergeWithLocalPriority(local, remote []entities.Track, limit int) []entities.Track {
	if limit < 1 {
		limit = 1
	}
	out := append([]entities.Track(nil), local...)
	if len(out) > limit {
		return out[:limit]
	}
	for _, track := range remote {
		if len(out) >= limit {
			break
		}
		if duplicatesLocalTrack(track, local) {
			continue
		}
		out = append(out, track)
	}
	return out
}

func duplicatesLocalTrack(track entities.Track, local []entities.Track) bool {
	remoteBase := strings.ToLower(filepath.Base(strings.ReplaceAll(track.Filename, "\\", "/")))
	for _, cached := range local {
		if cached.Size != track.Size {
			continue
		}
		if remoteBase == strings.ToLower(filepath.Base(strings.ReplaceAll(cached.Filename, "\\", "/"))) {
			return true
		}
	}
	return false
}