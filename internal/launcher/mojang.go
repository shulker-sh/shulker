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
	"shulker.sh/shulker/internal/out"
)

const ProfilesFile = "launcher_profiles.json"

var ErrNotFound = errors.New("launcher not found")

func appDataUnset() *out.Error {
	return out.Errorf("appdata-unset", "APPDATA is not set")
}

func invalidLoaderProfile(problem string) *out.Error {
	e := out.Errorf("loader-profile-invalid", "the loader profile isn't a version JSON shulker can install")
	e.Rows = []out.Detail{{Label: "profile", Text: problem}}
	return e
}

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
			return "", appDataUnset()
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

func (m *Mojang) Check() error {
	info, err := os.Stat(m.Dir)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("%w at %s", ErrNotFound, m.Dir)
	}
	return nil
}

func (m *Mojang) InstallVersion(profile json.RawMessage) (string, error) {
	var head struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(profile, &head); err != nil {
		return "", invalidLoaderProfile(err.Error())
	}
	if head.ID == "" {
		return "", invalidLoaderProfile("no id")
	}
	dir := filepath.Join(m.Dir, "versions", head.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return head.ID, fsutil.Write(filepath.Join(dir, head.ID+".json"), profile)
}

func (m *Mojang) profilesPath() string { return filepath.Join(m.Dir, ProfilesFile) }

func (m *Mojang) readProfiles() (top, profiles map[string]json.RawMessage, err error) {
	path := m.profilesPath()
	top = map[string]json.RawMessage{}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, nil, err
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &top); err != nil {
			return nil, nil, invalidFile(path, err)
		}
	}
	if profiles, err = jsonObjectAt(path, top, "profiles"); err != nil {
		return nil, nil, err
	}
	return top, profiles, nil
}

func (m *Mojang) writeProfiles(top, profiles map[string]json.RawMessage) error {
	raw, err := json.Marshal(profiles)
	if err != nil {
		return err
	}
	top["profiles"] = raw
	return fsutil.WriteJSON(m.profilesPath(), top)
}

func (m *Mojang) WriteProfile(p Profile) error {
	path := m.profilesPath()
	top, profiles, err := m.readProfiles()
	if err != nil {
		return err
	}
	entry := map[string]any{}
	if raw, ok := profiles[p.Key]; ok {
		if err := json.Unmarshal(raw, &entry); err != nil {
			return invalidFile(path, fmt.Errorf("profile %s: %w", p.Key, err))
		}
	}
	now := m.now().UTC().Format("2006-01-02T15:04:05.000Z")
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
	return m.writeProfiles(top, profiles)
}

// EnsureProfilesFile writes an empty launcher_profiles.json when the launcher has never run, because
// the NeoForge and Forge installers refuse a directory without one.
func (m *Mojang) EnsureProfilesFile() error {
	_, err := os.Stat(m.profilesPath())
	if err == nil {
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return m.writeProfiles(map[string]json.RawMessage{}, map[string]json.RawMessage{})
}

// Profiles snapshots launcher_profiles.json so RestoreProfiles can undo what an installer run
// writes into it.
func (m *Mojang) Profiles() (map[string]json.RawMessage, error) {
	_, profiles, err := m.readProfiles()
	return profiles, err
}

// RestoreProfiles puts the profiles back the way the snapshot had them — dropping what an installer
// added, restoring what it overwrote — and returns the lastVersionId of the entry it wrote, which
// is the version id it installed. The installers rewrite the whole file in their own layout, so
// profiles are compared as values, not bytes, and an entry the installer added outranks one it
// merely rewrote.
func (m *Mojang) RestoreProfiles(before map[string]json.RawMessage) (string, error) {
	top, profiles, err := m.readProfiles()
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
	return versionID, m.writeProfiles(top, profiles)
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

// RemoveProfiles drops the shulker-made profiles that point at gameDir.
func (m *Mojang) RemoveProfiles(gameDir string) (int, error) {
	top, profiles, err := m.readProfiles()
	if err != nil {
		return 0, err
	}
	keys := shulkerProfiles(profiles, gameDir)
	for _, key := range keys {
		delete(profiles, key)
	}
	if len(keys) == 0 {
		return 0, nil
	}
	return len(keys), m.writeProfiles(top, profiles)
}

// JavaDir is the Java the launcher runs for a game directory, and whether shulker has a profile
// there at all. An empty path is a profile with no javaDir, which is the launcher's own runtime.
func (m *Mojang) JavaDir(gameDir string) (string, bool, error) {
	_, profiles, err := m.readProfiles()
	if err != nil {
		return "", false, err
	}
	keys := shulkerProfiles(profiles, gameDir)
	for _, key := range keys {
		var p struct {
			JavaDir string `json:"javaDir"`
		}
		if json.Unmarshal(profiles[key], &p) == nil && p.JavaDir != "" {
			return p.JavaDir, true, nil
		}
	}
	return "", len(keys) > 0, nil
}

// SetJavaDir points every shulker profile for a game directory at a Java, an empty one deleting the
// key so the launcher goes back to choosing the runtime itself.
func (m *Mojang) SetJavaDir(gameDir, javaDir string) error {
	top, profiles, err := m.readProfiles()
	if err != nil {
		return err
	}
	changed := false
	for _, key := range shulkerProfiles(profiles, gameDir) {
		entry := map[string]any{}
		if err := json.Unmarshal(profiles[key], &entry); err != nil {
			return invalidFile(m.profilesPath(), fmt.Errorf("profile %s: %w", key, err))
		}
		if current, _ := entry["javaDir"].(string); current == javaDir {
			continue
		}
		changed = true
		if javaDir == "" {
			delete(entry, "javaDir")
		} else {
			entry["javaDir"] = javaDir
		}
		if profiles[key], err = json.Marshal(entry); err != nil {
			return err
		}
	}
	if !changed {
		return nil
	}
	return m.writeProfiles(top, profiles)
}

// shulkerProfiles are the profiles shulker made for a game directory: the ones `link mojang` writes,
// which it knows by their key, and which are the only ones it ever changes or removes.
func shulkerProfiles(profiles map[string]json.RawMessage, gameDir string) []string {
	var keys []string
	for key, raw := range profiles {
		if dir, ok := shulkerProfileGameDir(key, raw); ok && filepath.Clean(dir) == filepath.Clean(gameDir) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

// shulkerProfileGameDir is the game directory of a profile shulker made, and whether it is one.
func shulkerProfileGameDir(key string, raw json.RawMessage) (string, bool) {
	if !strings.HasPrefix(key, "shulker-") {
		return "", false
	}
	var p struct {
		GameDir string `json:"gameDir"`
	}
	if json.Unmarshal(raw, &p) != nil || p.GameDir == "" {
		return "", false
	}
	return p.GameDir, true
}

func (m *Mojang) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}
