package launcher

import (
	"errors"
	"os"

	"shulker.sh/shulker/internal/fsutil"
)

// prepareGameDir makes gameDir a real directory for an instance to play out of. A symlink or
// junction standing there is replaced: the instance is a project of its own now, and it has to be
// somewhere shulker can write a manifest, a lock and a history to.
func prepareGameDir(gameDir string) error {
	_, linked := fsutil.ReadLink(gameDir)
	_, err := os.Lstat(gameDir)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return err
	case linked:
		if err := os.Remove(gameDir); err != nil {
			return err
		}
	default:
		return nil
	}
	return os.Mkdir(gameDir, 0o755)
}
