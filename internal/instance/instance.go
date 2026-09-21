package instance

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/schema"
)

const (
	// Dir also holds state.json and history/; build.StateDir names the same directory.
	Dir       = ".shulker"
	FileName  = "instance.json"
	SchemaURL = "https://shulker.sh/schema/v1/instance.json"
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
	// Source, Ref and Side are what a directory with no manifest of its own syncs from. An instance
	// that is a project keeps all three in its manifest instead — the one modpack it requires, and
	// the side carrying `build: "."` — so nothing here can go stale against it.
	Source       string `json:"source,omitempty"`
	Ref          string `json:"ref,omitempty"`
	Side         string `json:"side,omitempty"`
	AssumeClient bool   `json:"assumeClient,omitempty"`
	// Unlinked keeps a directory `unlink` let go of from being registered again by a repair scan.
	Unlinked bool      `json:"unlinked,omitempty"`
	Settings Settings  `json:"settings"`
	Resolved *Resolved `json:"resolved,omitempty"`
}

// Settings are the instance's own. Memory, JvmArgs, Java, Window and Wrapper override the play.*
// default of the same name in config.json when shulker launches the instance itself; an absent one
// inherits it.
type Settings struct {
	Hooks         Hooks     `json:"hooks"`
	Commands      *Commands `json:"commands,omitempty"`
	Marker        *bool     `json:"marker,omitempty"`
	Memory        string    `json:"memory,omitempty"`
	JvmArgs       []string  `json:"jvmArgs,omitempty"`
	Java          string    `json:"java,omitempty"`
	Window        string    `json:"window,omitempty"`
	Wrapper       []string  `json:"wrapper,omitempty"`
	Account       string    `json:"account,omitempty"`
	Shulker       string    `json:"shulker,omitempty"`
	LaunchHistory *int      `json:"launchHistory,omitempty"`
	SavesGroup    string    `json:"savesGroup,omitempty"`
}

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

type Resolved struct {
	Java         string `json:"java,omitempty"`
	LauncherJava string `json:"launcherJava,omitempty"`
	LastSyncAt   string `json:"lastSyncAt,omitempty"`
	LastResult   string `json:"lastResult,omitempty"`
}

func Path(dir string) string { return filepath.Join(dir, Dir, FileName) }

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
	path := Path(dir)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := checkSchema(path, data); err != nil {
		return nil, err
	}
	if err := schema.Validate(schema.Instance, data); err != nil {
		return nil, schema.Invalid("instance-invalid", path, data, err)
	}
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, schema.Invalid("instance-invalid", path, data, err)
	}
	return &f, nil
}

// checkSchema reports a file written by another shulker as something to repair rather than as a
// list of schema failures.
func checkSchema(path string, data []byte) error {
	var head struct {
		Schema string `json:"$schema"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return schema.Invalid("instance-invalid", path, data, err)
	}
	if head.Schema == SchemaURL {
		return nil
	}
	what := "names no $schema"
	if head.Schema != "" {
		what = "names the schema " + head.Schema
	}
	return schema.Invalid("instance-invalid", path, data,
		errors.New(what+", which this shulker doesn't know; `shulker instances repair` writes it again"))
}

func (f *File) Save(dir string) error {
	f.Schema = SchemaURL
	if err := os.MkdirAll(filepath.Join(dir, Dir), 0o755); err != nil {
		return err
	}
	return fsutil.WriteJSON(Path(dir), f)
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
