package music

import "strings"

const telegramMaxMessageLen = 4096

func splitTelegramMessages(text string, maxLen int) []string {
	if maxLen <= 0 || len(text) <= maxLen {
		return []string{text}
	}

	blocks := strings.Split(text, "\n\n")
	chunks := make([]string, 0, len(blocks))
	var current strings.Builder

	flush := func() {
		if current.Len() == 0 {
			return
		}
		chunks = append(chunks, current.String())
		current.Reset()
	}

	for _, block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		if len(block) > maxLen {
			flush()
			chunks = append(chunks, splitLongText(block, maxLen)...)
			continue
		}

		extra := len(block)
		if current.Len() > 0 {
			extra += 2
		}
		if current.Len()+extra > maxLen {
			flush()
		}
		if current.Len() > 0 {
			current.WriteString("\n\n")
		}
		current.WriteString(block)
	}
	flush()
	return chunks
}

func splitLongText(text string, maxLen int) []string {
	if len(text) <= maxLen {
		return []string{text}
	}

	lines := strings.Split(text, "\n")
	chunks := make([]string, 0, len(lines)/4+1)
	var current strings.Builder

	flush := func() {
		if current.Len() == 0 {
			return
		}
		chunks = append(chunks, current.String())
		current.Reset()
	}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(line) > maxLen {
			flush()
			for len(line) > maxLen {
				chunks = append(chunks, line[:maxLen])
				line = line[maxLen:]
			}
			if line != "" {
				current.WriteString(line)
			}
			continue
		}

		extra := len(line)
		if current.Len() > 0 {
			extra++
		}
		if current.Len()+extra > maxLen {
			flush()
		}
		if current.Len() > 0 {
			current.WriteByte('\n')
		}
		current.WriteString(line)
	}
	flush()
	return chunks
}
