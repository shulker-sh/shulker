package project

import "strings"

// Slugify makes a name a project key: lowercase, with each run of anything but letters, digits,
// dots, underscores and dashes turned into one dash. Dashes the name already holds stay as written.
func Slugify(s string) string {
	var b strings.Builder
	replaced := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
			replaced = false
		case !replaced:
			b.WriteRune('-')
			replaced = true
		}
	}
	out := strings.Trim(b.String(), "._-")
	if out == "" {
		return "shulker-project"
	}
	return out
}
