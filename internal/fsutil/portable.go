package fsutil

import "strings"

// IsPortableLocal reports whether a slash-separated relative path stays inside the folder it is
// joined onto on every OS: no `..` segment, no leading `/` or `\`, no colon (a drive letter or an
// NTFS stream), and no Windows device name in any segment. filepath.IsLocal answers only for the
// OS it runs on, and a path a pack's author writes on macOS is laid out on Windows too.
func IsPortableLocal(rel string) bool {
	if rel == "" || strings.HasPrefix(rel, "/") || strings.HasPrefix(rel, `\`) || strings.Contains(rel, ":") {
		return false
	}
	for _, part := range strings.FieldsFunc(rel, func(r rune) bool { return r == '/' || r == '\\' }) {
		if part == ".." || isWindowsDeviceName(part) {
			return false
		}
	}
	return true
}

// isWindowsDeviceName reports whether Windows opens a path segment as a device rather than a file,
// which it does whatever extension follows the name.
func isWindowsDeviceName(part string) bool {
	name, _, _ := strings.Cut(part, ".")
	name = strings.ToUpper(strings.TrimRight(name, " "))
	switch name {
	case "CON", "PRN", "AUX", "NUL":
		return true
	}
	if len(name) == 4 && (strings.HasPrefix(name, "COM") || strings.HasPrefix(name, "LPT")) {
		return name[3] >= '1' && name[3] <= '9'
	}
	return false
}
