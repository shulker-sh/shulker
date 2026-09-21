//go:build !windows

package saves

import "os"

func linkDir(target, link string) error { return os.Symlink(target, link) }

func unlinkDir(link string) error { return os.Remove(link) }
