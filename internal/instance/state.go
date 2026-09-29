package instance

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/managed"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/takedown"
	"shulker.sh/shulker/schema"
)

// StateFile is the state file's name under .shulker.
const StateFile = "state.json"

// StatePath is where dir's state file sits.
func StatePath(dir string) string {
	return filepath.Join(dir, Dir, StateFile)
}

// Origin is where the project a build came from was synced from: a source, a ref, path and commit,
// or an archive's sha256. It is empty for a build of the local project.
type Origin struct {
	Source string `json:"source,omitempty"`
	Ref    string `json:"ref,omitempty"`
	Path   string `json:"path,omitempty"`
	Commit string `json:"commit,omitempty"`
	Sha256 string `json:"sha256,omitempty"`
}

// State is .shulker/state.json: what the last build wrote into a directory, and the hash of each
// file it owns.
type State struct {
	Schema string `json:"$schema"`
	Side   string `json:"side"`
	Origin
	BuiltAt       string                       `json:"builtAt"`
	LockSha256    string                       `json:"lockSha256"`
	Minecraft     string                       `json:"minecraft,omitempty"`
	Loader        string                       `json:"loader,omitempty"`
	LoaderVersion string                       `json:"loaderVersion,omitempty"`
	Files         map[string]string            `json:"files"`
	Values        map[string]map[string]string `json:"managedValues,omitempty"`
	Links         []string                     `json:"links,omitempty"`
	// Packs is the file name each resource pack was placed under, so a renamed
	// pack's entry in the enabled list can follow it.
	Packs map[string]string `json:"packs,omitempty"`
	// InstalledLoader is the loader its own installer set up in the dir; the installer's files aren't tracked.
	InstalledLoader *InstalledLoader `json:"installedLoader,omitempty"`
	// LauncherImage is the hash of the instance image shulker last wrote into the launcher.
	LauncherImage string `json:"launcherImage,omitempty"`
	// Takedowns is the last takedown check a sync ran for the directory, which a build warns from.
	Takedowns *Takedowns `json:"takedowns,omitempty"`
	// Entries are the lock entries the last build placed from, which the next sync's changes are
	// found against; nil before any build recorded them.
	Entries []LockedEntry `json:"entries,omitempty"`
	// Changelog is what each sync of the last month brought, oldest first, for the marker.
	Changelog []Changes `json:"changelog,omitempty"`
}

// LockedEntry is one entry of a lock, by what a sync compares: where it comes from.
type LockedEntry struct {
	Key      string `json:"key"`
	Type     string `json:"type"`
	Provider string `json:"provider,omitempty"`
	Project  string `json:"project,omitempty"`
}

// Changes are what one sync brought that the player should see before playing it: mods and packs
// it adds, files no provider published, and entries now locked from another project.
type Changes struct {
	At          string    `json:"at"`
	Added       []Changed `json:"added"`
	Unpublished []Changed `json:"unpublished"`
	Moved       []Changed `json:"moved"`
}

// IsEmpty reports whether c brought nothing to show.
func (c *Changes) IsEmpty() bool {
	return c == nil || len(c.Added)+len(c.Unpublished)+len(c.Moved) == 0
}

// Changed is one change a sync brought: a lock entry by key, or a file an override folder lays by
// path.
type Changed struct {
	Key      string `json:"key,omitempty"`
	Type     string `json:"type,omitempty"`
	Path     string `json:"path,omitempty"`
	Provider string `json:"provider,omitempty"`
	Project  string `json:"project,omitempty"`
	// WasProvider and WasProject are where a moved entry was locked from before.
	WasProvider string `json:"wasProvider,omitempty"`
	WasProject  string `json:"wasProject,omitempty"`
}

// Takedowns is when a sync last asked the providers about a directory's locked files, and the
// ones it found gone or filed under another project.
type Takedowns struct {
	CheckedAt string          `json:"checkedAt"`
	Files     []takedown.File `json:"files"`
}

// InstalledLoader is a loader that its own installer set up, rather than shulker.
type InstalledLoader struct {
	Type    string `json:"type"`
	Version string `json:"version"`
}

// StateError is why ReadState treated a state file as empty: every file in the directory then counts
// as not written by shulker.
type StateError struct {
	Path string
	// Newer is a file written by a newer shulker, whose fix is `shulker self update` rather than
	// --force.
	Newer bool
	// Cause is why, without the path.
	Cause error
}

func (e *StateError) Error() string {
	if e.Newer {
		return fmt.Sprintf("%s %v; treating every file as not written by shulker", e.Path, e.Cause)
	}
	return fmt.Sprintf("%s is unreadable (%v); treating every file as not written by shulker", e.Path, e.Cause)
}

// ReadState treats a state file it can't read as empty, like LoadState, and also returns why.
func ReadState(dir string) (State, *StateError) {
	path := StatePath(dir)
	empty := State{Files: map[string]string{}}
	var s State
	err := managed.Read(schema.State, path, &s)
	if errors.Is(err, fs.ErrNotExist) {
		return empty, nil
	}
	if err != nil {
		e := out.AsError(err)
		if e.Cause == nil {
			return empty, &StateError{Path: path, Cause: err}
		}
		return empty, &StateError{Path: path, Newer: e.Code == "schema-newer", Cause: e.Cause}
	}
	if s.Files == nil {
		s.Files = map[string]string{}
	}
	return s, nil
}

// LoadState is ReadState without the reason: a state it can't read is empty.
func LoadState(dir string) State {
	s, _ := ReadState(dir)
	return s
}

// WriteState writes s as dir's state file under the current schema.
func WriteState(dir string, s State) error {
	if err := os.MkdirAll(filepath.Join(dir, Dir), 0o755); err != nil {
		return err
	}
	s.Schema = schema.URL(schema.State)
	return fsutil.WriteJSON(StatePath(dir), s)
}

// RecordLoader notes in dir's state that l was set up by its own installer.
func RecordLoader(dir string, l InstalledLoader) error {
	s := LoadState(dir)
	s.InstalledLoader = &l
	return WriteState(dir, s)
}

// RecordLauncherImage notes in dir's state the hash of the instance image written into the launcher.
func RecordLauncherImage(dir, hash string) error {
	s := LoadState(dir)
	s.LauncherImage = hash
	return WriteState(dir, s)
}
