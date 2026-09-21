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
	RemovedPostExit  = "post-exit command"
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
	relink            func(e *Entry, l Linked) (args []string, in string)
	forget            func(e *Entry, l config.Instance) (Forgotten, error)
}

// Linked is a registry row plus the intent its instance.json records, which is where the side and
// ref the relink command needs now live.
type Linked struct {
	config.Instance
	Side         string
	AssumeClient bool
	Ref          string
}

var All = []*Entry{
	{Name: "shulker", Title: "Shulker", Instanced: true, gameDirIsInstance: true, relink: relinkShulker, forget: forgetShulker},
	{Name: "prism", Title: "Prism Launcher", Instanced: true, DefaultDir: DefaultPrismDir, relink: relinkLauncher, forget: forgetInstance},
	{Name: "multimc", Title: "MultiMC", Instanced: true, relink: relinkLauncher, forget: forgetInstance},
	{Name: "mojang", Title: "Minecraft Launcher", DefaultDir: DefaultMojangDir, relink: relinkLauncher, forget: forgetMojang},
	{Name: "atlauncher", Title: "ATLauncher", Instanced: true, DefaultDir: DefaultATLauncherDir, gameDirIsInstance: true, relink: relinkLauncher, forget: forgetInstance},
	{Name: "gdlauncher", Title: "GDLauncher", Instanced: true, DefaultDir: DefaultGDLauncherDir, relink: relinkLauncher, forget: forgetInstance},
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
func Relink(l Linked) (command, in string) {
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

// Forget stops a launcher from syncing an instance, without touching its files.
func Forget(l config.Instance) (Forgotten, error) {
	e := Find(l.Launcher)
	if e == nil {
		return Forgotten{Summary: fmt.Sprintf("Forgot %q (%s); its files stay.", l.Label(), l.Dir)}, nil
	}
	return e.forget(e, l)
}

func relinkSync(l Linked) (args []string, in string) {
	args = []string{"shulker", "sync", shellArg(l.Source)}
	if l.Ref != "" {
		args = append(args, "--ref", shellArg(l.Ref))
	}
	args = append(args, "--side", shellArg(l.Side))
	if l.AssumeClient {
		args = append(args, "--assume-client")
	}
	return append(args, "--into", shellArg(l.Dir)), ""
}

// relinkShulker rebuilds an instance shulker owns. It names the instance with --as rather than
// --name, because the id is also the folder under the instances root, and it carries no side: a
// shulker instance is a client.
func relinkShulker(e *Entry, l Linked) (args []string, in string) {
	args = []string{"shulker", "link", e.Name, shellArg(l.Source)}
	if l.Ref != "" {
		args = append(args, "--ref", shellArg(l.Ref))
	}
	return append(args, "--as", shellArg(l.ID)), ""
}

// forgetShulker has nothing to take away. Shulker runs its own hooks in process, so no slot holds a
// command and no script was generated; unlinking is the registry row going and nothing else.
func forgetShulker(e *Entry, l config.Instance) (Forgotten, error) {
	return Forgotten{Summary: fmt.Sprintf("Unlinked %q (%s); the instance directory and its worlds stay.", l.Label(), e.Title)}, nil
}

// relinkLauncher rebuilds an instance a launcher owns. Every one of them names the source and
// follows it as a modpack, so the command is the same shape whichever launcher wrote the instance.
func relinkLauncher(e *Entry, l Linked) (args []string, in string) {
	args = []string{"shulker", "link", e.Name, shellArg(l.Source)}
	if l.Ref != "" {
		args = append(args, "--ref", shellArg(l.Ref))
	}
	return append(args, "--name", shellArg(l.Label())), ""
}

func forgetInstance(e *Entry, l config.Instance) (Forgotten, error) {
	if _, err := os.Stat(e.InstanceDir(l.Dir)); errors.Is(err, os.ErrNotExist) {
		return Forgotten{Summary: fmt.Sprintf("Unlinked %q (%s); its instance was already gone.", l.Label(), e.Title)}, nil
	}
	tookPreLaunch, tookPostExit, err := ReleaseSlots(e, l)
	if err != nil {
		return Forgotten{}, err
	}
	if !tookPreLaunch && !tookPostExit {
		return Forgotten{Summary: fmt.Sprintf("Unlinked %q (%s); its pre-launch command isn't a shulker sync, so it was kept.", l.Label(), e.Title)}, nil
	}
	// Report the slot that actually went: a pre-launch command shulker never wrote is kept, and then
	// the post-exit slot is all there was to remove.
	removed, what := RemovedPreLaunch, "pre-launch sync"
	if !tookPreLaunch {
		removed, what = RemovedPostExit, "post-exit command"
	}
	summary := fmt.Sprintf("Unlinked %q (%s): removed its %s; the instance and its worlds stay.", l.Label(), e.Title, what)
	// Unlink warns when GDLauncher is open, so the restart reminder is only for where it can't tell.
	if _, detectable := GDLauncherRunning(); e.Name != "gdlauncher" || !detectable {
		summary += "\nRestart the launcher if it is open so the change is picked up."
	}
	return Forgotten{Removed: removed, Summary: summary}, nil
}

func forgetMojang(e *Entry, l config.Instance) (Forgotten, error) {
	// Unlink takes the generated scripts and the shim with the profile, and puts the profile's own
	// Java back on the way.
	if _, _, err := ReleaseSlots(e, l); err != nil {
		return Forgotten{}, err
	}
	n, err := (&Mojang{Dir: l.LauncherDir}).RemoveProfiles(l.Dir)
	if err != nil {
		return Forgotten{}, err
	}
	if n == 0 {
		return Forgotten{Summary: fmt.Sprintf("Unlinked %q (%s); it had no launcher profile left.", l.Label(), e.Title)}, nil
	}
	return Forgotten{
		Removed: RemovedProfile,
		Summary: fmt.Sprintf("Unlinked %q (%s): removed its launcher profile; the instance directory and the loader stay.", l.Label(), e.Title),
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
