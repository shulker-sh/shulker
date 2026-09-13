package launcher

import (
	"crypto/rand"
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
)

const ATLauncherInstanceFile = "instance.json"

type ATLauncher struct {
	Dir string
}

type ATLauncherInstance struct {
	Name          string
	Minecraft     string
	LoaderType    string
	LoaderVersion string
	PreLaunch     string
	// Version is the merged version JSON the launcher installs and starts the game from.
	Version json.RawMessage
}

var atlauncherLoaderTypes = map[string]string{"fabric": "Fabric", "quilt": "Quilt", "neoforge": "NeoForge", "forge": "Forge"}

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
			return "", errors.New("APPDATA is not set")
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

func (l *ATLauncher) Check() error {
	info, err := os.Stat(l.Dir)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("%w at %s", ErrNotFound, l.Dir)
	}
	return nil
}

var atlauncherUnsafeChars = regexp.MustCompile(`[^A-Za-z0-9]`)

// ATLauncherFolder is the folder ATLauncher keeps an instance in: its name with everything but ASCII
// letters and digits dropped. ATLauncher derives the folder from the name again when it saves, so the
// two must agree.
func ATLauncherFolder(name string) string {
	return atlauncherUnsafeChars.ReplaceAllString(name, "")
}

// InstanceDir is an instance's folder, which is also its game directory.
func (l *ATLauncher) InstanceDir(name string) string {
	return filepath.Join(l.Dir, "instances", ATLauncherFolder(name))
}

func (l *ATLauncher) LibrariesDir() string {
	return filepath.Join(l.Dir, "libraries")
}

// WriteInstance writes the instance's instance.json. A relink keeps the instance's uuid and every
// launcher setting the player chose, such as memory or Java arguments.
func (l *ATLauncher) WriteInstance(inst ATLauncherInstance) (InstanceResult, error) {
	dir := l.InstanceDir(inst.Name)
	res := InstanceResult{Dir: dir, GameDir: dir}
	path := filepath.Join(dir, ATLauncherInstanceFile)
	previous := map[string]json.RawMessage{}
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		res.Created = true
	case err != nil:
		return res, err
	default:
		if err := json.Unmarshal(data, &previous); err != nil {
			return res, fmt.Errorf("%s: %w", path, err)
		}
	}
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
	settings := map[string]json.RawMessage{}
	if raw, ok := previous["launcher"]; ok {
		if err := json.Unmarshal(raw, &settings); err != nil {
			return res, fmt.Errorf("%s: launcher: %w", path, err)
		}
	}
	loaderVersion, err := json.Marshal(map[string]any{
		"version":     inst.LoaderVersion,
		"rawVersion":  inst.LoaderVersion,
		"recommended": false,
		"type":        atlauncherLoaderTypes[inst.LoaderType],
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
	settings["loaderVersion"] = loaderVersion
	if inst.PreLaunch != "" {
		settings["enableCommands"] = json.RawMessage("true")
		settings["preLaunchCommand"] = jsonString(inst.PreLaunch)
	}
	if top["launcher"], err = json.Marshal(settings); err != nil {
		return res, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return res, err
	}
	return res, fsutil.WriteJSON(path, top)
}

// RemoveATLauncherPreLaunch drops an instance's pre-launch command when it is a shulker sync, and the
// per-instance switch that turned commands on for it. It reports whether a command was removed.
func RemoveATLauncherPreLaunch(instanceDir string) (bool, error) {
	path := filepath.Join(instanceDir, ATLauncherInstanceFile)
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
	settings := map[string]json.RawMessage{}
	if raw, ok := top["launcher"]; ok {
		if err := json.Unmarshal(raw, &settings); err != nil {
			return false, fmt.Errorf("%s: launcher: %w", path, err)
		}
	}
	var command string
	if json.Unmarshal(settings["preLaunchCommand"], &command) != nil || !IsSyncCommand(command) {
		return false, nil
	}
	delete(settings, "preLaunchCommand")
	delete(settings, "enableCommands")
	if top["launcher"], err = json.Marshal(settings); err != nil {
		return false, err
	}
	return true, fsutil.WriteJSON(path, top)
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
