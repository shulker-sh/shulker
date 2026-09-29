package out

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// proseColumns caps how wide a wrapped line of prose runs, however wide the terminal.
const proseColumns = 100

// prose prints a glyph and its text, wrapped on a terminal to its width, capped at
// proseColumns, with each continuation indented under the text. Off a terminal the text stays
// on one line, for a log or a pipe to search.
func (l *Lines) prose(glyph, text string) {
	limit := 0
	if IsTerminal(unwrap(l.W)) {
		limit = min(TerminalWidth(l.W), proseColumns) - len(gutter) - Width(glyph) - 1
	}
	indent := strings.Repeat(" ", Width(glyph)+1)
	for i, line := range Wrap(text, limit) {
		if i == 0 {
			l.line(glyph + " " + line)
			continue
		}
		l.line(indent + line)
	}
}

// Wrap breaks text at spaces into lines no wider than limit, never inside a path or a word, so a
// flag like --json stays whole: a space between two words that both hold a / joins them. A word
// wider than limit gets a line of its own. A limit of 0 keeps one line.
func Wrap(text string, limit int) []string {
	if limit <= 0 || Width(text) <= limit {
		return []string{text}
	}
	var words []string
	for _, word := range strings.Split(text, " ") {
		if n := len(words); n > 0 && strings.Contains(ansi.Strip(words[n-1]), "/") && strings.Contains(ansi.Strip(word), "/") {
			words[n-1] += " " + word
			continue
		}
		words = append(words, word)
	}
	var lines []string
	current := ""
	for _, word := range words {
		switch {
		case current == "":
			current = word
		case Width(current)+1+Width(word) <= limit:
			current += " " + word
		default:
			lines = append(lines, current)
			current = word
		}
	}
	return append(lines, current)
}
