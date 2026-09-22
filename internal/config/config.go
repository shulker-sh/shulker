// Package config reads and writes shulker's machine-wide files: config.json, and the registry of
// the instances shulker syncs.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"

	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/schema"
)

const (
	PathEnv           = "SHULKER_CONFIG"
	DataPathEnv       = "SHULKER_DATA"
	RegistryFileName  = "registry.json"
	RegistrySchemaURL = "https://shulker.sh/schema/v1/registry.json"
)

// Keys are the config.json keys `shulker config` reads and sets.
var Keys = []string{"accounts.default", "accounts.providers", "curseforge.key", "instances", "log.keepDays", "play.java", "play.jvmArgs", "play.memory", "play.saveBackups", "play.window", "play.wrapper", "registry", "saves", "store"}

// Config is config.json.
type Config struct {
	Accounts   Accounts   `json:"accounts"`
	CurseForge CurseForge `json:"curseforge"`
	Instances  string     `json:"instances,omitempty"`
	Log        Log        `json:"log"`
	Play       Play       `json:"play"`
	Registry   string     `json:"registry,omitempty"`
	Saves      string     `json:"saves,omitempty"`
	Store      string     `json:"store,omitempty"`
}

// Play is how shulker launches its own instances unless an instance says otherwise: each key here
// is a default the same key in an instance's settings overrides, and an absent one inherits.
type Play struct {
	Memory  string   `json:"memory,omitempty"`
	JVMArgs []string `json:"jvmArgs,omitempty"`
	Java    string   `json:"java,omitempty"`
	Window  string   `json:"window,omitempty"`
	Wrapper []string `json:"wrapper,omitempty"`
	// SaveBackups is how many automatic pre-change backups a save group or instance keeps. It is
	// global only: no instance setting overrides it.
	SaveBackups *int `json:"saveBackups,omitempty"`
}

const DefaultSaveBackups = 5

// Backups is SaveBackups with its default filled in; 0 takes no automatic backups.
func (p Play) Backups() int {
	if p.SaveBackups == nil {
		return DefaultSaveBackups
	}
	return *p.SaveBackups
}

// Log is how long log.jsonl keeps what each run did.
type Log struct {
	KeepDays *int `json:"keepDays,omitempty"`
}

const DefaultLogKeepDays = 30

// Days is KeepDays with its default filled in, which also stands in for a count too small to keep
// anything.
func (l Log) Days() int {
	if l.KeepDays == nil || *l.KeepDays < 1 {
		return DefaultLogKeepDays
	}
	return *l.KeepDays
}

type CurseForge struct {
	Key string `json:"key"`
}

// Accounts is which accounts shulker can see and which one it uses by default. The default lives
// here rather than in accounts.json because a borrowed account may be it, and shulker never writes
// another launcher's accounts into its own file.
type Accounts struct {
	// Providers is where accounts are read from, in the order the earliest one wins ties by UUID.
	Providers []string `json:"providers,omitempty"`
	// Default is the id of the account a launch falls back to.
	Default string `json:"default,omitempty"`
}

// Instance is one row of the registry: the index shulker keeps of the instances it syncs. What an
// instance syncs from and how it is set up lives in the instance's own .shulker/instance.json;
// LauncherDir stays here because unlink needs it when the instance directory is gone.
type Instance struct {
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"`
	Launcher    string `json:"launcher,omitempty"`
	LauncherDir string `json:"launcherDir,omitempty"`
	Dir         string `json:"dir"`
	Source      string `json:"source"`
	LastSync    string `json:"lastSync,omitempty"`
	LastError   string `json:"lastError,omitempty"`
}

// Label is what shulker calls an instance in its own output: the name the launcher shows, or the
// id when there is none.
func (i Instance) Label() string {
	if i.Name != "" {
		return i.Name
	}
	return i.ID
}

// Path is config.json: SHULKER_CONFIG when it is set, else shulker/config.json in the user config
// directory.
func Path() (string, error) {
	if p := os.Getenv(PathEnv); p != "" {
		return p, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", configDirUnset(err)
	}
	return filepath.Join(base, "shulker", "config.json"), nil
}

// DataDir is where shulker keeps what a player would miss if it went: instances and save groups,
// as against the cache, which holds only what it can fetch again. macOS and Windows keep data
// beside the config; Linux separates the two, so XDG_DATA_HOME decides there.
func DataDir() (string, error) {
	if dir := os.Getenv(DataPathEnv); dir != "" {
		return dir, nil
	}
	if runtime.GOOS == "linux" {
		if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
			return filepath.Join(dir, "shulker"), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", configDirUnset(err)
		}
		return filepath.Join(home, ".local", "share", "shulker"), nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", configDirUnset(err)
	}
	return filepath.Join(base, "shulker"), nil
}

func configDirUnset(err error) *out.Error {
	e := out.Errorf("config-dir-unset", "shulker can't tell where this user's config and data folders are")
	e.WithCause("os", err)
	e.Help = "set " + PathEnv + " and " + DataPathEnv
	return e
}

func Load() (Config, error) {
	path, err := Path()
	if err != nil {
		return Config{}, err
	}
	return LoadFile(path)
}

// LoadFile reads config.json at path. A file that isn't there is the zero Config.
func LoadFile(path string) (Config, error) {
	var cfg Config
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, schema.Invalid("config-invalid", path, data, err)
	}
	return cfg, nil
}

// LoadDocument reads config.json as a raw document, numbers kept exact, so a key can be set without
// dropping the ones Config doesn't know. A file that isn't there is an empty document.
func LoadDocument(path string) (map[string]any, error) {
	doc := map[string]any{}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return doc, nil
	}
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		return nil, schema.Invalid("config-invalid", path, data, err)
	}
	if doc == nil {
		doc = map[string]any{}
	}
	return doc, nil
}

// SaveDocument creates a missing config.json readable by its owner only, since it can hold an API
// key; fsutil keeps the mode of an existing file.
func SaveDocument(path string, doc map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	created := false
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	switch {
	case err == nil:
		f.Close()
		created = true
	case !errors.Is(err, fs.ErrExist):
		return err
	}
	if err := fsutil.WriteJSON(path, doc); err != nil {
		if created {
			os.Remove(path)
		}
		return err
	}
	return nil
}

// RegistryPath is the registry the config at configPath points to, registry.json beside it by default.
func RegistryPath(configPath string, cfg Config) string {
	return Root(configPath, cfg.Registry, filepath.Join(filepath.Dir(configPath), RegistryFileName))
}

// Root resolves a path-valued key of config.json: an absolute value as given, a relative one
// against config.json's own directory, and an empty one to the default the caller passes.
func Root(configPath, value, fallback string) string {
	switch {
	case value == "":
		return fallback
	case filepath.IsAbs(value):
		return value
	default:
		return filepath.Join(filepath.Dir(configPath), value)
	}
}

// LoadInstances reads the registry at path. A registry that is missing or empty has no instances.
func LoadInstances(path string) ([]Instance, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}
	if err := checkRegistrySchema(path, data); err != nil {
		return nil, err
	}
	if err := schema.Validate(schema.Registry, data); err != nil {
		return nil, schema.Invalid("registry-invalid", path, data, err)
	}
	var registry struct {
		Instances []Instance `json:"instances"`
	}
	if err := json.Unmarshal(data, &registry); err != nil {
		return nil, schema.Invalid("registry-invalid", path, data, err)
	}
	return registry.Instances, nil
}

// checkRegistrySchema reports a registry this shulker can't read as something to repair, rather
// than as the list of schema failures validating it would produce. A newer registry isn't: repair
// would write it back in this shulker's shape.
func checkRegistrySchema(path string, data []byte) error {
	err := schema.CheckMarker(schema.Registry, "registry-invalid", path, data)
	if out.CodeOf(err) == "registry-invalid" {
		e := out.AsError(err)
		e.Help = "run `shulker instances repair` to rebuild it"
		return e
	}
	return err
}

// CreateRegistry writes an empty registry at path when nothing is there yet, and reports whether it
// did.
func CreateRegistry(path string) (bool, error) {
	if _, err := os.Stat(path); err == nil || !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	return true, fsutil.WriteJSON(path, map[string]any{"$schema": RegistrySchemaURL})
}

func FindInstance(instances []Instance, dir string) (int, bool) {
	dir = filepath.Clean(dir)
	for i, in := range instances {
		if filepath.Clean(in.Dir) == dir {
			return i, true
		}
	}
	return -1, false
}

func FindID(instances []Instance, id string) (int, bool) {
	for i, in := range instances {
		if in.ID == id {
			return i, true
		}
	}
	return -1, false
}

// WriteInstances replaces the registry wholesale, keeping the file it replaces as
// registry.json.replaced, and returns where that went. It is what repair uses, since the file it is
// fixing may be one UpdateInstances refuses to read.
func WriteInstances(path string, instances []Instance) (kept string, err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	doc := map[string]any{"$schema": RegistrySchemaURL}
	if len(instances) > 0 {
		doc["instances"] = instances
	}
	data, err := fsutil.MarshalJSON(doc)
	if err != nil {
		return "", err
	}
	return fsutil.Replace(path, data)
}

// UpdateInstances rereads the registry right before writing and rewrites only the instances key, so
// another writer's entries and keys shulker doesn't know survive.
func UpdateInstances(path string, update func([]Instance) []Instance) (bool, error) {
	top := map[string]json.RawMessage{}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if len(bytes.TrimSpace(data)) > 0 {
		if err := checkRegistrySchema(path, data); err != nil {
			return false, err
		}
		if err := json.Unmarshal(data, &top); err != nil {
			return false, schema.Invalid("registry-invalid", path, data, err)
		}
	}
	var instances []Instance
	if raw, ok := top["instances"]; ok {
		if err := json.Unmarshal(raw, &instances); err != nil {
			return false, schema.Invalid("registry-invalid", path, data, err)
		}
	}
	next := update(slices.Clone(instances))
	if slices.Equal(instances, next) {
		return false, nil
	}
	if top["$schema"], err = json.Marshal(RegistrySchemaURL); err != nil {
		return false, err
	}
	if len(next) == 0 {
		delete(top, "instances")
	} else if top["instances"], err = json.Marshal(next); err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	return true, fsutil.WriteJSON(path, top)
}
