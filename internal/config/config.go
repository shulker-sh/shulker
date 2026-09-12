package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/out"
)

const (
	PathEnv          = "SHULKER_CONFIG"
	RegistryFileName = "registry.json"
)

var Keys = []string{"curseforge.key", "registry"}

type Config struct {
	CurseForge CurseForge `json:"curseforge"`
	Registry   string     `json:"registry,omitempty"`
}

type CurseForge struct {
	Key string `json:"key"`
}

type Link struct {
	Launcher    string `json:"launcher,omitempty"`
	LauncherDir string `json:"launcherDir,omitempty"`
	Side        string `json:"side"`
	Name        string `json:"name"`
	Dir         string `json:"dir"`
	Source      string `json:"source"`
	Target      string `json:"target"`
	Ref         string `json:"ref,omitempty"`
}

func Path() (string, error) {
	if p := os.Getenv(PathEnv); p != "" {
		return p, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "shulker", "config.json"), nil
}

func Load() (Config, error) {
	path, err := Path()
	if err != nil {
		return Config{}, err
	}
	return LoadFile(path)
}

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
		return cfg, out.Errorf("config-invalid", "%s: %v", path, err)
	}
	return cfg, nil
}

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
		return nil, out.Errorf("config-invalid", "%s: %v", path, err)
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

func RegistryPath(configPath string, cfg Config) string {
	switch {
	case cfg.Registry == "":
		return filepath.Join(filepath.Dir(configPath), RegistryFileName)
	case filepath.IsAbs(cfg.Registry):
		return cfg.Registry
	default:
		return filepath.Join(filepath.Dir(configPath), cfg.Registry)
	}
}

func LoadLinks(path string) ([]Link, error) {
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
	var registry struct {
		Links []Link `json:"links"`
	}
	if err := json.Unmarshal(data, &registry); err != nil {
		return nil, out.Errorf("registry-invalid", "%s: %v", path, err)
	}
	return registry.Links, nil
}

// CreateRegistry writes an empty registry at path when nothing is there yet.
func CreateRegistry(path string) (bool, error) {
	if _, err := os.Stat(path); err == nil || !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	return true, fsutil.WriteJSON(path, map[string]any{})
}

func FindLink(links []Link, dir string) (int, bool) {
	dir = filepath.Clean(dir)
	for i, l := range links {
		if filepath.Clean(l.Dir) == dir {
			return i, true
		}
	}
	return -1, false
}

// UpdateLinks rereads the registry right before writing and rewrites only the links key, so another
// writer's entries and keys shulker doesn't know survive.
func UpdateLinks(path string, update func([]Link) []Link) (bool, error) {
	top := map[string]json.RawMessage{}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if len(bytes.TrimSpace(data)) > 0 {
		if err := json.Unmarshal(data, &top); err != nil {
			return false, out.Errorf("registry-invalid", "%s: %v", path, err)
		}
	}
	var links []Link
	if raw, ok := top["links"]; ok {
		if err := json.Unmarshal(raw, &links); err != nil {
			return false, fmt.Errorf("%s: links: %w", path, err)
		}
	}
	next := update(slices.Clone(links))
	if slices.Equal(links, next) {
		return false, nil
	}
	if len(next) == 0 {
		delete(top, "links")
	} else if top["links"], err = json.Marshal(next); err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	return true, fsutil.WriteJSON(path, top)
}
