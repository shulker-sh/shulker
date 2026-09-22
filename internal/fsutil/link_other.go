//go:build !windows

package fsutil

import "os"

// LinkDir makes link a symlink to the directory target, which may be relative to link's folder.
func LinkDir(target, link string) error { return os.Symlink(target, link) }

// UnlinkDir removes the link itself, never what it points at.
func UnlinkDir(link string) error { return os.Remove(link) }
