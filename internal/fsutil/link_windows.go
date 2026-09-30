package fsutil

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

// LinkDir makes a directory junction rather than a symlink, because a junction needs neither
// administrator rights nor Developer Mode. A junction's target must be absolute, so a relative
// target is resolved against link's folder, and a moved target needs linking again. It writes the
// junction itself rather than through cmd's mklink, which fails on a path past 260 characters.
func LinkDir(target, link string) error {
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(link), target)
	}
	link, err := filepath.Abs(link)
	if err != nil {
		return err
	}
	target = filepath.Clean(target)
	if err := os.Mkdir(link, 0o755); err != nil {
		return err
	}
	if err := setJunction(link, target); err != nil {
		os.Remove(link)
		return fmt.Errorf("junction %s: %w", link, err)
	}
	return nil
}

func setJunction(link, target string) error {
	name, err := windows.UTF16PtrFromString(extendedPath(link))
	if err != nil {
		return err
	}
	h, err := windows.CreateFile(name, windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	buf := mountPointBuffer(`\??\`+target, target)
	var returned uint32
	return windows.DeviceIoControl(h, windows.FSCTL_SET_REPARSE_POINT, &buf[0], uint32(len(buf)), nil, 0, &returned, nil)
}

// mountPointBuffer is a REPARSE_DATA_BUFFER for a junction: the tag, the data's length, then the
// substitute and print names' offsets and lengths in bytes, and both names each ending in a NUL.
func mountPointBuffer(substitute, print string) []byte {
	sub, prn := utf16.Encode([]rune(substitute)), utf16.Encode([]rune(print))
	names := make([]uint16, 0, len(sub)+len(prn)+2)
	names = append(append(append(append(names, sub...), 0), prn...), 0)
	buf := make([]byte, 16+2*len(names))
	binary.LittleEndian.PutUint32(buf[0:], windows.IO_REPARSE_TAG_MOUNT_POINT)
	binary.LittleEndian.PutUint16(buf[4:], uint16(8+2*len(names)))
	binary.LittleEndian.PutUint16(buf[8:], 0)
	binary.LittleEndian.PutUint16(buf[10:], uint16(2*len(sub)))
	binary.LittleEndian.PutUint16(buf[12:], uint16(2*(len(sub)+1)))
	binary.LittleEndian.PutUint16(buf[14:], uint16(2*len(prn)))
	for i, c := range names {
		binary.LittleEndian.PutUint16(buf[16+2*i:], c)
	}
	return buf
}

// extendedPath lifts an absolute path past MAX_PATH for the Windows API, which, unlike package os,
// doesn't do it itself.
func extendedPath(path string) string {
	switch {
	case strings.HasPrefix(path, `\\?\`):
		return path
	case strings.HasPrefix(path, `\\`):
		return `\\?\UNC\` + path[2:]
	}
	return `\\?\` + path
}

// UnlinkDir removes the junction itself; RemoveDirectory never follows it into its target.
func UnlinkDir(link string) error { return os.Remove(link) }
