package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andrewmast/shulker/internal/out"
)

func TestLinkMojang(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack")
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
	if !strings.Contains(stdout, "Installed fabric-loader-0.17.3-26.2 into") || !strings.Contains(stdout, "Run `shulker install` before launching.") {
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

func TestLinkMojangErrors(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack")
	h.mustRun(t, "add", "sodium")

	code, stdout, _ := h.run(t, "link", "mojang", "--launcher-dir", filepath.Join(t.TempDir(), "missing"), "--json")
	if code == 0 || failureCode(t, stdout).Code != "launcher-not-found" {
		t.Fatalf("missing launcher: exit %d %s", code, stdout)
	}

	code, stdout, _ = h.run(t, "link", "mojang", "--launcher-dir", t.TempDir(), "--target", "nope", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "target-not-found" || strings.Join(e.Candidates, ",") != "client" {
		t.Fatalf("unknown target: exit %d %s", code, stdout)
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
