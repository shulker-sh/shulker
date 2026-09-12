package config

import (
	"encoding/json"
	"errors"
	"fmt"
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
	var registry struct {
		Links []Link `json:"links"`
	}
	if err := json.Unmarshal(data, &registry); err != nil {
		return nil, out.Errorf("registry-invalid", "%s: %v", path, err)
	}
	return registry.Links, nil
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
	if len(data) > 0 {
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
