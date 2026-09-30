package out

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

var home = sync.OnceValue(func() string {
	dir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return dir
})

// hyperlinkTarget is the opening of an OSC 8 link, whose target has to stay an absolute URL.
var hyperlinkTarget = regexp.MustCompile("\x1b\\]8;;[^\x1b]*\x1b\\\\")

// Tilde shortens every path under the home directory in text to one starting with ~, leaving
// hyperlink targets whole. A path right after a quote stays whole too: it is a word of a command,
// and no shell expands ~ inside quotes.
func Tilde(text string) string {
	dir := home()
	if dir == "" || !strings.Contains(text, dir+string(filepath.Separator)) {
		return text
	}
	var b strings.Builder
	last := 0
	for _, m := range hyperlinkTarget.FindAllStringIndex(text, -1) {
		b.WriteString(tildeIn(text[last:m[0]], dir))
		b.WriteString(text[m[0]:m[1]])
		last = m[1]
	}
	b.WriteString(tildeIn(text[last:], dir))
	return b.String()
}

func tildeIn(text, dir string) string {
	prefix := dir + string(filepath.Separator)
	var b strings.Builder
	for {
		i := strings.Index(text, prefix)
		if i < 0 {
			b.WriteString(text)
			return b.String()
		}
		b.WriteString(text[:i])
		if i > 0 && (text[i-1] == '"' || text[i-1] == '\'') {
			b.WriteString(prefix)
		} else {
			b.WriteString("~" + string(filepath.Separator))
		}
		text = text[i+len(prefix):]
	}
}
