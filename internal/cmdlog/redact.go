package cmdlog

import (
	"path/filepath"
	"regexp"
	"strings"
)

// hiddenKey stands in for an API key wherever one turns up in an entry.
const hiddenKey = "[key]"

// A shorter key is a typo rather than a CurseForge key, and hiding it would cut into ordinary words.
const minKey = 8

var userinfo = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://)[^/?#@\s]+@`)

// Redaction scrubs what an entry holds that shouldn't leave the machine: credentials in a URL, the
// API keys in Keys, and the home directory, which reads as ~. The log on disk keeps all of it, and
// only what is read out of it is scrubbed.
type Redaction struct {
	Home string
	Keys []string
}

// Entry is e scrubbed, in every string it holds, the result payload included.
func (r Redaction) Entry(e Entry) Entry {
	return e.rewrite(r.String)
}

// String is s scrubbed.
func (r Redaction) String(s string) string {
	for _, key := range r.Keys {
		if len(key) >= minKey {
			s = strings.ReplaceAll(s, key, hiddenKey)
		}
	}
	return r.shortenHome(userinfo.ReplaceAllString(s, "${1}"))
}

func (r Redaction) shortenHome(s string) string {
	if r.Home == "" {
		return s
	}
	home := filepath.Clean(r.Home)
	if home == filepath.VolumeName(home)+string(filepath.Separator) {
		return s
	}
	var b strings.Builder
	for {
		i := strings.Index(s, home)
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		end := i + len(home)
		b.WriteString(s[:i])
		if (i == 0 || !inPath(s[i-1])) && (end == len(s) || !inName(s[end])) {
			b.WriteString("~")
		} else {
			b.WriteString(home)
		}
		s = s[end:]
	}
}

func inName(c byte) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' || c == '-' || c == '_' || c == '.'
}

func inPath(c byte) bool {
	return inName(c) || c == '/' || c == '\\'
}
