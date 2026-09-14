package launcher

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"shulker.sh/shulker/internal/fsutil"
)

const (
	GDLauncherInstanceFile = "instance.json"
	GDLauncherIconFile     = "icon.png"
	// GDLauncherGameDir is the folder inside a GDLauncher instance that Minecraft runs in.
	GDLauncherGameDir = "instance"
)

//go:embed assets/gdlauncher-icon.png
var GDLauncherIcon []byte

type GDLauncher struct {
	Dir string
}

type GDLauncherInstance struct {
	Name          string
	Minecraft     string
	LoaderType    string
	LoaderVersion string
	PreLaunch     string
}

var gdlauncherLoaderTypes = map[string]string{"fabric": "Fabric", "quilt": "Quilt", "neoforge": "Neoforge", "forge": "Forge"}

var gdlauncherReservedNames = []string{
	"con", "prn", "aux", "clock$", "nul",
	"com1", "com2", "com3", "com4", "com5", "com6", "com7", "com8", "com9",
	"lpt1", "lpt2", "lpt3", "lpt4", "lpt5", "lpt6", "lpt7", "lpt8", "lpt9",
}

// DefaultGDLauncherDir is GDLauncher's runtime path, which holds its instances: data/ in its app data
// folder, or the folder named in runtime_path_override there once the player moves it in Settings.
func DefaultGDLauncherDir() (string, error) {
	appData, err := gdlauncherAppData()
	if err != nil {
		return "", err
	}
	// GDLauncher uses the file's contents as the path without trimming them.
	if data, err := os.ReadFile(filepath.Join(appData, "runtime_path_override")); err == nil && len(data) > 0 {
		return string(data), nil
	}
	return filepath.Join(appData, "data"), nil
}

func gdlauncherAppData() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support", "gdlauncher_carbon"), nil
	case "windows":
		appdata := os.Getenv("APPDATA")
		if appdata == "" {
			return "", errors.New("APPDATA is not set")
		}
		return filepath.Join(appdata, "gdlauncher_carbon"), nil
	default:
		if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
			return filepath.Join(xdg, "gdlauncher_carbon"), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".local", "share", "gdlauncher_carbon"), nil
	}
}

func (l *GDLauncher) Check() error {
	info, err := os.Stat(l.Dir)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("%w at %s", ErrNotFound, l.Dir)
	}
	return nil
}

// GDLauncherFolder is the folder GDLauncher gives an instance with this name. Like GDLauncher, it checks
// the untrimmed name against Windows' reserved names.
func GDLauncherFolder(name string) string {
	folder := strings.TrimSpace(name)
	if slices.Contains(gdlauncherReservedNames, strings.ToLower(name)) {
		folder = "_" + folder
	}
	if strings.HasPrefix(folder, ".") || strings.HasPrefix(folder, "~") {
		folder = "_" + folder[1:]
	}
	if strings.HasSuffix(folder, ".") || strings.HasSuffix(folder, "~") {
		folder = folder[:len(folder)-1] + "_"
	}
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/:\<>*|"?^`, r) {
			return '_'
		}
		return r
	}, folder)
}

// GDLauncherLoaderVersion is the loader version as GDLauncher's meta names it. Forge builds are named
// <game>-<build> there, the way Forge's maven publishes them.
func GDLauncherLoaderVersion(minecraft, loaderType, version string) string {
	if loaderType == "forge" {
		return minecraft + "-" + version
	}
	return version
}

func (l *GDLauncher) InstanceDir(name string) string {
	return filepath.Join(l.Dir, "instances", GDLauncherFolder(name))
}

func (l *GDLauncher) GameDir(name string) string {
	return filepath.Join(l.InstanceDir(name), GDLauncherGameDir)
}

// WriteInstance writes the instance's instance.json, from which GDLauncher installs Minecraft, the loader
// and Java on first Play. A relink replaces the name, version and hook and keeps everything else, such as
// memory, Java arguments and playtime.
func (l *GDLauncher) WriteInstance(inst GDLauncherInstance) (InstanceResult, error) {
	dir := l.InstanceDir(inst.Name)
	res := InstanceResult{Dir: dir, GameDir: filepath.Join(dir, GDLauncherGameDir)}
	path := filepath.Join(dir, GDLauncherInstanceFile)
	top := map[string]json.RawMessage{}
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		res.Created = true
	case err != nil:
		return res, err
	default:
		if err := json.Unmarshal(data, &top); err != nil {
			return res, fmt.Errorf("%s: %w", path, err)
		}
	}
	config := map[string]json.RawMessage{}
	if raw, ok := top["game_configuration"]; ok {
		if err := json.Unmarshal(raw, &config); err != nil {
			return res, fmt.Errorf("%s: game_configuration: %w", path, err)
		}
	}
	modloaders := []map[string]string{}
	if loaderType, ok := gdlauncherLoaderTypes[inst.LoaderType]; ok {
		modloaders = append(modloaders, map[string]string{"type": loaderType, "version": GDLauncherLoaderVersion(inst.Minecraft, inst.LoaderType, inst.LoaderVersion)})
	}
	if config["version"], err = json.Marshal(map[string]any{"release": inst.Minecraft, "modloaders": modloaders}); err != nil {
		return res, err
	}
	if top["game_configuration"], err = json.Marshal(config); err != nil {
		return res, err
	}
	top["_version"] = jsonString("1")
	top["name"] = jsonString(inst.Name)
	if inst.PreLaunch != "" {
		top["pre_launch_hook"] = jsonString(inst.PreLaunch)
	}
	if err := os.MkdirAll(res.GameDir, 0o755); err != nil {
		return res, err
	}
	if res.Created {
		if err := fsutil.Write(filepath.Join(dir, GDLauncherIconFile), GDLauncherIcon); err != nil {
			return res, err
		}
		top["icon"] = jsonString(GDLauncherIconFile)
	}
	return res, fsutil.WriteJSON(path, top)
}

// GDLauncherPreLaunch reads an instance's pre-launch hook, and whether the instance exists at all.
func GDLauncherPreLaunch(instanceDir string) (hook string, found bool, err error) {
	path := filepath.Join(instanceDir, GDLauncherInstanceFile)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	var inst struct {
		Hook string `json:"pre_launch_hook"`
	}
	if err := json.Unmarshal(data, &inst); err != nil {
		return "", true, fmt.Errorf("%s: %w", path, err)
	}
	return inst.Hook, true, nil
}

// RemoveGDLauncherPreLaunch drops an instance's pre-launch hook when it is a shulker sync. It reports
// whether a hook was removed.
func RemoveGDLauncherPreLaunch(instanceDir string) (bool, error) {
	path := filepath.Join(instanceDir, GDLauncherInstanceFile)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	top := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &top); err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	var hook string
	if json.Unmarshal(top["pre_launch_hook"], &hook) != nil || !IsSyncCommand(hook) {
		return false, nil
	}
	delete(top, "pre_launch_hook")
	return true, fsutil.WriteJSON(path, top)
}

// GDLauncherHookArg quotes one argument of a GDLauncher hook. GDLauncher splits a hook POSIX-style with
// no shell, but on Windows it doubles every backslash first, so there the argument only gets quotes; a
// Windows path can't hold a double quote.
func GDLauncherHookArg(s string) string {
	return gdlauncherHookArg(s, runtime.GOOS)
}

func gdlauncherHookArg(s, goos string) string {
	if goos == "windows" {
		return `"` + s + `"`
	}
	return CommandArg(s)
}
