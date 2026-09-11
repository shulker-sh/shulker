package launcher

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"shulker.sh/shulker/internal/fsutil"
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
	return head.ID, fsutil.Write(filepath.Join(dir, head.ID+".json"), profile)
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
	return fsutil.WriteJSON(path, top)
}

// RemoveProfiles drops the shulker-made profiles (keys starting "shulker-") that point at gameDir.
func (v *Mojang) RemoveProfiles(gameDir string) (int, error) {
	path := filepath.Join(v.Dir, ProfilesFile)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	top := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &top); err != nil {
		return 0, fmt.Errorf("%s: %w", path, err)
	}
	profiles := map[string]json.RawMessage{}
	if raw, ok := top["profiles"]; ok {
		if err := json.Unmarshal(raw, &profiles); err != nil {
			return 0, fmt.Errorf("%s: profiles: %w", path, err)
		}
	}
	removed := 0
	for key, raw := range profiles {
		var p struct {
			GameDir string `json:"gameDir"`
		}
		if strings.HasPrefix(key, "shulker-") && json.Unmarshal(raw, &p) == nil && p.GameDir != "" && filepath.Clean(p.GameDir) == filepath.Clean(gameDir) {
			delete(profiles, key)
			removed++
		}
	}
	if removed == 0 {
		return 0, nil
	}
	if top["profiles"], err = json.Marshal(profiles); err != nil {
		return 0, err
	}
	return removed, fsutil.WriteJSON(path, top)
}

func (v *Mojang) now() time.Time {
	if v.Now != nil {
		return v.Now()
	}
	return time.Now()
}
