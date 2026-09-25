// Package launcher holds the launcher seam: one file per launcher, each with its entry in the table,
// its usage block, where its instances go, how a link is placed, its after-note and its accounts
// reader. Shulker's own instances are a launcher here too.
package launcher

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"shulker.sh/shulker/internal/account"
	"shulker.sh/shulker/internal/config"
)

const (
	RemovedPreLaunch = "pre-launch command"
	RemovedPostExit  = "post-exit command"
	RemovedProfile   = "launcher profile"
)

// Forgotten is what unlinking an entry did: what it took away, if anything, the sentence describing
// it, and a warning worth printing first.
type Forgotten struct {
	Removed string
	Summary string
	Warning string
}

// InstanceResult is what a link step left in the launcher: the instance folder, its game directory,
// the launcher's own key for it, whether it is new, and the version the launcher was made to
// install, for a launcher that keeps versions of its own.
type InstanceResult struct {
	Dir        string
	GameDir    string
	Key        string
	Created    bool
	Version    string
	VersionDir string
}

// Entry is one launcher shulker links into. Everything that differs between launchers is described
// here, once, rather than switched on by name.
type Entry struct {
	Name  string
	Title string
	// IsInstanced marks launchers that keep the game directory inside an instance directory they
	// own, so the instance going away is the entry going away.
	IsInstanced bool
	DefaultDir  func() (string, error)
	// gameDirIsInstance marks launchers whose instance folder is the game directory itself,
	// rather than holding it as minecraft/.
	gameDirIsInstance bool
	// Slot is how the launcher's command slots behave. Nil means shulker fills none, as for its own
	// instances, which run the hooks themselves.
	Slot *Slot
	// Image is the instance picture shulker keeps in step with the pack icon. Nil means the
	// launcher shows none shulker writes.
	Image *Image
	// Usage is how the launcher's link command reads.
	Usage Usage
	// NamesFolder marks launchers that keep an instance in a folder named after the instance, so
	// a folder a link would take may hold an instance the player made.
	NamesFolder bool
	// MetaURL is the launcher's own metadata service, for a launcher that installs loaders only
	// from one.
	MetaURL string
	// NeedsRuntime marks a launcher whose instances start the game with the Java shulker manages
	// rather than the launcher's own, so a sync records that Java in the instance.
	NeedsRuntime bool
	// Accounts reads the accounts the launcher keeps in its data directory. Nil means it keeps
	// none shulker can read.
	Accounts   func(e *Entry, dir string, now time.Time) ([]account.Resolved, []error)
	relink     func(e *Entry, l Linked) (args []string, in string)
	forget     func(e *Entry, l config.Instance) (Forgotten, error)
	name       func(e *Entry, launcherDir, gameDir string) string
	gameDirs   func(e *Entry, launcherDir string) []string
	readSlots  func(e *Entry, in config.Instance) (Slots, bool, error)
	writeSlots func(e *Entry, in config.Instance, s Slots) error
	// locate settles the launcher directory a link works in, for a launcher that reaches its own
	// files through a resolved path; nil means the directory as given.
	locate func(dir string) (string, error)
	// running says whether the launcher is open, for a launcher that can tell.
	running func() (running, detectable bool)
	place   func(e *Entry, req *Link) (Placement, error)
	link    func(ctx context.Context, e *Entry, req *Link, p Placement) (InstanceResult, error)
	after   func(e *Entry, res InstanceResult) string
}

// Linked is a registry row plus the intent its instance.json records, which is where the side and
// ref and path the relink command needs now live.
type Linked struct {
	config.Instance
	Side          string
	AssumesClient bool
	Ref           string
	Path          string
}

// All is every launcher shulker knows, in the order Rank displays them.
var All = []*Entry{Shulker, prismEntry, multimcEntry, mojangEntry, atlauncherEntry, gdlauncherEntry}

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

// Resolve is Find for an argument a user typed: it takes a launcher's command aliases as well as
// its name.
func Resolve(arg string) *Entry {
	if e := Find(arg); e != nil {
		return e
	}
	for _, e := range All {
		if slices.Contains(e.Usage.Aliases, arg) {
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

// AccountsDir is where the launcher's accounts are read from: the directory a registered instance
// was linked against, since the player named it with `link --launcher-dir` and re-deriving would
// read a folder they don't use, else the launcher's default on this machine, else nowhere.
func (e *Entry) AccountsDir(instances []config.Instance) string {
	for _, in := range instances {
		if in.Launcher == e.Name && in.LauncherDir != "" {
			return in.LauncherDir
		}
	}
	return e.defaultDir()
}

// HasDir says whether the launcher has a directory of its own, which a link records and
// --launcher-dir names. Shulker's own instances root stands in for one otherwise.
func (e *Entry) HasDir() bool { return e.Usage.Dir != "" }

// IsRunning says whether the launcher is open, and whether this machine can tell at all.
func (e *Entry) IsRunning() (running, detectable bool) {
	if e.running == nil {
		return false, false
	}
	return e.running()
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
	if l.Path != "" {
		args = append(args, "--path", shellArg(l.Path))
	}
	args = append(args, "--side", shellArg(l.Side))
	if l.AssumesClient {
		args = append(args, "--assume-client")
	}
	return append(args, "--into", shellArg(l.Dir)), ""
}

// relinkLauncher rebuilds an instance a launcher owns. Every one of them names the source and
// follows it as a modpack, so the command is the same shape whichever launcher wrote the instance.
func relinkLauncher(e *Entry, l Linked) (args []string, in string) {
	args = []string{"shulker", "link", e.Name, shellArg(l.Source)}
	if l.Ref != "" {
		args = append(args, "--ref", shellArg(l.Ref))
	}
	if l.Path != "" {
		args = append(args, "--path", shellArg(l.Path))
	}
	return append(args, "--name", shellArg(l.Label())), ""
}

func forgetInstance(e *Entry, l config.Instance) (Forgotten, error) {
	f := Forgotten{}
	running, detectable := e.IsRunning()
	if running {
		f.Warning = fmt.Sprintf("%s is open; it may put back the pre-launch sync this removes from %q. Quit it, then check the instance's settings", e.Title, l.Label())
	}
	if _, err := os.Stat(e.InstanceDir(l.Dir)); errors.Is(err, os.ErrNotExist) {
		f.Summary = fmt.Sprintf("Unlinked %q (%s); its instance was already gone.", l.Label(), e.Title)
		return f, nil
	}
	tookPreLaunch, tookPostExit, err := ReleaseSlots(e, l)
	if err != nil {
		return Forgotten{}, err
	}
	if !tookPreLaunch && !tookPostExit {
		f.Summary = fmt.Sprintf("Unlinked %q (%s); its pre-launch command isn't a shulker sync, so it was kept.", l.Label(), e.Title)
		return f, nil
	}
	// Report the slot that actually went: a pre-launch command shulker never wrote is kept, and then
	// the post-exit slot is all there was to remove.
	removed, what := RemovedPreLaunch, "pre-launch sync"
	if !tookPreLaunch {
		removed, what = RemovedPostExit, "post-exit command"
	}
	f.Removed = removed
	f.Summary = fmt.Sprintf("Unlinked %q (%s): removed its %s; the instance and its worlds stay.", l.Label(), e.Title, what)
	// Unlink warns when the launcher is open, so the restart reminder is only for where it can't tell.
	if !detectable {
		f.Summary += "\nRestart the launcher if it is open so the change is picked up."
	}
	return f, nil
}

var plainShellArg = regexp.MustCompile(`^[A-Za-z0-9_./:@%+=,-]+$`)

func shellArg(s string) string {
	if plainShellArg.MatchString(s) {
		return s
	}
	return CommandArg(s)
}

// RefreshRow keeps a row that a link or a repair wrote in step with the directory it points at:
// in brings the source, and old keeps its id, name, launcher and last sync. A row written before
// shulker recorded a launcher gets the one its layout gives away.
func RefreshRow(old, in config.Instance) config.Instance {
	in.ID, in.Name = old.ID, old.Name
	in.Launcher, in.LauncherDir = old.Launcher, old.LauncherDir
	in.LastSync, in.LastError = old.LastSync, old.LastError
	if in.Launcher == "" {
		in.Launcher, in.LauncherDir = Detect(in.Dir)
	}
	return in
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
	if _, err := os.Stat(filepath.Join(instanceDir, PrismPackFile)); err != nil {
		return "", ""
	}
	cfg, err := os.ReadFile(filepath.Join(instanceDir, PrismInstanceFile))
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
