package fsutil

import "golang.org/x/sys/unix"

// cloneFile makes dst, which must not exist, a copy-on-write clone of src. APFS clones; any other
// filesystem refuses.
func cloneFile(src, dst string) error {
	return unix.Clonefile(src, dst, unix.CLONE_NOFOLLOW)
}
