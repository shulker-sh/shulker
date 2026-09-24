package launcher

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/mavenver"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/proc"
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

// GDLauncherMetaURL is GDLauncher's own meta, the only place it installs loaders from.
const GDLauncherMetaURL = "https://meta.gdl.gg"

// gdlauncherAnyGame is the game id GDLauncher's meta lists Fabric and Quilt loaders under, since one
// loader build runs on every game version.
const gdlauncherAnyGame = "${gdlauncher.gameVersion}"

// gdlauncherEntry is GDLauncher: an instance folder named the way it names one, holding the game
// directory as instance/, which installs the game, the loader and Java on first Play.
var gdlauncherEntry = &Entry{
	Name: "gdlauncher", Title: "GDLauncher", IsInstanced: true, DefaultDir: DefaultGDLauncherDir, NamesFolder: true,
	Slot:    &Slot{Deadline: "4m", Quote: gdlauncherHookArg},
	Image:   &Image{File: GDLauncherIconFile, Default: GDLauncherIcon},
	MetaURL: GDLauncherMetaURL,
	Usage: Usage{
		Short: "Create a GDLauncher instance that syncs the client build before each launch",
		Noun:  "instance",
		Dir:   "launcher runtime directory (default: GDLauncher's)",
		Names: true,
		Force: "repoint the modpack an instance already follows, link over one shulker didn't link, and use the locked loader version even if GDLauncher can't install it yet",
	},
	relink: relinkLauncher, forget: forgetInstance, name: gdlauncherName, gameDirs: gdlauncherGameDirs,
	readSlots: readGDLauncherSlots, writeSlots: writeGDLauncherSlots,
	locate: filepath.EvalSymlinks, running: GDLauncherRunning,
	place: placeGDLauncher, link: linkGDLauncher, after: gdlauncherAfter,
}

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
	return proc.IsAlive(pid), true
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

func gdlauncherName(e *Entry, _, gameDir string) string {
	var inst struct {
		Name string `json:"name"`
	}
	readJSON(filepath.Join(e.InstanceDir(gameDir), GDLauncherInstanceFile), &inst)
	return inst.Name
}

func gdlauncherGameDirs(_ *Entry, launcherDir string) []string {
	return gameDirsUnder(filepath.Join(launcherDir, "instances"), func(dir string) []string {
		return []string{filepath.Join(dir, GDLauncherGameDir)}
	})
}

func placeGDLauncher(_ *Entry, req *Link) (Placement, error) {
	if GDLauncherFolder(req.Name) == "" {
		return Placement{}, blankName("GDLauncher needs an instance name that isn't blank")
	}
	g := &GDLauncher{Dir: req.LauncherDir}
	return Placement{ID: req.ID, Dir: g.InstanceDir(req.Name), GameDir: g.GameDir(req.Name)}, nil
}

// linkGDLauncher writes the instance GDLauncher installs from, asking for a loader version it can
// install. GDLauncher is warned about rather than waited for: it reads its instances when it starts
// and writes its own copy back over them while open.
func linkGDLauncher(ctx context.Context, _ *Entry, req *Link, _ Placement) (InstanceResult, error) {
	loaderVersion, err := gdlauncherLoaderVersion(ctx, req)
	if err != nil {
		return InstanceResult{}, err
	}
	if running, _ := GDLauncherRunning(); running {
		req.Warn("GDLauncher is open; it may overwrite this instance's changes. Quit it and run this link again")
	}
	g := &GDLauncher{Dir: req.LauncherDir}
	return g.WriteInstance(GDLauncherInstance{
		Name:          req.Name,
		Minecraft:     req.Minecraft,
		LoaderType:    req.LoaderType,
		LoaderVersion: loaderVersion,
	})
}

// gdlauncherAfter keeps the restart reminder for where an open GDLauncher can't be detected.
func gdlauncherAfter(e *Entry, _ InstanceResult) string {
	if _, detectable := GDLauncherRunning(); detectable {
		return ""
	}
	return "restart " + e.Title + " if it is open so the instance shows up"
}

// gdlauncherLoaderVersion is the loader version the instance asks GDLauncher for. GDLauncher installs
// loaders only from its own meta, which lags behind new releases, so a locked version it doesn't list
// yet gives way to the newest one it has, unless force.
func gdlauncherLoaderVersion(ctx context.Context, req *Link) (string, error) {
	if req.LoaderType == "" {
		return "", nil
	}
	want := GDLauncherLoaderVersion(req.Minecraft, req.LoaderType, req.LoaderVersion)
	req.Log("checking which %s versions GDLauncher can install", req.LoaderType)
	meta := &GDLauncherMeta{Client: req.Fetch, BaseURL: req.MetaURL}
	listed, err := meta.LoaderVersions(ctx, req.LoaderType, req.Minecraft)
	switch {
	case err != nil:
		req.Warn("couldn't check whether GDLauncher can install %s %s (%v); the instance asks for it anyway", req.LoaderType, want, err)
		return want, nil
	case slices.Contains(listed, want):
		return want, nil
	case len(listed) == 0:
		req.Warn("GDLauncher can't install %s for Minecraft %s yet, so the instance won't start until it can", req.LoaderType, req.Minecraft)
		return want, nil
	}
	newest := slices.MaxFunc(listed, func(x, y string) int {
		return mavenver.Compare(mavenver.Parse(x), mavenver.Parse(y))
	})
	if req.Force {
		req.Warn("GDLauncher can't install %s %s yet, so the instance won't start until it can; without --force it would use %s", req.LoaderType, want, newest)
		return want, nil
	}
	req.Warn("GDLauncher can't install %s %s yet, so the instance uses %s, the newest it has; run this link again once GDLauncher adds %s, or pass --force to use it anyway", req.LoaderType, want, newest, want)
	return newest, nil
}

// GDLauncherMeta reads GDLauncher's own meta.
type GDLauncherMeta struct {
	Client  *fetch.Client
	BaseURL string
}

// LoaderVersions lists the versions of a loader GDLauncher can install for a game, named the way its
// meta names them.
func (g *GDLauncherMeta) LoaderVersions(ctx context.Context, loader, game string) ([]string, error) {
	var manifest struct {
		GameVersions []struct {
			ID      string `json:"id"`
			Loaders []struct {
				ID string `json:"id"`
			} `json:"loaders"`
		} `json:"gameVersions"`
	}
	if err := g.Client.GetJSON(ctx, g.BaseURL+"/"+loader+"/v2/manifest.json", &manifest); err != nil {
		e := out.Errorf("meta-fetch", "couldn't read GDLauncher's %s versions", loader)
		e.WithCause("gdlauncher", err)
		if fetch.IsNetwork(err) {
			return nil, fetch.Unreachable(e)
		}
		return nil, e
	}
	var versions []string
	for _, gv := range manifest.GameVersions {
		if gv.ID != game && gv.ID != gdlauncherAnyGame {
			continue
		}
		for _, l := range gv.Loaders {
			versions = append(versions, l.ID)
		}
	}
	return versions, nil
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

// GDLauncherLoaderVersion is the loader version as GDLauncher's meta names it: the version the
// loader's own Maven publishes it under, which for Forge carries the game version.
func GDLauncherLoaderVersion(minecraft, loaderType, version string) string {
	l, _ := loader.Lookup(loaderType)
	return l.ArtifactVersion(minecraft, version)
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
	res := InstanceResult{Dir: dir, GameDir: filepath.Join(dir, GDLauncherGameDir), Key: GDLauncherFolder(inst.Name)}
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
	if l, ok := loader.Lookup(inst.LoaderType); ok {
		modloaders = append(modloaders, map[string]string{"type": l.GDLauncherType, "version": inst.LoaderVersion})
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
