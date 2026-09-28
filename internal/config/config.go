// Package config reads and writes shulker's machine-wide files: config.json, and the registry of
// the instances shulker syncs.
package config

import (
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/managed"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/schema"
)

const (
	PathEnv           = "SHULKER_CONFIG"
	DataPathEnv       = "SHULKER_DATA"
	FileName          = "config.json"
	RegistryFileName  = "registry.json"
	RegistrySchemaURL = schema.Base + string(schema.Registry)
)

// Keys are the config.json keys `shulker config` reads and sets.
var Keys = []string{"accounts.default", "accounts.stores", "curseforge.key", "downloads.watch", "eula", "instances", "log.keepDays", "play.java", "play.jvmArgs", "play.memory", "play.saveBackups", "play.window", "play.wrapper", "registry", "saves", "store"}

// Secrets are the keys whose values `shulker config` masks unless asked to reveal them.
var Secrets = []string{"curseforge.key"}

// IsSecret reports whether key holds a secret.
func IsSecret(key string) bool { return slices.Contains(Secrets, key) }

// Redact is a copy of the document with every secret's text passed through mask; a secret that is
// not text, or is unset, is left as it is.
func Redact(doc map[string]any, mask func(string) string) map[string]any {
	doc = maps.Clone(doc)
	for _, key := range Secrets {
		path := strings.Split(key, ".")
		parent := doc
		for i, part := range path {
			if i == len(path)-1 {
				if text, ok := parent[part].(string); ok {
					parent[part] = mask(text)
				}
				break
			}
			child, ok := parent[part].(map[string]any)
			if !ok {
				break
			}
			child = maps.Clone(child)
			parent[part] = child
			parent = child
		}
	}
	return doc
}

// Config is config.json.
type Config struct {
	Accounts   Accounts   `json:"accounts"`
	CurseForge CurseForge `json:"curseforge"`
	Downloads  Downloads  `json:"downloads"`
	EULA       bool       `json:"eula,omitempty"`
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
	instance.LaunchSettings
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

// Downloads is where a wait for manual downloads looks besides a project's downloads/.
type Downloads struct {
	// Watch is nil when unset, which watches the home folder's Downloads, and empty to watch none.
	Watch *[]string `json:"watch,omitempty"`
}

// Watched is every folder to watch, with a leading ~ read as home. Unset, it is the OS's Downloads
// folder.
func (d Downloads) Watched(home string) []string {
	if d.Watch == nil {
		return []string{osDownloads(home, runtime.GOOS, os.Getenv("XDG_CONFIG_HOME"))}
	}
	dirs := make([]string, 0, len(*d.Watch))
	for _, dir := range *d.Watch {
		if rest, ok := strings.CutPrefix(dir, "~"); ok && (rest == "" || os.IsPathSeparator(rest[0])) {
			dir = home + rest
		}
		dirs = append(dirs, dir)
	}
	return dirs
}

// osDownloads is the user's Downloads folder: the one xdg-user-dirs names on Linux, where it is
// renamed with the desktop's language, and ~/Downloads everywhere else.
func osDownloads(home, goos, xdgConfig string) string {
	fallback := filepath.Join(home, "Downloads")
	if goos != "linux" {
		return fallback
	}
	if xdgConfig == "" {
		xdgConfig = filepath.Join(home, ".config")
	}
	data, err := os.ReadFile(filepath.Join(xdgConfig, "user-dirs.dirs"))
	if err != nil {
		return fallback
	}
	for line := range strings.Lines(string(data)) {
		value, ok := strings.CutPrefix(strings.TrimSpace(line), "XDG_DOWNLOAD_DIR=")
		if !ok {
			continue
		}
		value = strings.Trim(value, `"`)
		if rest, ok := strings.CutPrefix(value, "$HOME"); ok {
			return home + rest
		}
		if filepath.IsAbs(value) {
			return value
		}
	}
	return fallback
}

type CurseForge struct {
	Key string `json:"key"`
}

// Accounts is which accounts shulker can see and which one it uses by default. The default lives
// here rather than in accounts.json because a launcher account may be it, and shulker never writes
// another launcher's accounts into its own file.
type Accounts struct {
	// Stores is where accounts are read from, in the order the earliest one wins ties by UUID.
	Stores []string `json:"stores,omitempty"`
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

// RecordSync stamps how a sync ended. A failed one leaves LastSync where it is: the files in the
// directory are still the ones the last good sync built.
func (i *Instance) RecordSync(at, failure string) {
	if failure == "" {
		i.LastSync = at
	}
	i.LastError = failure
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
	return filepath.Join(base, "shulker", FileName), nil
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
	err := managed.Read(schema.Config, path, &cfg)
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, configInvalid(err)
	}
	return cfg, nil
}

// LoadDocument reads config.json as a raw document, numbers kept exact, so a key can be set without
// dropping the ones Config doesn't know. A file that isn't there is an empty document.
func LoadDocument(path string) (map[string]any, error) {
	doc := map[string]any{}
	err := managed.Read(schema.Config, path, &doc)
	if errors.Is(err, os.ErrNotExist) {
		return doc, nil
	}
	if err != nil {
		return nil, configInvalid(err)
	}
	if doc == nil {
		doc = map[string]any{}
	}
	return doc, nil
}

// configInvalid points a config this shulker can't read at the one command that replaces it.
func configInvalid(err error) error {
	if out.CodeOf(err) == "config-invalid" {
		e := out.AsError(err)
		e.Help = "run `shulker config set <key> <value>` to start a new one"
		return e
	}
	return err
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
	doc["$schema"] = schema.URL(schema.Config)
	if err := fsutil.WriteJSON(path, doc); err != nil {
		if created {
			os.Remove(path)
		}
		return err
	}
	return nil
}

// ReplaceDocument writes doc over a config.json that couldn't be read, keeping the old file as
// config.json.replaced, and returns where that went. The new file keeps the old one's mode.
func ReplaceDocument(path string, doc map[string]any) (kept string, err error) {
	doc["$schema"] = schema.URL(schema.Config)
	return managed.Replace(schema.Config, path, doc)
}

// AcceptEULA records in config.json that this user accepts the Minecraft EULA.
func AcceptEULA(path string) error {
	doc, err := LoadDocument(path)
	if err != nil {
		return err
	}
	doc["eula"] = true
	return SaveDocument(path, doc)
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
	var registry struct {
		Instances []Instance `json:"instances"`
	}
	err := managed.Read(schema.Registry, path, &registry)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, registryInvalid(err)
	}
	return registry.Instances, nil
}

// registryInvalid points a registry this shulker can't read at repair. A newer registry isn't
// pointed there: repair would write it back in this shulker's shape.
func registryInvalid(err error) error {
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

// SlugID is the id derived from an instance's name, matching manifest.IsValidKey so it is safe as a
// path segment and as the argument to -i.
func SlugID(name string) string {
	slug := strings.Trim(unsafeIDChars.ReplaceAllString(strings.ToLower(name), "-"), "-._")
	for slug != "" && !manifest.IsValidKey(slug) {
		slug = slug[1:]
	}
	if len(slug) > 64 {
		slug = strings.TrimRight(slug[:64], "-._")
	}
	if slug == "" {
		slug = "instance"
	}
	return slug
}

var unsafeIDChars = regexp.MustCompile(`[^a-z0-9._-]+`)

// InstanceID is the id a game directory gets: the one its registry row already holds, else the id
// asked for or a slug of its display name, suffixed until free across the registry the way history
// entries taken in the same second are suffixed. A new instance takes it as its manifest's name
// too, so the id a player types and the project they play are the same thing.
func InstanceID(instances []Instance, as, display, dir string) string {
	if as == "" {
		if i, ok := FindInstance(instances, dir); ok && instances[i].ID != "" {
			return instances[i].ID
		}
		as = SlugID(display)
	}
	if _, taken := IDTaken(instances, as, dir); !taken {
		return as
	}
	for n := 2; ; n++ {
		id := as + "-" + strconv.Itoa(n)
		if _, taken := IDTaken(instances, id, dir); !taken {
			return id
		}
	}
}

// IDTaken is the directory of another instance whose row already holds id; the instance at dir
// itself may hold it.
func IDTaken(instances []Instance, id, dir string) (heldBy string, taken bool) {
	if i, ok := FindID(instances, id); ok && !SameDir(instances[i].Dir, dir) {
		return instances[i].Dir, true
	}
	return "", false
}

// SameDir also treats a symlink to dir as dir, since a launcher's game directory may be reached
// through one.
func SameDir(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && ra == rb
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
	return managed.Replace(schema.Registry, path, doc)
}

// UpdateInstances rereads the registry right before writing and rewrites only the instances key, so
// another writer's entries and keys shulker doesn't know survive.
func UpdateInstances(path string, update func([]Instance) []Instance) (bool, error) {
	top := map[string]json.RawMessage{}
	err := managed.Read(schema.Registry, path, &top)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, registryInvalid(err)
	}
	var instances []Instance
	if raw, ok := top["instances"]; ok {
		if err := json.Unmarshal(raw, &instances); err != nil {
			return false, schema.Invalid("registry-invalid", path, raw, err)
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
