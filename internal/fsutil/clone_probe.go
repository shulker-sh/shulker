//go:build !windows

package fsutil

import (
	"os"
	"path/filepath"
)

func canClone(src, dir string) bool {
	tmp, err := tempName(filepath.Join(dir, "clone"))
	if err != nil {
		return false
	}
	err = cloneFile(src, tmp)
	os.Remove(tmp)
	return err == nil
}
