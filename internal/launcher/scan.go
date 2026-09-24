package launcher

import (
	"os"
	"path/filepath"

	"shulker.sh/shulker/internal/config"
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

// Scan looks where each launcher keeps its instances, or only the named one, and hands each game
// directory to recognise, which says whether shulker syncs it. dir replaces the launcher's own
// directory; for shulker's own instances that is the instances root. A shulker row records no
// launcher directory, the same as the row link writes.
func Scan(only, dir, instancesRoot string, recognise func(dir string) (config.Instance, bool)) []config.Instance {
	var found []config.Instance
	for _, e := range All {
		if only != "" && e.Name != only {
			continue
		}
		launcherDir := dir
		switch {
		case launcherDir != "":
		case !e.HasDir():
			launcherDir = instancesRoot
		case e.DefaultDir == nil:
			continue
		default:
			d, err := e.DefaultDir()
			if err != nil {
				continue
			}
			launcherDir = d
		}
		for _, gameDir := range e.GameDirs(launcherDir) {
			in, ok := recognise(gameDir)
			if !ok {
				continue
			}
			in.Launcher = e.Name
			if e.HasDir() {
				in.LauncherDir = launcherDir
			}
			if name := e.InstanceName(launcherDir, gameDir); name != "" {
				in.Name = name
			} else if e.IsInstanced {
				in.Name = filepath.Base(e.InstanceDir(gameDir))
			}
			found = append(found, in)
		}
	}
	return found
}
