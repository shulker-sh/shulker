package launcher

import (
	"os"
	"path/filepath"

	"shulker.sh/shulker/internal/instance"
)

// shulkerDir is the tool-owned directory inside an instance.
const shulkerDir = instance.Dir

// GameDirs lists the game directories a launcher's data directory holds, for the scan that repairs
// the registry. A launcher whose instances shulker can't enumerate returns nothing. For shulker
// itself the directory is its instances root.
func (e *Entry) GameDirs(launcherDir string) []string {
	if e == nil || launcherDir == "" || e.gameDirs == nil {
		return nil
	}
	return e.gameDirs(e, launcherDir)
}

func prismGameDirs(e *Entry, launcherDir string) []string {
	l := &Prism{Dir: launcherDir, MultiMC: e.multimcINI}
	return gameDirsUnder(l.InstancesDir(), func(dir string) []string {
		return []string{filepath.Join(dir, "minecraft"), filepath.Join(dir, ".minecraft")}
	})
}

func atlauncherGameDirs(_ *Entry, launcherDir string) []string {
	return gameDirsUnder(filepath.Join(launcherDir, "instances"), func(dir string) []string {
		return []string{dir}
	})
}

func gdlauncherGameDirs(_ *Entry, launcherDir string) []string {
	return gameDirsUnder(filepath.Join(launcherDir, "instances"), func(dir string) []string {
		return []string{filepath.Join(dir, GDLauncherGameDir)}
	})
}

func instanceGameDirs(_ *Entry, launcherDir string) []string {
	return gameDirsUnder(launcherDir, func(dir string) []string {
		return []string{dir}
	})
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

// mojangGameDirs reads the game directories of the profiles shulker wrote, since the official
// launcher keeps no instances of its own and a profile can point anywhere.
func mojangGameDirs(_ *Entry, launcherDir string) []string {
	profiles, err := (&Mojang{Dir: launcherDir}).Profiles()
	if err != nil {
		return nil
	}
	var dirs []string
	for key, raw := range profiles {
		dir, ok := shulkerProfileGameDir(key, raw)
		if !ok {
			continue
		}
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}
