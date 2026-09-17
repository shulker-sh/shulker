package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/out"
)

func TestLinkMojang(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")

	launcherDir := t.TempDir()
	existing := `{
  "profiles": {
    "other": {"name": "Other", "type": "custom", "lastVersionId": "26.1", "icon": "Grass"}
  },
  "settings": {"keepLauncherOpen": true},
  "version": 3
}
`
	if err := os.WriteFile(filepath.Join(launcherDir, "launcher_profiles.json"), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout := h.mustRun(t, "link", "vanilla", "--launcher-dir", launcherDir)
	if !strings.Contains(stdout, "installed fabric-loader-0.17.3-26.2 »") || !strings.Contains(stdout, "$ shulker install") {
		t.Fatalf("link output: %s", stdout)
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
	wantGameDir, _ := filepath.Abs(filepath.Join(h.dir, "build", "client"))
	if linked["gameDir"] != wantGameDir || linked["lastVersionId"] != "fabric-loader-0.17.3-26.2" || linked["type"] != "custom" || linked["name"] != "pack" {
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
	if env.Command != "link mojang" || env.Data.Profile != "shulker-pack" || env.Data.Target != "client" || env.Data.GameDir != wantGameDir {
		t.Fatalf("relink envelope: %s", stdout)
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
	if rep := env.Data; rep.Source != source || rep.Ref != "main" || rep.Name != "Friends" || rep.Profile != "shulker-friends" || rep.GameDir != gameDir || rep.Sync == nil {
		t.Fatalf("link report: %+v", rep)
	}
	if linked := readProfiles(t, launcherDir).Profiles["shulker-friends"]; linked["gameDir"] != gameDir || linked["name"] != "Friends" {
		t.Fatalf("linked profile: %v", linked)
	}
	if _, err := os.Stat(filepath.Join(gameDir, "mods", h.jars["sodium"].filename)); err != nil {
		t.Fatalf("the first sync should ship the mods: %v", err)
	}
	if st := build.LoadState(gameDir); st.Source != source || st.Ref != "main" {
		t.Fatalf("state origin: %+v", st.Origin)
	}
	if _, err := os.Stat(filepath.Join(h.dir, "build")); !os.IsNotExist(err) {
		t.Fatalf("linking a remote source must not build in the current directory: %v", err)
	}
	if instances := readInstances(t, h); len(instances) != 1 || instances[0].Launcher != "mojang" || instances[0].Dir != gameDir || instances[0].Source != source {
		t.Fatalf("registry: %+v", instances)
	}
	if f := readIntent(t, gameDir); f.Ref != "main" || f.Target != "client" {
		t.Fatalf("the ref and target live in the instance file: %+v", f)
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
	if r[0].Removed != launcher.RemovedProfile || r[0].Relink != "shulker link mojang "+source+" --ref main --target client --name Friends --launcher-dir "+launcherDir {
		t.Fatalf("unlink mojang: %+v", r[0])
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

	code, stdout, _ = h.run(t, "link", "mojang", "--launcher-dir", t.TempDir(), "--target", "nope", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "target-not-found" || strings.Join(e.Candidates, ",") != "client" {
		t.Fatalf("unknown target: exit %d %s", code, stdout)
	}

	h.mustRun(t, "target", "add", "server")
	code, stdout, _ = h.run(t, "link", "mojang", "--launcher-dir", t.TempDir(), "--target", "server", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "wrong-side-target" || strings.Join(e.Candidates, ",") != "client" {
		t.Fatalf("server target: exit %d %s", code, stdout)
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
