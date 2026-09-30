//go:build !darwin && !linux && !windows

package fsutil

import "errors"

func cloneFile(src, dst string) error {
	return errors.ErrUnsupported
}
