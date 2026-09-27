package launcher

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"shulker.sh/shulker/internal/account"
	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/out"
)

// Usage is how a launcher's link command reads: its help, and which of the shared flags it takes.
type Usage struct {
	Short   string
	Aliases []string
	// Noun is what the launcher calls what a link makes there: an instance, or a profile.
	Noun string
	// Dir is the --launcher-dir help; empty for a launcher with no such flag.
	Dir string
	// DirHint says what the folder holds, for the question a terminal asks when the launcher has
	// no default directory; NoDefault is the error off one.
	DirHint   string
	NoDefault string
	// Names marks a launcher that shows the name a link gives, so --name exists.
	Names bool
	As    string
	Force string
}

// Link is what a launcher needs to make an instance for a build: where it goes, what to call it,
// the platform it starts, and the means to install that platform.
type Link struct {
	LauncherDir   string
	Name          string
	ID            string
	Minecraft     string
	LoaderType    string
	LoaderVersion string
	Force         bool
	// Registry is every instance shulker knows, for a launcher that names an instance around them.
	Registry []config.Instance
	Cache    *cache.Cache
	Fetch    *fetch.Client
	// MetaURL is the launcher's metadata service, the entry's own unless a test points elsewhere.
	MetaURL  string
	Versions ClientVersions
	Log      func(format string, args ...any)
	// Working shows work under way that clears when it ends, for a check whose outcome is a
	// warning or nothing.
	Working func(format string, args ...any)
	Warn    func(format string, args ...any)
}

// ClientVersions is what a link needs from the loader side to make a launcher start the locked
// platform: the vanilla version JSON, and the loader's, either as a profile for loaders that
// publish one or by running the installer for those that don't.
type ClientVersions interface {
	Vanilla(ctx context.Context) (json.RawMessage, error)
	HasInstaller() bool
	LoaderProfile(ctx context.Context) (json.RawMessage, error)
	// InstallClient runs the loader's installer into a launcher directory laid out like the
	// official launcher's, and names the version it installed.
	InstallClient(ctx context.Context, launcherDir string) (string, error)
	InstallerVersion(ctx context.Context) (json.RawMessage, error)
}

// Placement is where a link puts an instance before anything is written: the registry id it goes
// under, its folder and its game directory.
type Placement struct {
	ID      string
	Dir     string
	GameDir string
}

// Locate settles the launcher directory a link works in, failing launcher-not-found when nothing
// is there.
func (e *Entry) Locate(dir string) (string, error) {
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		err := out.Errorf("launcher-not-found", "no %s directory at %s", e.Title, dir)
		err.Help = fmt.Sprintf("run %s once or pass --launcher-dir", e.Title)
		return "", err
	}
	if e.locate == nil {
		return dir, nil
	}
	return e.locate(dir)
}

// Place is where the link would put the instance, and refuses a name the launcher can't use.
func (e *Entry) Place(req *Link) (Placement, error) {
	return e.place(e, req)
}

// Link writes the launcher's own files for the instance where Place put it, installing what the
// launcher needs to start it first.
func (e *Entry) Link(ctx context.Context, req *Link, p Placement) (InstanceResult, error) {
	return e.link(ctx, e, req, p)
}

// AfterNote is the launcher's closing line for a link, or nothing.
func (e *Entry) AfterNote(res InstanceResult) string {
	if e.after == nil {
		return ""
	}
	return e.after(e, res)
}

// LinkedByShulker is whether the folder the launcher names after an instance is shulker's to link:
// the launcher names no folder, nothing is there yet, or what is there has shulker's command in its
// slot. Anything else is an instance the player made.
func (e *Entry) LinkedByShulker(launcherDir, gameDir string) (bool, error) {
	if !e.NamesFolder {
		return true, nil
	}
	slots, found, err := ReadSlots(e, config.Instance{Dir: gameDir, LauncherDir: launcherDir})
	if err != nil {
		return false, err
	}
	return !found || IsShulkerSlot(slots.PreLaunch), nil
}

// LinkDir is the launcher directory a link works in: shulker's instances root for a launcher with
// no directory of its own, else the one given or the launcher's default, made absolute.
func (e *Entry) LinkDir(given, instancesRoot string) (string, error) {
	switch {
	case !e.HasDir():
		given = instancesRoot
	case given == "":
		var err error
		if given, err = e.DefaultDir(); err != nil {
			return "", err
		}
	}
	return filepath.Abs(given)
}

// AccountStores is every name accounts.stores accepts: shulker's own file first, then each launcher
// whose accounts shulker reads, in table order.
func AccountStores() []string {
	names := []string{account.SourceShulker}
	for _, e := range All {
		if e.Accounts != nil {
			names = append(names, e.Name)
		}
	}
	return names
}

// ReadAccounts is the accounts a launcher holds in dir, as its reader takes them. A launcher with no
// reader yields nothing.
func (e *Entry) ReadAccounts(dir string, now time.Time) ([]account.Resolved, []error) {
	if e.Accounts == nil {
		return nil, nil
	}
	return e.Accounts(e, dir, now)
}

var unsafeKeyChars = regexp.MustCompile(`[^a-z0-9]+`)

// InstanceKey is the folder or profile key a launcher keeps a shulker instance under, marked as
// shulker's by its prefix.
func InstanceKey(name string) string {
	slug := strings.Trim(unsafeKeyChars.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if slug == "" {
		slug = "project"
	}
	return "shulker-" + slug
}

// blankName refuses an instance name the launcher's folder rule leaves empty.
func blankName(message string) error {
	e := out.Errorf("usage", "%s", message)
	e.Help = "pass --name"
	return e
}
