package launcher

import (
	"os"
	"path/filepath"
)

// GameDirs lists the game directories a launcher's data directory holds, for the scan that repairs
// the registry. A launcher whose instances shulker can't enumerate returns nothing. For shulker
// itself the directory is its instances root.
func (e *Entry) GameDirs(launcherDir string) []string {
	if e == nil || launcherDir == "" || e.gameDirs == nil {
		return nil
	}
	return e.gameDirs(e, launcherDir)
}

func gameDirsUnder(instancesDir string, candidates func(dir string) []string) []string {
	items, err := os.ReadDir(instancesDir)
	if err != nil {
		return nil
	}
	var dirs []string
	for _, item := range items {
		if !item.IsDir() {
			continue
		}
		for _, dir := range candidates(filepath.Join(instancesDir, item.Name())) {
			if info, err := os.Stat(dir); err == nil && info.IsDir() {
				dirs = append(dirs, dir)
				break
			}
		}
	}
	return dirs
}
