package launcher

import (
	"bytes"
	"context"
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

	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/out"
)

const ProfilesFile = "launcher_profiles.json"

func appDataUnset() *out.Error {
	return out.Errorf("appdata-unset", "APPDATA is not set")
}

func invalidLoaderProfile(problem string) *out.Error {
	e := out.Errorf("loader-profile-invalid", "the loader profile isn't a version JSON shulker can install")
	e.Rows = []out.Detail{{Label: "profile", Text: problem}}
	return e
}

// mojangEntry is the official launcher: a profile shulker-<slug> whose game directory is
// shulker/<slug> under the launcher directory, started through the shim in the profile's javaDir.
var mojangEntry = &Entry{
	Name: "mojang", Title: "Minecraft Launcher", DefaultDir: DefaultMojangDir,
	Slot: &Slot{UsesShim: true}, NeedsRuntime: true,
	Usage: Usage{
		Short:   "Add a profile for the client build to the official launcher, installing its loader if it has one",
		Aliases: []string{"vanilla"},
		Noun:    "profile",
		Dir:     "launcher directory (default: the official launcher's .minecraft folder)",
		Names:   true,
		Force:   "repoint the modpack a profile already follows",
	},
	Accounts: mojangAccounts,
	relink:   relinkLauncher, forget: forgetMojang, name: mojangName, gameDirs: mojangGameDirs,
	readSlots: readMojangSlots, writeSlots: writeMojangSlots,
	place: placeMojang, link: linkMojang,
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

func forgetMojang(e *Entry, l config.Instance) (Forgotten, error) {
	// Unlink takes the generated scripts and the shim with the profile, and puts the profile's own
	// Java back on the way.
	if _, _, err := ReleaseSlots(e, l); err != nil {
		return Forgotten{}, err
	}
	n, err := (&Mojang{Dir: l.LauncherDir}).RemoveProfiles(l.Dir)
	if err != nil {
		return Forgotten{}, err
	}
	if n == 0 {
		return Forgotten{Summary: fmt.Sprintf("Unlinked %q (%s); it had no launcher profile left.", l.Label(), e.Title)}, nil
	}
	return Forgotten{
		Removed: RemovedProfile,
		Summary: fmt.Sprintf("Unlinked %q (%s): removed its launcher profile; the instance directory and the loader stay.", l.Label(), e.Title),
	}, nil
}

// mojangName reads the profile rather than the instance: the official launcher keeps no instance of
// its own, so the name lives beside the gameDir that points here.
func mojangName(_ *Entry, launcherDir, gameDir string) string {
	if launcherDir == "" {
		return ""
	}
	_, profiles, err := (&Mojang{Dir: launcherDir}).readProfiles()
	if err != nil {
		return ""
	}
	for _, key := range shulkerProfiles(profiles, gameDir) {
		var p struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(profiles[key], &p) == nil && p.Name != "" {
			return p.Name
		}
	}
	return ""
}

// mojangGameDirs reads the game directories of the profiles shulker wrote, since the official
// launcher keeps no instances of its own and a profile can point anywhere.
func mojangGameDirs(_ *Entry, launcherDir string) []string {
	profiles, err := (&Mojang{Dir: launcherDir}).Profiles()
	if err != nil {
		return nil
	}
	var dirs []string
	for key, raw := range profiles {
		dir, ok := shulkerProfileGameDir(key, raw)
		if !ok {
			continue
		}
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

// placeMojang puts the game directory under shulker/ in the launcher directory, named after the
// profile key without its prefix, whatever the source.
func placeMojang(_ *Entry, req *Link) (Placement, error) {
	dir := filepath.Join(req.LauncherDir, "shulker", strings.TrimPrefix(InstanceKey(req.Name), "shulker-"))
	return Placement{ID: req.ID, Dir: dir, GameDir: dir}, nil
}

// linkMojang installs the version the profile starts, then writes the profile pointing at the game
// directory.
func linkMojang(ctx context.Context, _ *Entry, req *Link, p Placement) (InstanceResult, error) {
	m := &Mojang{Dir: req.LauncherDir}
	key := InstanceKey(req.Name)
	res := InstanceResult{Dir: p.Dir, GameDir: p.Dir, Key: key}
	profiles, err := m.Profiles()
	if err != nil {
		return res, err
	}
	_, exists := profiles[key]
	res.Created = !exists
	versionID, err := mojangVersion(ctx, m, req)
	if err != nil {
		return res, err
	}
	if err := m.WriteProfile(Profile{Key: key, Name: req.Name, VersionID: versionID, GameDir: p.Dir}); err != nil {
		return res, err
	}
	if req.LoaderType != "" {
		res.Version, res.VersionDir = versionID, filepath.Join(m.Dir, "versions")
	}
	return res, nil
}

// mojangVersion is the version id the profile starts: vanilla's for a project with no loader, and
// the loader's otherwise, installed from its profile or by its own installer.
func mojangVersion(ctx context.Context, m *Mojang, req *Link) (string, error) {
	switch {
	case req.LoaderType == "":
		return req.Minecraft, nil
	case req.Versions.HasInstaller():
		return req.Versions.InstallClient(ctx, m.Dir)
	}
	req.Log("fetching %s loader %s for %s", req.LoaderType, req.LoaderVersion, req.Minecraft)
	profile, err := req.Versions.LoaderProfile(ctx)
	if err != nil {
		return "", err
	}
	return m.InstallVersion(profile)
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
