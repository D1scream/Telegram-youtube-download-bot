package music

import (
	"fmt"
	"strings"
)

const MyMusicPageSize = 10

func formatPagedList(items []string, page, pageSize int, listCmd, actionPrefix string) (string, bool) {
	total := len(items)
	if total == 0 {
		return "", false
	}

	totalPages := (total + pageSize - 1) / pageSize
	if page < 1 || page > totalPages {
		return fmt.Sprintf("Нет страницы %d. Всего страниц: %d.\n/%s", page, totalPages, listCmd), false
	}

	start := (page - 1) * pageSize
	end := min(start+pageSize, total)

	parts := make([]string, 0, end-start)
	for i, name := range items[start:end] {
		index := start + i + 1
		parts = append(parts, fmt.Sprintf("%d. %s\n/%s%d", index, name, actionPrefix, index))
	}

	footer := fmt.Sprintf("\n\nСтраница %d/%d", page, totalPages)
	if page > 1 {
		footer += fmt.Sprintf("\n/%s %d", listCmd, page-1)
	}
	if page < totalPages {
		footer += fmt.Sprintf("\n/%s %d", listCmd, page+1)
	}
	return strings.Join(parts, "\n\n") + footer, true
}

func FormatMyMusicReply(files []string, page int) (string, bool) {
	return formatPagedList(files, page, MyMusicPageSize, "mymusic", "delete")
}
