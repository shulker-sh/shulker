package saves

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// InUse reports whether a running game has world open. The game holds an OS lock on the world's
// session.lock for as long as it has it open, and the OS drops that lock with the process, so a
// file a crash left behind, or none at all, is not in use.
func InUse(world string) (bool, error) {
	f, err := os.Open(filepath.Join(world, "session.lock"))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer f.Close()
	return locked(f)
}

// OpenWorlds is those of worlds in dir that a running game has open.
func OpenWorlds(dir string, worlds []string) ([]string, error) {
	var open []string
	for _, w := range worlds {
		held, err := InUse(filepath.Join(dir, w))
		if err != nil {
			return nil, err
		}
		if held {
			open = append(open, w)
		}
	}
	return open, nil
}
