package project

import "strings"

// Slugify makes a name a project key: lowercase, with anything but letters, digits, dots,
// underscores and dashes turned into a dash.
func Slugify(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "._-")
	if out == "" {
		return "shulker-project"
	}
	return out
}
