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

// File is what an instance directory syncs from and how shulker sets it up. It is the only
// file read as intent; .shulker/state.json records what the last build actually did.
type File struct {
	Schema string `json:"$schema"`
	Source string `json:"source"`
	Ref    string `json:"ref,omitempty"`
	Side   string `json:"side"`
	// AssumeClient builds a client the source never declared, from what both sides share.
	AssumeClient bool `json:"assumeClient,omitempty"`
	// Unlinked keeps a directory `unlink` let go of from being registered again by a repair scan.
	Unlinked bool      `json:"unlinked,omitempty"`
	Settings Settings  `json:"settings"`
	Resolved *Resolved `json:"resolved,omitempty"`
}

type Settings struct {
	Hooks         Hooks     `json:"hooks"`
	Commands      *Commands `json:"commands,omitempty"`
	Marker        *bool     `json:"marker,omitempty"`
	Java          string    `json:"java,omitempty"`
	Wrapper       []string  `json:"wrapper,omitempty"`
	Shulker       string    `json:"shulker,omitempty"`
	LaunchHistory *int      `json:"launchHistory,omitempty"`
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

// New is the file shulker writes for a directory it starts syncing into, with every setting at
// its default so opening it shows what shulker will do.
func New(source, ref, side string) *File {
	return &File{
		Schema:   SchemaURL,
		Source:   source,
		Ref:      ref,
		Side:     side,
		Settings: Settings{Hooks: Hooks{PreLaunch: on(), PostExit: on()}, Marker: on()},
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

func (s Settings) MarkerOn() bool { return s.Marker == nil || *s.Marker }

func on() *bool {
	v := true
	return &v
}
