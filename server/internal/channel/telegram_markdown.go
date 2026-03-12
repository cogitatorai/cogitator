package channel

import (
	"fmt"
	"regexp"
	"strings"
)

// markdownToTelegramHTML converts common Markdown patterns produced by LLMs
// into Telegram-compatible HTML. Telegram's HTML mode supports a small subset
// of tags (<b>, <i>, <code>, <pre>, <a>) and requires only <, >, & to be
// escaped. This makes it far more reliable than Telegram's own "Markdown" or
// "MarkdownV2" parse modes, which choke on unbalanced or unexpected characters.
func markdownToTelegramHTML(s string) string {
	// Protect code blocks from further processing by replacing them with
	// placeholders, then restoring them at the end.
	var blocks []string
	placeholder := func(content string) string {
		idx := len(blocks)
		blocks = append(blocks, content)
		return fmt.Sprintf("\x00BLK%d\x00", idx)
	}

	// Fenced code blocks: ```lang\n...\n``` -> <pre>...</pre>
	s = reFencedCode.ReplaceAllStringFunc(s, func(m string) string {
		parts := reFencedCode.FindStringSubmatch(m)
		return placeholder("<pre>" + escapeHTML(parts[1]) + "</pre>")
	})

	// Inline code: `...` -> <code>...</code>
	s = reInlineCode.ReplaceAllStringFunc(s, func(m string) string {
		parts := reInlineCode.FindStringSubmatch(m)
		return placeholder("<code>" + escapeHTML(parts[1]) + "</code>")
	})

	// Escape HTML entities in the remaining text.
	s = escapeHTML(s)

	// Bold: **text** and __text__ -> <b>text</b>
	s = reBoldAsterisk.ReplaceAllString(s, "<b>$1</b>")
	s = reBoldUnderscore.ReplaceAllString(s, "<b>$1</b>")

	// Italic: *text* and _text_ -> <i>text</i>
	// Safe to run after bold since ** and __ are already consumed.
	s = reItalicAsterisk.ReplaceAllString(s, "<i>$1</i>")
	s = reItalicUnderscore.ReplaceAllString(s, "<i>$1</i>")

	// Links: [text](url) -> <a href="url">text</a>
	s = reLink.ReplaceAllString(s, `<a href="$2">$1</a>`)

	// Headings: strip # prefixes, wrap in <b>
	s = reHeading.ReplaceAllString(s, "<b>$1</b>")

	// Restore protected blocks.
	for i, blk := range blocks {
		s = strings.Replace(s, fmt.Sprintf("\x00BLK%d\x00", i), blk, 1)
	}

	return s
}

var (
	reFencedCode       = regexp.MustCompile("(?s)```(?:\\w*)\n?(.*?)```")
	reInlineCode       = regexp.MustCompile("`([^`]+)`")
	reBoldAsterisk     = regexp.MustCompile(`\*\*(.+?)\*\*`)
	reBoldUnderscore   = regexp.MustCompile(`__(.+?)__`)
	reItalicAsterisk   = regexp.MustCompile(`\*(.+?)\*`)
	reItalicUnderscore = regexp.MustCompile(`_(.+?)_`)
	reLink             = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	reHeading          = regexp.MustCompile(`(?m)^#{1,6}\s+(.+)$`)
)


func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

