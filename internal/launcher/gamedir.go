package launcher

import (
	"errors"
	"os"
)

// prepareGameDir makes gameDir a real directory for an instance to play out of. A symlink standing
// there is replaced: the instance is a project of its own now, and it has to be somewhere
// shulker can write a manifest, a lock and a history to.
func prepareGameDir(gameDir string) error {
	info, err := os.Lstat(gameDir)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return err
	case info.Mode()&os.ModeSymlink != 0:
		if err := os.Remove(gameDir); err != nil {
			return err
		}
	default:
		return nil
	}
	return os.Mkdir(gameDir, 0o755)
}
