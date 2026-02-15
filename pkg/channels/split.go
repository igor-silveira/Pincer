package channels

import (
	"strings"
	"unicode/utf8"
)

func SplitMessage(content string, maxLen int) []string {
	if maxLen <= 0 || len(content) <= maxLen {
		return []string{content}
	}

	var chunks []string
	for len(content) > 0 {
		if len(content) <= maxLen {
			chunks = append(chunks, content)
			break
		}

		cut := findCutPoint(content, maxLen)
		chunks = append(chunks, content[:cut])
		content = content[cut:]
	}
	return chunks
}

func findCutPoint(content string, maxLen int) int {
	effective := maxLen
	if codeBlockCut := findCodeBlockSafeCut(content, maxLen); codeBlockCut > 0 {
		effective = codeBlockCut
	}

	if idx := strings.LastIndex(content[:effective], "\n\n"); idx > 0 {
		return idx + 2
	}

	if idx := strings.LastIndex(content[:effective], "\n"); idx > 0 {
		return idx + 1
	}

	if idx := strings.LastIndex(content[:effective], " "); idx > 0 {
		return idx + 1
	}

	return runeAlignedCut(content, effective)
}

func findCodeBlockSafeCut(content string, maxLen int) int {
	open := false
	lastFenceEnd := 0
	i := 0
	for i < len(content) && i < maxLen {
		if strings.HasPrefix(content[i:], "```") {
			if !open {
				if i > 0 {
					lastFenceEnd = i
				}
				open = true
			} else {
				fenceEnd := i + 3
				for fenceEnd < len(content) && content[fenceEnd] != '\n' {
					fenceEnd++
				}
				if fenceEnd < len(content) && content[fenceEnd] == '\n' {
					fenceEnd++
				}
				open = false
				if fenceEnd <= maxLen {
					lastFenceEnd = fenceEnd
				}
			}
			skip := 3
			if open {
				for i+skip < len(content) && content[i+skip] != '\n' {
					skip++
				}
			}
			i += skip
			continue
		}
		i++
	}

	if open && lastFenceEnd > 0 {
		return lastFenceEnd
	}
	return 0
}

func runeAlignedCut(content string, maxLen int) int {
	if maxLen >= len(content) {
		return len(content)
	}
	for maxLen > 0 && !utf8.RuneStart(content[maxLen]) {
		maxLen--
	}
	if maxLen == 0 {
		_, size := utf8.DecodeRuneInString(content)
		return size
	}
	return maxLen
}
