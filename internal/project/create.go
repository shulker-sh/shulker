package project

import (
	"errors"
	"os"
	"path/filepath"
)

// Scaffold gives an empty project folder what every project has: an overrides folder and a
// .gitignore for what a build makes in place.
func Scaffold(dir string) error {
	if err := os.MkdirAll(filepath.Join(dir, "overrides"), 0o755); err != nil {
		return err
	}
	gi := filepath.Join(dir, ".gitignore")
	if _, err := os.Stat(gi); errors.Is(err, os.ErrNotExist) {
		return fsutil.Write(gi, []byte("/build/\n/data/\n/downloads/\n/shulker.local.json\n/.shulker/\n"))
	}
	return nil
}
