package launcher

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

const ProfilesFile = "launcher_profiles.json"

var ErrNotFound = errors.New("launcher not found")

type Mojang struct {
	Dir string
	Now func() time.Time
}

type Profile struct {
	Key       string
	Name      string
	VersionID string
	GameDir   string
}

func DefaultMojangDir() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support", "minecraft"), nil
	case "windows":
		appdata := os.Getenv("APPDATA")
		if appdata == "" {
			return "", errors.New("APPDATA is not set")
		}
		return filepath.Join(appdata, ".minecraft"), nil
	default:
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".minecraft"), nil
	}
}

func (v *Mojang) Check() error {
	info, err := os.Stat(v.Dir)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("%w at %s", ErrNotFound, v.Dir)
	}
	return nil
}

func (v *Mojang) InstallVersion(profile json.RawMessage) (string, error) {
	var head struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(profile, &head); err != nil {
		return "", fmt.Errorf("loader profile: %w", err)
	}
	if head.ID == "" {
		return "", errors.New("loader profile has no id")
	}
	dir := filepath.Join(v.Dir, "versions", head.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return head.ID, writeAtomic(filepath.Join(dir, head.ID+".json"), profile)
}

func (v *Mojang) WriteProfile(p Profile) error {
	path := filepath.Join(v.Dir, ProfilesFile)
	top := map[string]json.RawMessage{}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &top); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	}
	profiles := map[string]json.RawMessage{}
	if raw, ok := top["profiles"]; ok {
		if err := json.Unmarshal(raw, &profiles); err != nil {
			return fmt.Errorf("%s: profiles: %w", path, err)
		}
	}
	entry := map[string]any{}
	if raw, ok := profiles[p.Key]; ok {
		if err := json.Unmarshal(raw, &entry); err != nil {
			return fmt.Errorf("%s: profile %s: %w", path, p.Key, err)
		}
	}
	now := v.now().UTC().Format("2006-01-02T15:04:05.000Z")
	if _, ok := entry["created"]; !ok {
		entry["created"] = now
	}
	if _, ok := entry["icon"]; !ok {
		entry["icon"] = "Chest"
	}
	entry["name"] = p.Name
	entry["type"] = "custom"
	entry["lastVersionId"] = p.VersionID
	entry["gameDir"] = p.GameDir
	entry["lastUsed"] = now
	if profiles[p.Key], err = json.Marshal(entry); err != nil {
		return err
	}
	if top["profiles"], err = json.Marshal(profiles); err != nil {
		return err
	}
	out, err := json.MarshalIndent(top, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path, append(out, '\n'))
}

func (v *Mojang) now() time.Time {
	if v.Now != nil {
		return v.Now()
	}
	return time.Now()
}

func writeAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
