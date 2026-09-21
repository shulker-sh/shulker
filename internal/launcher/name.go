package launcher

import (
	"encoding/json"
	"os"
	"path/filepath"

	"shulker.sh/shulker/internal/manifest"
)

// InstanceName is the name the launcher shows for an instance, read back from the file shulker wrote
// it into, or empty when there is none to read. An unreadable file reads as empty rather than
// failing, since whoever asks is about to name the instance around it.
func (e *Entry) InstanceName(launcherDir, gameDir string) string {
	if e == nil || e.name == nil {
		return ""
	}
	return e.name(e, launcherDir, gameDir)
}

func prismName(e *Entry, _, gameDir string) string {
	cfg, err := readINI(filepath.Join(e.InstanceDir(gameDir), PrismInstanceFile), prismUnescape)
	if err != nil {
		return ""
	}
	return cfg["name"]
}

func multimcName(e *Entry, _, gameDir string) string {
	cfg, err := readINI(filepath.Join(e.InstanceDir(gameDir), MultiMCInstanceFile), multimcUnescape)
	if err != nil {
		return ""
	}
	return cfg["name"]
}

func atlauncherName(e *Entry, _, gameDir string) string {
	var inst struct {
		Launcher struct {
			Name string `json:"name"`
		} `json:"launcher"`
	}
	readJSON(filepath.Join(e.InstanceDir(gameDir), ATLauncherInstanceFile), &inst)
	return inst.Launcher.Name
}

func gdlauncherName(e *Entry, _, gameDir string) string {
	var inst struct {
		Name string `json:"name"`
	}
	readJSON(filepath.Join(e.InstanceDir(gameDir), GDLauncherInstanceFile), &inst)
	return inst.Name
}

// mojangName reads the profile rather than the instance: the official launcher keeps no instance of
// its own, so the name lives beside the gameDir that points here.
func mojangName(_ *Entry, launcherDir, gameDir string) string {
	if launcherDir == "" {
		return ""
	}
	_, profiles, err := (&Mojang{Dir: launcherDir}).readProfiles()
	if err != nil {
		return ""
	}
	for _, key := range shulkerProfiles(profiles, gameDir) {
		var p struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(profiles[key], &p) == nil && p.Name != "" {
			return p.Name
		}
	}
	return ""
}

// shulkerName is the display name link recorded, since shulker is the launcher that shows it and a
// shulker instance is always a project building where it stands.
func shulkerName(_ *Entry, _, gameDir string) string {
	m, err := manifest.Load(filepath.Join(gameDir, manifest.FileName))
	if err != nil {
		return ""
	}
	return m.DisplayName("client")
}

func readJSON(path string, v any) {
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, v)
	}
}
