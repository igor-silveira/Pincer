package whatsapp

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	codeBlockRe     = regexp.MustCompile("(?s)```(\\w*)\n(.*?)```")
	inlineCodeRe    = regexp.MustCompile("`([^`]+)`")
	boldItalicRe    = regexp.MustCompile(`\*{3}(.+?)\*{3}`)
	boldRe          = regexp.MustCompile(`\*{2}(.+?)\*{2}`)
	italicUnderRe   = regexp.MustCompile(`_(.+?)_`)
	italicStarRe    = regexp.MustCompile(`(?:^|(?P<pre>[^*]))\*(?P<text>[^*]+?)\*(?:[^*]|$)`)
	strikethroughRe = regexp.MustCompile(`~~(.+?)~~`)
	linkRe          = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	headerRe        = regexp.MustCompile(`(?m)^#{1,6}\s+(.+)$`)
	bulletStarRe    = regexp.MustCompile(`(?m)^(\s*)\*(\s)`)
)

// markdownToWhatsApp converts standard Markdown to WhatsApp formatting.
func markdownToWhatsApp(s string) string {
	var protected []string
	protect := func(replacement string) string {
		idx := len(protected)
		ph := fmt.Sprintf("\x00PH%d\x00", idx)
		protected = append(protected, replacement)
		return ph
	}

	// 1. Protect code blocks (``` ... ```).
	s = codeBlockRe.ReplaceAllStringFunc(s, func(m string) string {
		parts := codeBlockRe.FindStringSubmatch(m)
		return protect("```\n" + parts[2] + "```")
	})

	// 2. Protect inline code (` ... `) → WhatsApp monospace (``` ... ```).
	s = inlineCodeRe.ReplaceAllStringFunc(s, func(m string) string {
		parts := inlineCodeRe.FindStringSubmatch(m)
		return protect("```" + parts[1] + "```")
	})

	// 3. Headers → *bold text* (protected from italic conversion).
	s = headerRe.ReplaceAllStringFunc(s, func(m string) string {
		parts := headerRe.FindStringSubmatch(m)
		return protect("*" + parts[1] + "*")
	})

	// 4. Bold+italic: ***text*** → *_text_* (protected).
	s = boldItalicRe.ReplaceAllStringFunc(s, func(m string) string {
		parts := boldItalicRe.FindStringSubmatch(m)
		return protect("*_" + parts[1] + "_*")
	})

	// 5. Bold: **text** → *text*
	// Protect these so step 6 doesn't turn them into italics.
	s = boldRe.ReplaceAllStringFunc(s, func(m string) string {
		parts := boldRe.FindStringSubmatch(m)
		return protect("*" + parts[1] + "*")
	})

	// 6. Italic: *text* → _text_ (only remaining single-asterisk pairs)
	s = replaceItalicStars(s)

	// 7. Strikethrough: ~~text~~ → ~text~
	s = strikethroughRe.ReplaceAllString(s, "~$1~")

	// 8. Links: [text](url) → text (url)
	s = linkRe.ReplaceAllString(s, "$1 ($2)")

	// 9. Bullet points: * item → • item
	s = bulletStarRe.ReplaceAllString(s, "${1}•${2}")

	// 10. Restore protected content.
	for i, repl := range protected {
		ph := fmt.Sprintf("\x00PH%d\x00", i)
		s = strings.Replace(s, ph, repl, 1)
	}

	return s
}

// replaceItalicStars converts remaining *text* to _text_.
func replaceItalicStars(s string) string {
	var b strings.Builder
	b.Grow(len(s))

	i := 0
	for i < len(s) {
		if s[i] != '*' {
			b.WriteByte(s[i])
			i++
			continue
		}
		// Look for a closing * that isn't immediately adjacent.
		end := strings.IndexByte(s[i+1:], '*')
		if end <= 0 {
			b.WriteByte(s[i])
			i++
			continue
		}
		end += i + 1
		inner := s[i+1 : end]
		// Only convert if it looks like inline text (no newlines).
		if strings.ContainsAny(inner, "\n\r") {
			b.WriteByte(s[i])
			i++
			continue
		}
		b.WriteByte('_')
		b.WriteString(inner)
		b.WriteByte('_')
		i = end + 1
	}
	return b.String()
}
