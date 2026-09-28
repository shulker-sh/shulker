package launcher

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/loader"
)

const (
	ATLauncherInstanceFile = "instance.json"
	ATLauncherImageFile    = "instance.png"
)

// ATLauncherImage is the instance card image. ATLauncher stretches a non-square image to its
// 300×150 card, so the logo is pre-placed on a canvas of that size to stay sharp.
//
//go:embed assets/atlauncher-instance.png
var ATLauncherImage []byte

// atlauncherEntry is ATLauncher: an instance folder named after the letters and digits in the
// instance name, which is the game directory too, started from a complete version JSON.
var atlauncherEntry = &Entry{
	Name: "atlauncher", Title: "ATLauncher", IsInstanced: true, DefaultDir: DefaultATLauncherDir, gameDirIsInstance: true, NamesFolder: true,
	Slot:  &Slot{Token: "$INST_DIR", Tokens: instTokens, Unreproducible: []string{"INST_JAVA", "INST_JAVA_ARGS"}, Quote: bareWord},
	Image: &Image{File: ATLauncherImageFile, Default: ATLauncherImage, fit: atlauncherCard},
	Usage: Usage{
		Short: "Create an ATLauncher instance that syncs the client build before each launch",
		Noun:  "instance",
		Dir:   "launcher data directory (default: ATLauncher's)",
		Names: true,
		Force: "repoint the modpack an instance already follows, or link over one shulker didn't link",
	},
	relink: relinkLauncher, forget: forgetInstance, name: atlauncherName, gameDirs: atlauncherGameDirs,
	readSlots: readATLauncherSlots, writeSlots: writeATLauncherSlots, slotFile: instanceFileIn(ATLauncherInstanceFile),
	place: placeATLauncher, link: linkATLauncher,
	Accounts: atlauncherAccounts,
	after: func(e *Entry, _ InstanceResult) string {
		return "restart " + e.Title + " if it is open so the instance shows up"
	},
}

type ATLauncher struct {
	Dir string
}

type ATLauncherInstance struct {
	Name          string
	Minecraft     string
	LoaderType    string
	LoaderVersion string
	// Version is the merged version JSON the launcher installs and starts the game from.
	Version json.RawMessage
}

// DefaultATLauncherDir is where ATLauncher keeps its data. It runs from its working directory, which
// the macOS app bundle sets to Contents/Java inside the app.
func DefaultATLauncherDir() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		candidates := []string{"/Applications/ATLauncher.app/Contents/Java", filepath.Join(home, "Applications", "ATLauncher.app", "Contents", "Java")}
		for _, dir := range candidates {
			if _, err := os.Stat(dir); err == nil {
				return dir, nil
			}
		}
		return candidates[0], nil
	case "windows":
		appdata := os.Getenv("APPDATA")
		if appdata == "" {
			return "", appDataUnset()
		}
		return filepath.Join(appdata, "ATLauncher"), nil
	default:
		if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
			return filepath.Join(xdg, "atlauncher"), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".local", "share", "atlauncher"), nil
	}
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

func atlauncherGameDirs(_ *Entry, launcherDir string) []string {
	return gameDirsUnder(filepath.Join(launcherDir, "instances"), func(dir string) []string {
		return []string{dir}
	})
}

func placeATLauncher(_ *Entry, req *Link) (Placement, error) {
	if ATLauncherFolder(req.Name) == "" {
		return Placement{}, blankName(fmt.Sprintf("ATLauncher names an instance's folder after the letters and digits in its name, and %q has none", req.Name))
	}
	dir := (&ATLauncher{Dir: req.LauncherDir}).InstanceDir(req.Name)
	return Placement{ID: req.ID, Dir: dir, GameDir: dir}, nil
}

func linkATLauncher(ctx context.Context, _ *Entry, req *Link, _ Placement) (InstanceResult, error) {
	atl := &ATLauncher{Dir: req.LauncherDir}
	version, err := atlauncherVersion(ctx, req, atl)
	if err != nil {
		return InstanceResult{}, err
	}
	return atl.WriteInstance(ATLauncherInstance{
		Name:          req.Name,
		Minecraft:     req.Minecraft,
		LoaderType:    req.LoaderType,
		LoaderVersion: req.LoaderVersion,
		Version:       version,
	})
}

// atlauncherVersion is the version JSON an ATLauncher instance starts the game from. NeoForge and
// Forge build part of the client with their installer, so it runs once per loader version into a
// scratch launcher directory in the cache, and what it put in libraries/ is copied into ATLauncher's.
func atlauncherVersion(ctx context.Context, req *Link, atl *ATLauncher) (json.RawMessage, error) {
	req.Log("fetching Minecraft %s", req.Minecraft)
	vanilla, err := req.Versions.Vanilla(ctx)
	if err != nil {
		return nil, err
	}
	if req.LoaderType == "" {
		return vanilla, nil
	}
	if !req.Versions.HasInstaller() {
		req.Log("fetching %s loader %s for %s", loader.Title(req.LoaderType), req.LoaderVersion, req.Minecraft)
		loaderVersion, err := req.Versions.LoaderProfile(ctx)
		if err != nil {
			return nil, err
		}
		return MergeVersion(vanilla, loaderVersion)
	}
	scratch := req.Cache.ATLauncherInstall(req.LoaderType, req.LoaderVersion)
	installed := filepath.Join(scratch, ".installed")
	if _, err := os.Stat(installed); err != nil {
		if err := os.MkdirAll(scratch, 0o755); err != nil {
			return nil, err
		}
		if _, err := req.Versions.InstallClient(ctx, scratch); err != nil {
			return nil, err
		}
		if err := fsutil.Write(installed, nil); err != nil {
			return nil, err
		}
	}
	loaderVersion, err := req.Versions.InstallerVersion(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := CopyLibraries(filepath.Join(scratch, "libraries"), atl.LibrariesDir()); err != nil {
		return nil, err
	}
	return MergeVersion(vanilla, loaderVersion)
}

var atlauncherUnsafeChars = regexp.MustCompile(`[^A-Za-z0-9]`)

// ATLauncherFolder is the folder ATLauncher keeps an instance in: its name with everything but ASCII
// letters and digits dropped. ATLauncher derives the folder from the name again when it saves, so the
// two must agree.
func ATLauncherFolder(name string) string {
	return atlauncherUnsafeChars.ReplaceAllString(name, "")
}

// InstanceDir is an instance's folder, which is also its game directory.
func (a *ATLauncher) InstanceDir(name string) string {
	return filepath.Join(a.Dir, "instances", ATLauncherFolder(name))
}

func (a *ATLauncher) LibrariesDir() string {
	return filepath.Join(a.Dir, "libraries")
}

// WriteInstance writes the instance's instance.json. A relink keeps the instance's uuid and every
// launcher setting the player chose, such as memory or Java arguments.
func (a *ATLauncher) WriteInstance(inst ATLauncherInstance) (InstanceResult, error) {
	dir := a.InstanceDir(inst.Name)
	res := InstanceResult{Dir: dir, GameDir: dir, Key: ATLauncherFolder(inst.Name)}
	path := filepath.Join(dir, ATLauncherInstanceFile)
	previous, found, err := readJSONObject(path)
	if err != nil {
		return res, err
	}
	res.Created = !found
	top := map[string]json.RawMessage{}
	if err := json.Unmarshal(inst.Version, &top); err != nil {
		return res, fmt.Errorf("version json: %w", err)
	}
	if uuid, ok := previous["uuid"]; ok {
		top["uuid"] = uuid
	} else {
		id, err := newUUID()
		if err != nil {
			return res, err
		}
		top["uuid"] = jsonString(id)
	}
	settings, err := jsonObjectAt(path, previous, "launcher")
	if err != nil {
		return res, err
	}
	l, _ := loader.Lookup(inst.LoaderType)
	loaderVersion, err := json.Marshal(map[string]any{
		"version":     inst.LoaderVersion,
		"rawVersion":  inst.LoaderVersion,
		"recommended": false,
		"type":        l.Title,
	})
	if err != nil {
		return res, err
	}
	settings["name"] = jsonString(inst.Name)
	settings["pack"] = jsonString("Minecraft")
	settings["version"] = jsonString(inst.Minecraft)
	settings["vanillaInstance"] = json.RawMessage("true")
	settings["isPlayable"] = json.RawMessage("true")
	// ATLauncher unboxes both when Play is pressed, so a missing one throws before launch.
	settings["requiredMemory"] = json.RawMessage("0")
	settings["requiredPermGen"] = json.RawMessage("0")
	if inst.LoaderType != "" {
		settings["loaderVersion"] = loaderVersion
	} else {
		delete(settings, "loaderVersion")
	}
	if top["launcher"], err = json.Marshal(settings); err != nil {
		return res, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return res, err
	}
	image := filepath.Join(dir, ATLauncherImageFile)
	if _, err := os.Stat(image); errors.Is(err, os.ErrNotExist) {
		if err := fsutil.Write(image, ATLauncherImage); err != nil {
			return res, err
		}
	} else if err != nil {
		return res, err
	}
	return res, fsutil.WriteJSON(path, top)
}

// ATLauncherPreLaunch reads an instance's pre-launch command, and whether the instance exists at all.
func ATLauncherPreLaunch(instanceDir string) (command string, found bool, err error) {
	path := filepath.Join(instanceDir, ATLauncherInstanceFile)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	var inst struct {
		Launcher struct {
			PreLaunchCommand string `json:"preLaunchCommand"`
		} `json:"launcher"`
	}
	if err := json.Unmarshal(data, &inst); err != nil {
		return "", true, invalidFile(path, err)
	}
	return inst.Launcher.PreLaunchCommand, true, nil
}

// MergeVersion lays a loader's version JSON over the vanilla one the way ATLauncher's own installer
// does. The id stays vanilla's, because ATLauncher finds the client jar and fills ${version_name}
// from it; the loader's libraries go before vanilla's and its arguments after. ATLauncher reads a
// library only through downloads.artifact, so maven-style entries are converted.
func MergeVersion(vanilla, loaderVersion json.RawMessage) (json.RawMessage, error) {
	var base, over map[string]json.RawMessage
	if err := json.Unmarshal(vanilla, &base); err != nil {
		return nil, fmt.Errorf("vanilla version json: %w", err)
	}
	if err := json.Unmarshal(loaderVersion, &over); err != nil {
		return nil, fmt.Errorf("loader version json: %w", err)
	}
	delete(base, "inheritsFrom")
	if mainClass, ok := over["mainClass"]; ok {
		base["mainClass"] = mainClass
	}
	var libraries []json.RawMessage
	for _, version := range []map[string]json.RawMessage{over, base} {
		var libs []json.RawMessage
		if raw, ok := version["libraries"]; ok {
			if err := json.Unmarshal(raw, &libs); err != nil {
				return nil, fmt.Errorf("libraries: %w", err)
			}
		}
		for _, lib := range libs {
			converted, err := artifactLibrary(lib)
			if err != nil {
				return nil, err
			}
			libraries = append(libraries, converted)
		}
	}
	var err error
	if base["libraries"], err = json.Marshal(libraries); err != nil {
		return nil, err
	}
	type arguments struct {
		Game []json.RawMessage `json:"game"`
		JVM  []json.RawMessage `json:"jvm"`
	}
	var args, loaderArgs arguments
	if raw, ok := base["arguments"]; ok {
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, fmt.Errorf("vanilla arguments: %w", err)
		}
	}
	if raw, ok := over["arguments"]; ok {
		if err := json.Unmarshal(raw, &loaderArgs); err != nil {
			return nil, fmt.Errorf("loader arguments: %w", err)
		}
	}
	args.Game = append(append([]json.RawMessage{}, args.Game...), loaderArgs.Game...)
	args.JVM = append(append([]json.RawMessage{}, args.JVM...), loaderArgs.JVM...)
	if _, had := base["arguments"]; had || len(args.Game)+len(args.JVM) > 0 {
		if base["arguments"], err = json.Marshal(args); err != nil {
			return nil, err
		}
	}
	return json.Marshal(base)
}

func artifactLibrary(raw json.RawMessage) (json.RawMessage, error) {
	lib := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &lib); err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}
	if _, ok := lib["downloads"]; ok {
		return raw, nil
	}
	var head struct {
		Name string `json:"name"`
		URL  string `json:"url"`
		Sha1 string `json:"sha1"`
		Size *int64 `json:"size"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}
	path, err := mavenPath(head.Name)
	if err != nil {
		return nil, err
	}
	base := head.URL
	if base == "" {
		base = "https://libraries.minecraft.net/"
	}
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	artifact := map[string]any{"path": path, "url": base + path}
	if head.Sha1 != "" {
		artifact["sha1"] = head.Sha1
	}
	if head.Size != nil {
		artifact["size"] = *head.Size
	}
	if lib["downloads"], err = json.Marshal(map[string]any{"artifact": artifact}); err != nil {
		return nil, err
	}
	delete(lib, "url")
	return json.Marshal(lib)
}

// mavenPath is where a library's coordinates put it in a maven repository:
// group:artifact:version[:classifier][@extension].
func mavenPath(coords string) (string, error) {
	name, ext := coords, "jar"
	if i := strings.LastIndex(name, "@"); i >= 0 {
		name, ext = name[:i], name[i+1:]
	}
	parts := strings.Split(name, ":")
	if len(parts) < 3 || len(parts) > 4 {
		return "", fmt.Errorf("library %q is not group:artifact:version", coords)
	}
	file := parts[1] + "-" + parts[2]
	if len(parts) == 4 {
		file += "-" + parts[3]
	}
	return strings.ReplaceAll(parts[0], ".", "/") + "/" + parts[1] + "/" + parts[2] + "/" + file + "." + ext, nil
}

// CopyLibraries copies a loader installer's libraries into the launcher's, skipping files already
// there at the same size. It returns how many it copied.
func CopyLibraries(from, to string) (int, error) {
	if _, err := os.Stat(from); errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	copied := 0
	err := filepath.WalkDir(from, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		dst := filepath.Join(to, rel)
		if existing, err := os.Stat(dst); err == nil && existing.Size() == info.Size() {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		if err := fsutil.WriteFrom(dst, f); err != nil {
			return err
		}
		copied++
		return nil
	})
	return copied, err
}

func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
