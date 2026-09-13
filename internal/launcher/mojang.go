package launcher

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
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

func (v *Mojang) profilesPath() string { return filepath.Join(v.Dir, ProfilesFile) }

func (v *Mojang) readProfiles() (top, profiles map[string]json.RawMessage, err error) {
	path := v.profilesPath()
	top, profiles = map[string]json.RawMessage{}, map[string]json.RawMessage{}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, nil, err
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &top); err != nil {
			return nil, nil, fmt.Errorf("%s: %w", path, err)
		}
	}
	if raw, ok := top["profiles"]; ok {
		if err := json.Unmarshal(raw, &profiles); err != nil {
			return nil, nil, fmt.Errorf("%s: profiles: %w", path, err)
		}
	}
	return top, profiles, nil
}

func (v *Mojang) writeProfiles(top, profiles map[string]json.RawMessage) error {
	raw, err := json.Marshal(profiles)
	if err != nil {
		return err
	}
	top["profiles"] = raw
	return fsutil.WriteJSON(v.profilesPath(), top)
}

func (v *Mojang) WriteProfile(p Profile) error {
	path := v.profilesPath()
	top, profiles, err := v.readProfiles()
	if err != nil {
		return err
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
	return v.writeProfiles(top, profiles)
}

// EnsureProfilesFile writes an empty launcher_profiles.json when the launcher has never run, because
// the NeoForge and Forge installers refuse a directory without one.
func (v *Mojang) EnsureProfilesFile() error {
	_, err := os.Stat(v.profilesPath())
	if err == nil {
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return v.writeProfiles(map[string]json.RawMessage{}, map[string]json.RawMessage{})
}

// Profiles snapshots launcher_profiles.json so RestoreProfiles can undo what an installer run
// writes into it.
func (v *Mojang) Profiles() (map[string]json.RawMessage, error) {
	_, profiles, err := v.readProfiles()
	return profiles, err
}

// RestoreProfiles puts the profiles back the way the snapshot had them — dropping what an installer
// added, restoring what it overwrote — and returns the lastVersionId of the entry it wrote, which
// is the version id it installed. The installers rewrite the whole file in their own layout, so
// profiles are compared as values, not bytes, and an entry the installer added outranks one it
// merely rewrote.
func (v *Mojang) RestoreProfiles(before map[string]json.RawMessage) (string, error) {
	top, profiles, err := v.readProfiles()
	if err != nil {
		return "", err
	}
	var added, changed []string
	for key, raw := range profiles {
		old, had := before[key]
		switch {
		case !had:
			added = append(added, key)
		case !sameJSON(old, raw):
			changed = append(changed, key)
		}
	}
	sort.Strings(added)
	sort.Strings(changed)
	touched := append(added, changed...)
	var versionID string
	for _, key := range touched {
		if versionID == "" {
			var p struct {
				LastVersionID string `json:"lastVersionId"`
			}
			if json.Unmarshal(profiles[key], &p) == nil {
				versionID = p.LastVersionID
			}
		}
		if old, had := before[key]; had {
			profiles[key] = old
		} else {
			delete(profiles, key)
		}
	}
	for key, old := range before {
		if _, ok := profiles[key]; !ok {
			profiles[key] = old
			touched = append(touched, key)
		}
	}
	if len(touched) == 0 {
		return versionID, nil
	}
	return versionID, v.writeProfiles(top, profiles)
}

func sameJSON(a, b json.RawMessage) bool {
	if bytes.Equal(a, b) {
		return true
	}
	var va, vb any
	if json.Unmarshal(a, &va) != nil || json.Unmarshal(b, &vb) != nil {
		return false
	}
	return reflect.DeepEqual(va, vb)
}

// RemoveProfiles drops the shulker-made profiles (keys starting "shulker-") that point at gameDir.
func (v *Mojang) RemoveProfiles(gameDir string) (int, error) {
	top, profiles, err := v.readProfiles()
	if err != nil {
		return 0, err
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
	return removed, v.writeProfiles(top, profiles)
}

func (v *Mojang) now() time.Time {
	if v.Now != nil {
		return v.Now()
	}
	return time.Now()
}
