package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
)

// instanceManifest is the shulker.json a link leaves in a game directory, read as the map it
// was written as so a test can see the keys that are absent as well as the ones that are there.
func instanceManifest(t *testing.T, dir string) map[string]any {
	t.Helper()
	var m map[string]any
	readJSONFile(t, filepath.Join(dir, manifest.FileName), &m)
	return m
}

func onlyModpack(t *testing.T, m map[string]any) (string, map[string]any) {
	t.Helper()
	requires, _ := m["requires"].(map[string]any)
	if len(requires) != 1 {
		t.Fatalf("a link writes exactly one modpack entry: %v", requires)
	}
	for key, entry := range requires {
		e, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("requires.%s: %v", key, entry)
		}
		return key, e
	}
	return "", nil
}

// mojangGameDir is where `link mojang` puts an instance: the launcher's own folder for it.
func mojangGameDir(launcherDir, name string) string {
	return filepath.Join(launcherDir, "shulker", strings.TrimPrefix(launcher.InstanceKey(name), "shulker-"))
}

func mojangLauncherDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	profiles := `{
  "profiles": {
    "other": {"name": "Other", "type": "custom", "lastVersionId": "26.1", "icon": "Grass"}
  },
  "settings": {"keepLauncherOpen": true},
  "version": 3
}
`
	if err := os.WriteFile(filepath.Join(dir, "launcher_profiles.json"), []byte(profiles), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLinkMojang(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")

	launcherDir := mojangLauncherDir(t)
	stdout := h.mustRun(t, "link", "vanilla", "--launcher-dir", launcherDir)
	gameDir := filepath.Join(launcherDir, "shulker", "pack")
	if !strings.Contains(stdout, "installed fabric-loader-0.17.3-26.2 »") {
		t.Fatalf("link output: %s", stdout)
	}
	if !strings.Contains(stdout, "follows pack from "+h.dir) {
		t.Fatalf("the tree should name the pack the instance follows: %s", stdout)
	}
	if strings.Contains(stdout, "shulker install") {
		t.Fatalf("a link builds, so there is nothing left to nudge: %s", stdout)
	}

	m := instanceManifest(t, gameDir)
	client, _ := m["client"].(map[string]any)
	if m["name"] != "pack" || client["name"] != "pack" || client["build"] != "." {
		t.Fatalf("instance manifest: %v", m)
	}
	for _, key := range []string{"minecraft", "loader", "features", "history"} {
		if _, ok := m[key]; ok {
			t.Fatalf("an instance inherits %s from its pack: %v", key, m)
		}
	}
	key, entry := onlyModpack(t, m)
	if key != "pack" || entry["source"] != h.dir || entry["ref"] != nil {
		t.Fatalf("modpack entry %s: %v", key, entry)
	}

	if _, err := os.Stat(filepath.Join(gameDir, "mods", h.jars["sodium"].filename)); err != nil {
		t.Fatalf("a link builds the instance before it returns: %v", err)
	}
	if _, err := os.Stat(filepath.Join(h.dir, "build")); !os.IsNotExist(err) {
		t.Fatalf("a link must not build in the source project: %v", err)
	}

	versionPath := filepath.Join(launcherDir, "versions", "fabric-loader-0.17.3-26.2", "fabric-loader-0.17.3-26.2.json")
	var version struct {
		ID           string `json:"id"`
		InheritsFrom string `json:"inheritsFrom"`
	}
	readJSONFile(t, versionPath, &version)
	if version.ID != "fabric-loader-0.17.3-26.2" || version.InheritsFrom != "26.2" {
		t.Fatalf("version json: %+v", version)
	}

	profiles := readProfiles(t, launcherDir)
	if profiles.Version != 3 || !profiles.Settings["keepLauncherOpen"].(bool) {
		t.Fatalf("existing top-level fields were not preserved: %+v", profiles)
	}
	if other := profiles.Profiles["other"]; other["name"] != "Other" || other["icon"] != "Grass" {
		t.Fatalf("existing profile was not preserved: %v", other)
	}
	linked := profiles.Profiles["shulker-pack"]
	if linked == nil {
		t.Fatalf("no linked profile: %v", profiles.Profiles)
	}
	if linked["gameDir"] != gameDir || linked["lastVersionId"] != "fabric-loader-0.17.3-26.2" || linked["type"] != "custom" || linked["name"] != "pack" {
		t.Fatalf("linked profile: %v", linked)
	}
	created := linked["created"]

	profiles.Profiles["shulker-pack"]["icon"] = "Furnace"
	writeProfiles(t, launcherDir, profiles)

	code, stdout, _ := h.run(t, "link", "mojang", "--launcher-dir", launcherDir, "--json")
	if code != 0 {
		t.Fatalf("relink exit %d: %s", code, stdout)
	}
	var env struct {
		out.Envelope
		Data linkReport `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatal(err)
	}
	if env.Command != "link mojang" || env.Data.Instance != "shulker-pack" || env.Data.GameDir != gameDir {
		t.Fatalf("relink envelope: %s", stdout)
	}
	if env.Data.Modpack != "pack" || env.Data.Sync == nil || env.Data.Sync.Dir != gameDir {
		t.Fatalf("every link reports the pack it follows and the build it did: %s", stdout)
	}
	profiles = readProfiles(t, launcherDir)
	if len(profiles.Profiles) != 2 {
		t.Fatalf("relink should update in place, got %d profiles", len(profiles.Profiles))
	}
	linked = profiles.Profiles["shulker-pack"]
	if linked["icon"] != "Furnace" || linked["created"] != created {
		t.Fatalf("relink should keep user-edited fields: %v", linked)
	}
}

// A mod added in the game directory sits on top of the pack, which is the whole point of
// ADR 0001: a later sync relocks around it instead of sweeping it away.
func TestLinkMojangKeepsWhatThePlayerAdds(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")

	launcherDir := mojangLauncherDir(t)
	h.mustRun(t, "link", "mojang", "--launcher-dir", launcherDir)
	gameDir := filepath.Join(launcherDir, "shulker", "pack")

	h.mustRun(t, "-C", gameDir, "add", "fabric-api")
	h.mustRun(t, "-C", gameDir, "sync")
	for _, id := range []string{"sodium", "fabric-api"} {
		if _, err := os.Stat(filepath.Join(gameDir, "mods", h.jars[id].filename)); err != nil {
			t.Fatalf("%s should survive a sync: %v", id, err)
		}
	}
	requires, _ := instanceManifest(t, gameDir)["requires"].(map[string]any)
	pack, _ := requires["pack"].(map[string]any)
	if len(requires) != 2 || pack["source"] != h.dir || requires["fabric-api"] == nil {
		t.Fatalf("the player's own mod joins the modpack entry, it doesn't replace it: %v", requires)
	}
}

// --force repoints the one modpack entry and leaves the player's own requires alone: ADR 0001
// exists so what a player added survives the pack they follow changing.
func TestLinkMojangForceRepointsTheModpack(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	other := filepath.Join(t.TempDir(), "other")
	lockedPack(t, h, other, `"fabric-api": {}`)

	launcherDir := mojangLauncherDir(t)
	h.mustRun(t, "link", "mojang", "--launcher-dir", launcherDir)
	gameDir := mojangGameDir(launcherDir, "pack")
	h.mustRun(t, "-C", gameDir, "add", "fresh-animations")

	h.mustRun(t, "link", "mojang", other, "--launcher-dir", launcherDir, "--name", "pack", "--force")
	requires, _ := instanceManifest(t, gameDir)["requires"].(map[string]any)
	entry, _ := requires["pack"].(map[string]any)
	if len(requires) != 2 || entry["source"] != other || requires["fresh-animations"] == nil {
		t.Fatalf("--force repoints the pack and keeps the player's own entries: %v", requires)
	}
	if _, err := os.Stat(filepath.Join(gameDir, "mods", h.jars["fabric-api"].filename)); err != nil {
		t.Fatalf("the new pack's mods arrive: %v", err)
	}
	if _, err := os.Stat(filepath.Join(gameDir, "resourcepacks", "FreshAnimations_v1.9.4.zip")); err != nil {
		t.Fatalf("what the player added stays: %v", err)
	}
}

// A pack's retention and its marker are copied once, because the author is the one who knows how
// big a build is and whether the mod list has to match exactly. From then on both are the player's.
func TestLinkMojangCopiesThePacksPreferences(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.editManifest(t, func(m map[string]any) {
		m["history"] = 2
		m["marker"] = false
	})
	h.mustRun(t, "lock")

	launcherDir := mojangLauncherDir(t)
	h.mustRun(t, "link", "mojang", "--launcher-dir", launcherDir)
	gameDir := filepath.Join(launcherDir, "shulker", "pack")
	m := instanceManifest(t, gameDir)
	if n, ok := m["history"].(float64); !ok || n != 2 {
		t.Fatalf("the pack's retention should be copied into the instance: %v", m)
	}
	if marker, ok := m["marker"].(bool); !ok || marker {
		t.Fatalf("the pack's marker should be copied into the instance: %v", m)
	}
	if _, err := os.Stat(filepath.Join(gameDir, "mods", "shulker-pack.jar")); !os.IsNotExist(err) {
		t.Fatalf("a pack that leaves the marker out gives an instance that leaves it out: %v", err)
	}
}

func TestLinkMojangFromRemoteSource(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "my-pack")
	h.mustRun(t, "add", "sodium")
	gitRun(t, h.dir, "init", "-q", "-b", "main")
	gitRun(t, h.dir, "add", ".")
	gitRun(t, h.dir, "commit", "-q", "-m", "one")
	source := "file://" + h.dir

	launcherDir := t.TempDir()
	var env struct {
		Data linkReport `json:"data"`
	}
	stdout := h.mustRun(t, "link", "mojang", source, "--launcher-dir", launcherDir, "--name", "Friends", "--ref", "main", "--json")
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatal(err)
	}
	gameDir := filepath.Join(launcherDir, "shulker", "friends")
	if rep := env.Data; rep.Source != source || rep.Ref != "main" || rep.Name != "Friends" || rep.Instance != "shulker-friends" || rep.GameDir != gameDir || rep.Sync == nil {
		t.Fatalf("link report: %+v", rep)
	}
	if env.Data.Modpack != "my-pack" {
		t.Fatalf("a modpack is keyed by its manifest's name: %+v", env.Data)
	}
	if linked := readProfiles(t, launcherDir).Profiles["shulker-friends"]; linked["gameDir"] != gameDir || linked["name"] != "Friends" {
		t.Fatalf("linked profile: %v", linked)
	}

	m := instanceManifest(t, gameDir)
	client, _ := m["client"].(map[string]any)
	if m["name"] != "friends" || client["name"] != "Friends" || client["build"] != "." {
		t.Fatalf("instance manifest: %v", m)
	}
	key, entry := onlyModpack(t, m)
	if key != "my-pack" || entry["source"] != source || entry["ref"] != "main" {
		t.Fatalf("a git source is written as it was given: %s %v", key, entry)
	}

	if _, err := os.Stat(filepath.Join(gameDir, "mods", h.jars["sodium"].filename)); err != nil {
		t.Fatalf("the first build should ship the mods: %v", err)
	}
	if st := build.LoadState(gameDir); st.Source != gameDir {
		t.Fatalf("an instance builds from itself: %+v", st.Origin)
	}
	if _, err := os.Stat(filepath.Join(h.dir, "build")); !os.IsNotExist(err) {
		t.Fatalf("linking a remote source must not build in the current directory: %v", err)
	}
	if instances := readInstances(t, h); len(instances) != 1 || instances[0].ID != "friends" || instances[0].Launcher != "mojang" || instances[0].Dir != gameDir || instances[0].Source != source {
		t.Fatalf("registry: %+v", instances)
	}

	other := t.TempDir()
	if err := os.CopyFS(other, os.DirFS(h.dir)); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ := h.run(t, "link", "mojang", other, "--launcher-dir", launcherDir, "--name", "Friends", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "instance-exists" || !strings.Contains(e.Message, source) {
		t.Fatalf("linking another source into the profile: exit %d %s", code, stdout)
	}
	code, stdout, _ = h.run(t, "link", "mojang", "--launcher-dir", launcherDir, "--ref", "main", "--json")
	if code == 0 || failureCode(t, stdout).Code != "usage" {
		t.Fatalf("--ref without a source: exit %d %s", code, stdout)
	}

	r := unlinkJSON(t, h, "Friends", "--launcher", "mojang")
	if r[0].Removed != launcher.RemovedProfile || r[0].Relink != "shulker link mojang "+source+" --ref main --name Friends --launcher-dir "+launcherDir {
		t.Fatalf("unlink mojang: %+v", r[0])
	}
}

func TestLinkMojangFromManifestURL(t *testing.T) {
	h, dir := projectWithLockedPack(t, "base")
	srv := httptest.NewServer(http.FileServer(http.Dir(dir)))
	defer srv.Close()
	source := srv.URL + "/shulker.json"

	launcherDir := mojangLauncherDir(t)
	h.mustRun(t, "link", "mojang", source, "--launcher-dir", launcherDir, "--name", "Base")
	gameDir := filepath.Join(launcherDir, "shulker", "base")

	key, entry := onlyModpack(t, instanceManifest(t, gameDir))
	if key != "base" || entry["source"] != source {
		t.Fatalf("a manifest URL is written as it was given: %s %v", key, entry)
	}
	if _, err := os.Stat(filepath.Join(gameDir, "mods", h.jars["sodium"].filename)); err != nil {
		t.Fatalf("a URL source is locked, so the link can build it: %v", err)
	}
}

// A pack with no client block still contributes its shared mods and its overrides, so the link
// goes ahead and says why the instance is thinner than the pack.
func TestLinkMojangWarnsWhenTheSourceDeclaresNoClient(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "fabric-api")
	h.editManifest(t, func(m map[string]any) {
		delete(m, "client")
		m["server"] = map[string]any{"eula": true}
	})
	h.mustRun(t, "lock")

	launcherDir := mojangLauncherDir(t)
	_, stderr := h.mustRunStderr(t, "link", "mojang", "--launcher-dir", launcherDir)
	if !strings.Contains(stderr, noClientPack) {
		t.Fatalf("the warning explains the thin instance: %s", stderr)
	}
	if _, err := os.Stat(filepath.Join(launcherDir, "shulker", "pack", "mods", h.jars["fabric-api"].filename)); err != nil {
		t.Fatalf("a both-side mod still reaches the client: %v", err)
	}
}

func TestLinkMojangErrors(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")

	code, stdout, _ := h.run(t, "link", "mojang", "--launcher-dir", filepath.Join(t.TempDir(), "missing"), "--json")
	if code == 0 || failureCode(t, stdout).Code != "launcher-not-found" {
		t.Fatalf("missing launcher: exit %d %s", code, stdout)
	}
	if code, stdout, _ := h.run(t, "link", "mojang", "--launcher-dir", t.TempDir(), "--assume-client", "--json"); code == 0 || !strings.Contains(stdout, "assume-client") {
		t.Fatalf("an instance has a client block of its own, so the flag is gone: exit %d %s", code, stdout)
	}
}

func failureCode(t *testing.T, stdout string) *out.Error {
	t.Helper()
	var env out.Envelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("%v: %s", err, stdout)
	}
	if env.Error == nil {
		t.Fatalf("expected an error envelope: %s", stdout)
	}
	return env.Error
}

type launcherProfiles struct {
	Profiles map[string]map[string]any `json:"profiles"`
	Settings map[string]any            `json:"settings"`
	Version  int                       `json:"version"`
}

func readProfiles(t *testing.T, dir string) launcherProfiles {
	t.Helper()
	var p launcherProfiles
	readJSONFile(t, filepath.Join(dir, "launcher_profiles.json"), &p)
	return p
}

func writeProfiles(t *testing.T, dir string, p launcherProfiles) {
	t.Helper()
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "launcher_profiles.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func readJSONFile(t *testing.T, path string, v any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

func TestLinkMojangFreshDoesNotWarnAboutTheLock(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	_, stderr := h.mustRunStderr(t, "link", "vanilla", "--launcher-dir", mojangLauncherDir(t))
	if strings.Contains(stderr, "not in the lock") {
		t.Fatalf("a fresh link has no lock to be missing from: %s", stderr)
	}
}
