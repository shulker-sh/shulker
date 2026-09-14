package launcher

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"shulker.sh/shulker/internal/config"
)

const (
	RemovedPreLaunch = "pre-launch command"
	RemovedProfile   = "launcher profile"
)

// Forgotten is what unlinking an entry did: what it took away, if anything, and
// the sentence describing it.
type Forgotten struct {
	Removed string
	Summary string
}

type Entry struct {
	Name  string
	Title string
	// Instanced launchers keep the game directory inside an instance directory
	// they own, so the instance going away is the entry going away.
	Instanced  bool
	DefaultDir func() (string, error)
	// gameDirIsInstance marks launchers whose instance folder is the game directory itself,
	// rather than holding it as minecraft/.
	gameDirIsInstance bool
	relink            func(e *Entry, l config.Link) (args []string, in string)
	forget            func(e *Entry, l config.Link) (Forgotten, error)
}

var All = []*Entry{
	{Name: "prism", Title: "Prism Launcher", Instanced: true, DefaultDir: DefaultPrismDir, relink: relinkInstance, forget: forgetInstance},
	{Name: "multimc", Title: "MultiMC", Instanced: true, relink: relinkInstance, forget: forgetInstance},
	{Name: "mojang", Title: "Minecraft Launcher", DefaultDir: DefaultMojangDir, relink: relinkMojang, forget: forgetMojang},
	{Name: "atlauncher", Title: "ATLauncher", Instanced: true, DefaultDir: DefaultATLauncherDir, gameDirIsInstance: true, relink: relinkMojang, forget: forgetInstance},
	{Name: "gdlauncher", Title: "GDLauncher", Instanced: true, DefaultDir: DefaultGDLauncherDir, relink: relinkMojang, forget: forgetInstance},
}

// InstanceDir is the instance folder that holds an instanced launcher's game directory.
func (e *Entry) InstanceDir(gameDir string) string {
	if e.gameDirIsInstance {
		return gameDir
	}
	return filepath.Dir(gameDir)
}

func Find(name string) *Entry {
	for _, e := range All {
		if e.Name == name {
			return e
		}
	}
	return nil
}

func Names() []string {
	names := make([]string, len(All))
	for i, e := range All {
		names[i] = e.Name
	}
	return names
}

// NameList reads the launcher names as prose: "prism, multimc, or mojang".
func NameList() string {
	names := Names()
	switch len(names) {
	case 0, 1:
		return strings.Join(names, "")
	case 2:
		return names[0] + " or " + names[1]
	}
	return strings.Join(names[:len(names)-1], ", ") + ", or " + names[len(names)-1]
}

// Title names a launcher for a heading or a sentence. Entries with no launcher
// are plain directories; a launcher this build doesn't know keeps its own name.
func Title(name string) string {
	if name == "" {
		return "Other directories"
	}
	if e := Find(name); e != nil {
		return e.Title
	}
	return name
}

// Rank orders entries for display: known launchers in table order, then
// unknown ones, then plain directories.
func Rank(name string) int {
	if name == "" {
		return len(All) + 1
	}
	if i := slices.IndexFunc(All, func(e *Entry) bool { return e.Name == name }); i >= 0 {
		return i
	}
	return len(All)
}

func (e *Entry) defaultDir() string {
	if e == nil || e.DefaultDir == nil {
		return ""
	}
	dir, _ := e.DefaultDir()
	return dir
}

// Relink is the command that recreates an entry, and the directory to run it in
// when the command needs one.
func Relink(l config.Link) (command, in string) {
	e := Find(l.Launcher)
	if e == nil {
		args, in := relinkSync(l)
		return strings.Join(args, " "), in
	}
	args, in := e.relink(e, l)
	if l.LauncherDir != "" && l.LauncherDir != e.defaultDir() {
		args = append(args, "--launcher-dir", shellArg(l.LauncherDir))
	}
	return strings.Join(args, " "), in
}

// Forget stops a launcher from syncing an entry, without touching its files.
func Forget(l config.Link) (Forgotten, error) {
	e := Find(l.Launcher)
	if e == nil {
		return Forgotten{Summary: fmt.Sprintf("Forgot %q (%s); its files stay.", l.Name, l.Dir)}, nil
	}
	return e.forget(e, l)
}

func relinkSync(l config.Link) (args []string, in string) {
	args = []string{"shulker", "sync", shellArg(l.Source)}
	if l.Ref != "" {
		args = append(args, "--ref", shellArg(l.Ref))
	}
	return append(args, "--target", shellArg(l.Target), "--into", shellArg(l.Dir), "--name", shellArg(l.Name)), ""
}

func relinkInstance(e *Entry, l config.Link) (args []string, in string) {
	args = []string{"shulker", "link", e.Name}
	if info, err := os.Lstat(l.Dir); err == nil && info.Mode()&os.ModeSymlink != 0 {
		in = l.Source
		args = append(args, "--mode", "symlink")
	} else {
		args = append(args, shellArg(l.Source))
		if l.Ref != "" {
			args = append(args, "--ref", shellArg(l.Ref))
		}
	}
	return append(args, "--target", shellArg(l.Target), "--name", shellArg(l.Name)), in
}

func relinkMojang(e *Entry, l config.Link) (args []string, in string) {
	args = []string{"shulker", "link", e.Name, shellArg(l.Source)}
	if l.Ref != "" {
		args = append(args, "--ref", shellArg(l.Ref))
	}
	return append(args, "--target", shellArg(l.Target), "--name", shellArg(l.Name)), ""
}

func forgetInstance(e *Entry, l config.Link) (Forgotten, error) {
	instanceDir := e.InstanceDir(l.Dir)
	_, statErr := os.Stat(instanceDir)
	switch info, err := os.Lstat(l.Dir); {
	case errors.Is(statErr, os.ErrNotExist):
		return Forgotten{Summary: fmt.Sprintf("Unlinked %q (%s); its instance was already gone.", l.Name, e.Title)}, nil
	case err == nil && info.Mode()&os.ModeSymlink != 0:
		return Forgotten{Summary: fmt.Sprintf("Unlinked %q (%s); the instance stays and still uses the build directory.", l.Name, e.Title)}, nil
	}
	var removed bool
	var err error
	switch e.Name {
	case "atlauncher":
		removed, err = RemoveATLauncherPreLaunch(instanceDir)
	case "gdlauncher":
		removed, err = RemoveGDLauncherPreLaunch(instanceDir)
	default:
		removed, err = RemovePreLaunch(instanceDir, e.Name == "multimc")
	}
	if err != nil {
		return Forgotten{}, err
	}
	if !removed {
		return Forgotten{Summary: fmt.Sprintf("Unlinked %q (%s); its pre-launch command isn't a shulker sync, so it was kept.", l.Name, e.Title)}, nil
	}
	return Forgotten{
		Removed: RemovedPreLaunch,
		Summary: fmt.Sprintf("Unlinked %q (%s): removed its pre-launch sync; the instance and its worlds stay.\nRestart the launcher if it is open so the change is picked up.", l.Name, e.Title),
	}, nil
}

func forgetMojang(e *Entry, l config.Link) (Forgotten, error) {
	n, err := (&Mojang{Dir: l.LauncherDir}).RemoveProfiles(l.Dir)
	if err != nil {
		return Forgotten{}, err
	}
	if n == 0 {
		return Forgotten{Summary: fmt.Sprintf("Unlinked %q (%s); it had no launcher profile left.", l.Name, e.Title)}, nil
	}
	return Forgotten{
		Removed: RemovedProfile,
		Summary: fmt.Sprintf("Unlinked %q (%s): removed its launcher profile; the build directory and the loader stay.", l.Name, e.Title),
	}, nil
}

var plainShellArg = regexp.MustCompile(`^[A-Za-z0-9_./:@%+=,-]+$`)

func shellArg(s string) string {
	if plainShellArg.MatchString(s) {
		return s
	}
	return CommandArg(s)
}

// Detect names the launcher that owns a game directory, and the launcher's data
// directory when the layout gives it away. It reads an instance registered
// before shulker recorded a launcher, or one a plain `sync --into` found.
func Detect(gameDir string) (name, dir string) {
	if filepath.Base(gameDir) == GDLauncherGameDir {
		instanceDir := filepath.Dir(gameDir)
		if instances := filepath.Dir(instanceDir); filepath.Base(instances) == "instances" {
			if _, err := os.Stat(filepath.Join(instanceDir, GDLauncherInstanceFile)); err == nil {
				return "gdlauncher", filepath.Dir(instances)
			}
		}
	}
	if instances := filepath.Dir(gameDir); filepath.Base(instances) == "instances" {
		if _, err := os.Stat(filepath.Join(gameDir, ATLauncherInstanceFile)); err == nil {
			return "atlauncher", filepath.Dir(instances)
		}
	}
	if base := filepath.Base(gameDir); base != "minecraft" && base != ".minecraft" {
		return "", ""
	}
	instanceDir := filepath.Dir(gameDir)
	if _, err := os.Stat(filepath.Join(instanceDir, PackFile)); err != nil {
		return "", ""
	}
	cfg, err := os.ReadFile(filepath.Join(instanceDir, InstanceConfigFile))
	if err != nil {
		return "", ""
	}
	instances := filepath.Dir(instanceDir)
	if filepath.Base(instances) == "instances" {
		dir = filepath.Dir(instances)
	}
	if dir != "" {
		if _, err := os.Stat(filepath.Join(dir, "prismlauncher.cfg")); err == nil {
			return "prism", dir
		}
		if _, err := os.Stat(filepath.Join(dir, "multimc.cfg")); err == nil {
			return "multimc", dir
		}
	}
	// Prism needs ConfigVersion to parse instance.cfg at all; MultiMC has no such key.
	if bytes.Contains(cfg, []byte("ConfigVersion")) {
		return "prism", dir
	}
	return "multimc", dir
}
