package fsutil

import (
	"os"
	"path/filepath"
)

// ReadLink reports where path links to, reading a Windows junction the same as a symlink, which a
// mode check would not: Lstat reports a junction as irregular rather than as a symlink.
func ReadLink(path string) (string, bool) {
	target, err := os.Readlink(path)
	return target, err == nil
}

// SameTarget reports whether target, as read from link, is the directory want. A relative target
// counts from link's folder.
func SameTarget(link, target, want string) bool {
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(link), target)
	}
	return filepath.Clean(target) == filepath.Clean(want)
}
