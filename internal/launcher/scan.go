package launcher

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"shulker.sh/shulker/internal/instance"
)

// shulkerDir is the tool-owned directory inside an instance.
const shulkerDir = instance.Dir

// GameDirs lists the game directories a launcher's data directory holds, for the scan that repairs
// the registry. A launcher whose instances shulker can't enumerate returns nothing.
func (e *Entry) GameDirs(launcherDir string) []string {
	if e == nil || launcherDir == "" {
		return nil
	}
	switch e.Name {
	case "prism", "multimc":
		l := &Prism{Dir: launcherDir, MultiMC: e.Name == "multimc"}
		return gameDirsUnder(l.InstancesDir(), func(dir string) []string {
			return []string{filepath.Join(dir, "minecraft"), filepath.Join(dir, ".minecraft")}
		})
	case "atlauncher":
		return gameDirsUnder(filepath.Join(launcherDir, "instances"), func(dir string) []string {
			return []string{dir}
		})
	case "gdlauncher":
		return gameDirsUnder(filepath.Join(launcherDir, "instances"), func(dir string) []string {
			return []string{filepath.Join(dir, GDLauncherGameDir)}
		})
	case "mojang":
		return mojangGameDirs(launcherDir)
	}
	return nil
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
func mojangGameDirs(launcherDir string) []string {
	profiles, err := (&Mojang{Dir: launcherDir}).Profiles()
	if err != nil {
		return nil
	}
	var dirs []string
	for key, raw := range profiles {
		if !strings.HasPrefix(key, "shulker-") {
			continue
		}
		var p struct {
			GameDir string `json:"gameDir"`
		}
		if json.Unmarshal(raw, &p) != nil || p.GameDir == "" {
			continue
		}
		if info, err := os.Stat(p.GameDir); err == nil && info.IsDir() {
			dirs = append(dirs, p.GameDir)
		}
	}
	return dirs
}
