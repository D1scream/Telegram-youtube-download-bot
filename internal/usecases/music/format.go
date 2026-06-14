package music

import (
	"fmt"
	"path"
	"strings"

	"telegram-bot/internal/entities"
)

const maxDisplayPathLen = 120

func formatTracksReply(tracks []entities.Track) string {
	parts := make([]string, len(tracks))
	for i, track := range tracks {
		parts[i] = formatTrack(i+1, track)
	}
	return strings.Join(parts, "\n\n")
}

func formatTrack(index int, track entities.Track) string {
	name := formatDisplayPath(track)
	if track.LocalPath != "" {
		name = "[C] " + name
	}
	meta := fmt.Sprintf("%s - %s - %s - %s",
		formatUploadSpeed(track.UploadSpeed),
		formatSize(track.Size),
		formatDuration(track.Length),
		formatQuality(track),
	)
	return fmt.Sprintf("%d. %s\n%s /download%d", index, name, meta, index)
}

func formatDisplayPath(track entities.Track) string {
	display := strings.ReplaceAll(track.Filename, "\\", "/")
	ext := strings.TrimPrefix(strings.ToLower(track.Extension), ".")
	if ext == "" {
		ext = strings.TrimPrefix(strings.ToLower(path.Ext(display)), ".")
	}
	if ext != "" && !strings.HasSuffix(strings.ToLower(display), "."+ext) {
		display += "." + ext
	}
	return truncateDisplayPath(display)
}

func truncateDisplayPath(display string) string {
	runes := []rune(display)
	if len(runes) <= maxDisplayPathLen {
		return display
	}
	start := len(runes) - (maxDisplayPathLen - 1)
	return "…" + string(runes[start:])
}

func formatUploadSpeed(bps int) string {
	if bps <= 0 {
		return "—"
	}
	const mb = 1024 * 1024
	if bps >= mb {
		return fmt.Sprintf("%.1f MB/s", float64(bps)/float64(mb))
	}
	return fmt.Sprintf("%d KB/s", bps/1024)
}

func formatSize(size int64) string {
	const mb = 1024 * 1024
	if size < mb {
		return fmt.Sprintf("%d KB", size/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(size)/float64(mb))
}

func formatDuration(seconds int) string {
	if seconds <= 0 {
		return "—"
	}
	minutes := seconds / 60
	secs := seconds % 60
	return fmt.Sprintf("%d:%02d", minutes, secs)
}

func formatQuality(track entities.Track) string {
	if track.BitRate > 0 {
		if track.SampleRate > 0 {
			return fmt.Sprintf("%d kbps, %d Hz", track.BitRate, track.SampleRate)
		}
		return fmt.Sprintf("%d kbps", track.BitRate)
	}
	if track.BitDepth > 0 && track.SampleRate > 0 {
		return fmt.Sprintf("%d bit, %d Hz", track.BitDepth, track.SampleRate)
	}
	if track.BitDepth > 0 {
		return fmt.Sprintf("%d bit", track.BitDepth)
	}
	if track.SampleRate > 0 {
		return fmt.Sprintf("%d Hz", track.SampleRate)
	}
	return "—"
}
