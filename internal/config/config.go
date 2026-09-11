package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"shulker.sh/shulker/internal/out"
)

const PathEnv = "SHULKER_CONFIG"

type Config struct {
	CurseForge CurseForge `json:"curseforge"`
	Links      []Link     `json:"links,omitempty"`
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

func FindLink(links []Link, dir string) (int, bool) {
	dir = filepath.Clean(dir)
	for i, l := range links {
		if filepath.Clean(l.Dir) == dir {
			return i, true
		}
	}
	return -1, false
}

// UpdateLinks rereads the file right before writing and rewrites only the links key, so another
// writer's entries and keys shulker doesn't know survive.
func UpdateLinks(path string, update func([]Link) []Link) (bool, error) {
	top := map[string]json.RawMessage{}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &top); err != nil {
			return false, fmt.Errorf("%s: %w", path, err)
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
	out, err := json.MarshalIndent(top, "", "  ")
	if err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	return true, writeAtomic(path, append(out, '\n'))
}

func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
