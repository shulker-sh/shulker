package fsutil

import (
	"os"

	"golang.org/x/sys/unix"
)

// cloneFile makes dst, which must not exist, a copy-on-write clone of src. Btrfs and XFS with
// reflink=1 clone; ext4 and the rest refuse.
func cloneFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	err = unix.IoctlFileClone(int(out.Fd()), int(in.Fd()))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(dst)
	}
	return err
}
