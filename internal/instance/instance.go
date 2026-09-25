// Package instance reads and writes .shulker/instance.json, how shulker sets up a directory it
// syncs into, and the launches it records there.
package instance

import (
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"

	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/managed"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/schema"
)

const (
	// Dir also holds state.json and history/; build.StateDir names the same directory.
	Dir       = ".shulker"
	FileName  = "instance.json"
	SchemaURL = schema.Base + string(schema.Instance)
)

// ErrNotFound is returned for a directory shulker has never synced into.
var ErrNotFound = errors.New("no " + FileName)

// How a sync ended, as resolved.lastResult records it.
const (
	ResultOK     = "ok"
	ResultFailed = "failed"
)

// File is how shulker sets an instance directory up, and, where the directory is no project of its
// own, what it syncs from. It is the only file read as intent; .shulker/state.json records what the
// last build actually did.
type File struct {
	Schema string `json:"$schema"`
	// Source, Ref, Path and Side are what a directory with no manifest of its own syncs from. An
	// instance that is a project keeps them in its manifest instead — the one modpack it requires,
	// and the side carrying `build: "."` — so nothing here can go stale against it.
	Source string `json:"source,omitempty"`
	Ref    string `json:"ref,omitempty"`
	Path   string `json:"path,omitempty"`
	Side   string `json:"side,omitempty"`
	// AssumesClient builds a directory from the mods both sides share while its source declares no
	// client.
	AssumesClient bool `json:"assumeClient,omitempty"`
	// IsUnlinked marks a directory `unlink` let go of, so a repair scan doesn't register it again.
	IsUnlinked bool      `json:"unlinked,omitempty"`
	Settings   Settings  `json:"settings"`
	Resolved   *Resolved `json:"resolved,omitempty"`
}

// Settings are the instance's own. Memory, JVMArgs, Java, Window and Wrapper override the play.*
// default of the same name in config.json when shulker launches the instance itself; an absent one
// inherits it.
type Settings struct {
	Hooks    Hooks     `json:"hooks"`
	Commands *Commands `json:"commands,omitempty"`
	Marker   *bool     `json:"marker,omitempty"`
	LaunchSettings
	Account       string `json:"account,omitempty"`
	Shulker       string `json:"shulker,omitempty"`
	LaunchHistory *int   `json:"launchHistory,omitempty"`
	SavesGroup    string `json:"savesGroup,omitempty"`
}

// LaunchSettings are the settings a launch takes from the instance, falling back to config.json's
// play defaults for each one the instance leaves unset.
type LaunchSettings struct {
	Memory  string   `json:"memory,omitempty"`
	JVMArgs []string `json:"jvmArgs,omitempty"`
	Java    string   `json:"java,omitempty"`
	Window  string   `json:"window,omitempty"`
	Wrapper []string `json:"wrapper,omitempty"`
}

// LaunchKeys are the JSON keys of LaunchSettings.
var LaunchKeys = []string{"memory", "jvmArgs", "java", "window", "wrapper"}

// DefaultMemory is the heap a launch gets when neither the instance, config.json nor the pack
// names one: the fixed default other launchers use, never the JVM's own quarter of RAM.
const DefaultMemory = "4G"

// ForLaunch is the settings a launch runs with: s over config.json's play defaults, and memory
// falling through two more steps, to the pack's client.memory and then DefaultMemory, so the
// game always gets a heap shulker chose.
func (s LaunchSettings) ForLaunch(play LaunchSettings, packMemory string) LaunchSettings {
	s = s.Over(play)
	if s.Memory == "" {
		s.Memory = packMemory
	}
	if s.Memory == "" {
		s.Memory = DefaultMemory
	}
	return s
}

// Over is s with each setting it leaves unset taken from base.
func (s LaunchSettings) Over(base LaunchSettings) LaunchSettings {
	if s.Memory == "" {
		s.Memory = base.Memory
	}
	if s.JVMArgs == nil {
		s.JVMArgs = base.JVMArgs
	}
	if s.Java == "" {
		s.Java = base.Java
	}
	if s.Window == "" {
		s.Window = base.Window
	}
	if s.Wrapper == nil {
		s.Wrapper = base.Wrapper
	}
	return s
}

// Hooks switch shulker's pre-launch and post-exit work on and off. An absent switch is on.
type Hooks struct {
	PreLaunch *bool `json:"preLaunch,omitempty"`
	PostExit  *bool `json:"postExit,omitempty"`
}

// Commands are the launcher's own slot commands, moved here when shulker took the slot over. The
// generated script runs them first, so they keep aborting a launch on a non-zero exit.
type Commands struct {
	PreLaunch string `json:"preLaunch,omitempty"`
	PostExit  string `json:"postExit,omitempty"`
}

// Resolved is what shulker found when it last synced, written for the player to read and never read
// back as intent.
type Resolved struct {
	Java         string `json:"java,omitempty"`
	LauncherJava string `json:"launcherJava,omitempty"`
	LastSyncAt   string `json:"lastSyncAt,omitempty"`
	LastResult   string `json:"lastResult,omitempty"`
}

func Path(dir string) string { return filepath.Join(dir, Dir, FileName) }

// NativesDir is where a launch unpacks its native libraries. It sits under .shulker/ rather than
// in the game directory, which belongs to the pack.
func NativesDir(dir string) string { return filepath.Join(dir, Dir, "natives") }

// New is the file shulker writes for a directory it starts syncing into. The hooks are written at
// their default so opening it shows what shulker will do; marker is left absent, because an absent
// marker defers to the manifest and a written one would freeze the manifest's value at this moment.
func New() *File {
	return &File{
		Schema:   SchemaURL,
		Settings: Settings{Hooks: Hooks{PreLaunch: On(), PostExit: On()}},
	}
}

func Load(dir string) (*File, error) {
	var f File
	err := managed.Read(schema.Instance, Path(dir), &f)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, unreadable(err)
	}
	return &f, nil
}

// unreadable points a file this shulker can't read at repair. A newer file isn't pointed there:
// repair would write it back in this shulker's shape.
func unreadable(err error) error {
	if out.CodeOf(err) == "instance-invalid" {
		e := out.AsError(err)
		e.Help = "run `shulker instances repair` to write it again"
		return e
	}
	return err
}

func (f *File) Save(dir string) error {
	f.Schema = SchemaURL
	if err := os.MkdirAll(filepath.Join(dir, Dir), 0o755); err != nil {
		return err
	}
	return fsutil.WriteJSON(Path(dir), f)
}

// Replace saves f after renaming the file already there to instance.json.replaced, and returns where
// the old file went, empty when there was none.
func (f *File) Replace(dir string) (kept string, err error) {
	f.Schema = SchemaURL
	if err := os.MkdirAll(filepath.Join(dir, Dir), 0o755); err != nil {
		return "", err
	}
	return managed.Replace(schema.Instance, Path(dir), f)
}

// Java is the Java the game runs with: the instance's own setting, else the one shulker resolved.
func (f *File) Java() string {
	if f.Settings.Java != "" || f.Resolved == nil {
		return f.Settings.Java
	}
	return f.Resolved.Java
}

// RecordSync stamps how a sync ended. A failed one leaves LastSyncAt where it is: the files in the
// directory are still the ones the last good sync built.
func (f *File) RecordSync(at, result string) {
	r := f.EnsureResolved()
	if result == ResultOK {
		r.LastSyncAt = at
	}
	r.LastResult = result
}

// SaveDocument writes the instance file as a document, once the schema allows it, through Save so
// the file keeps the order shulker writes it in.
func SaveDocument(dir string, doc map[string]any) error {
	doc = maps.Clone(doc)
	doc["$schema"] = SchemaURL
	data, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	if err := schema.Validate(schema.Instance, data); err != nil {
		return schema.Invalid("instance-invalid", Path(dir), data, err)
	}
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return err
	}
	return f.Save(dir)
}

// EnsureResolved returns f.Resolved, creating it first if the file records none.
func (f *File) EnsureResolved() *Resolved {
	if f.Resolved == nil {
		f.Resolved = &Resolved{}
	}
	return f.Resolved
}

func (s Settings) PreLaunch() bool { return s.Hooks.PreLaunch == nil || *s.Hooks.PreLaunch }

func (s Settings) PostExit() bool { return s.Hooks.PostExit == nil || *s.Hooks.PostExit }

// On is a switch a link flag or a hand edit turned on.
func On() *bool {
	v := true
	return &v
}

// Off is a switch a link flag or a hand edit turned off.
func Off() *bool {
	v := false
	return &v
}
