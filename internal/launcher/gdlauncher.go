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
	"strconv"
	"strings"

	"shulker.sh/shulker/internal/fsutil"
)

const (
	GDLauncherInstanceFile = "instance.json"
	GDLauncherIconFile     = "icon.png"
	// GDLauncherGameDir is the folder inside a GDLauncher instance that Minecraft runs in.
	GDLauncherGameDir = "instance"
	// GDLauncherSetupDir marks an instance for first-time setup. GDLauncher runs Forge's and NeoForge's
	// install processors only while it exists, and removes it once the install finishes.
	GDLauncherSetupDir = ".setup"
)

// GDLauncherIcon is the instance icon.
//
//go:embed assets/gdlauncher-icon.png
var GDLauncherIcon []byte

type GDLauncher struct {
	Dir string
}

type GDLauncherInstance struct {
	Name       string
	Minecraft  string
	LoaderType string
	// LoaderVersion is named the way GDLauncher's meta names it; see GDLauncherLoaderVersion.
	LoaderVersion string
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

// GDLauncherRunning reports whether GDLauncher is open, and whether that can be told at all. Electron's
// single-instance lock leaves SingletonLock in GDLauncher's app data folder, a symlink to
// "<hostname>-<pid>"; Windows gets no such file. The hostname isn't compared, because Chromium and Go
// can name the same Mac differently.
func GDLauncherRunning() (running, detectable bool) {
	if runtime.GOOS == "windows" {
		return false, false
	}
	appData, err := gdlauncherAppData()
	if err != nil {
		return false, false
	}
	target, err := os.Readlink(filepath.Join(appData, "SingletonLock"))
	if err != nil {
		return false, true
	}
	i := strings.LastIndex(target, "-")
	if i < 0 {
		return false, true
	}
	pid, err := strconv.Atoi(target[i+1:])
	if err != nil {
		return false, true
	}
	return processAlive(pid), true
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
			return "", appDataUnset()
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

func (g *GDLauncher) Check() error {
	info, err := os.Stat(g.Dir)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("%w at %s", ErrNotFound, g.Dir)
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

func (g *GDLauncher) InstanceDir(name string) string {
	return filepath.Join(g.Dir, "instances", GDLauncherFolder(name))
}

func (g *GDLauncher) GameDir(name string) string {
	return filepath.Join(g.InstanceDir(name), GDLauncherGameDir)
}

// WriteInstance writes the instance's instance.json, from which GDLauncher installs Minecraft, the loader
// and Java on first Play. A relink replaces the name, version and hook and keeps everything else, such as
// memory, Java arguments and playtime.
func (g *GDLauncher) WriteInstance(inst GDLauncherInstance) (InstanceResult, error) {
	dir := g.InstanceDir(inst.Name)
	res := InstanceResult{Dir: dir, GameDir: filepath.Join(dir, GDLauncherGameDir)}
	path := filepath.Join(dir, GDLauncherInstanceFile)
	top, found, err := readJSONObject(path)
	if err != nil {
		return res, err
	}
	res.Created = !found
	config, err := jsonObjectAt(path, top, "game_configuration")
	if err != nil {
		return res, err
	}
	modloaders := []map[string]string{}
	if loaderType, ok := gdlauncherLoaderTypes[inst.LoaderType]; ok {
		modloaders = append(modloaders, map[string]string{"type": loaderType, "version": inst.LoaderVersion})
	}
	if config["version"], err = json.Marshal(map[string]any{"release": inst.Minecraft, "modloaders": modloaders}); err != nil {
		return res, err
	}
	if top["game_configuration"], err = json.Marshal(config); err != nil {
		return res, err
	}
	top["_version"] = jsonString("1")
	top["name"] = jsonString(inst.Name)
	if err := os.MkdirAll(res.GameDir, 0o755); err != nil {
		return res, err
	}
	if err := os.MkdirAll(filepath.Join(dir, GDLauncherSetupDir), 0o755); err != nil {
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
		return "", true, invalidFile(path, err)
	}
	return inst.Hook, true, nil
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
