package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/launcher"
)

func TestLinkGDLauncher(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "my-pack")
	h.mustRun(t, "add", "sodium")

	unresolved := t.TempDir()
	launcherDir, err := filepath.EvalSymlinks(unresolved)
	if err != nil {
		t.Fatal(err)
	}
	var env struct {
		Data     prismReport `json:"data"`
		Warnings []string    `json:"warnings"`
	}
	stdout := h.mustRun(t, "link", "gdlauncher", "--launcher-dir", unresolved, "--name", "Friends: SMP", "--json")
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatal(err)
	}
	if len(env.Warnings) != 0 {
		t.Fatalf("GDLauncher lists the locked Fabric loader: %q", env.Warnings)
	}
	instDir := filepath.Join(launcherDir, "instances", "Friends_ SMP")
	gameDir := filepath.Join(instDir, "instance")
	if rep := env.Data; rep.Launcher != "gdlauncher" || rep.InstanceDir != instDir || rep.GameDir != gameDir || !rep.Created || rep.Sync != nil {
		t.Fatalf("link report: %+v", rep)
	}
	if len(h.installs) != 0 {
		t.Fatalf("GDLauncher installs loaders itself: %v", h.installs)
	}

	inst := readGDLInstance(t, instDir)
	exe, _ := os.Executable()
	wantHook := launcher.GDLauncherHookArg(exe) + " sync " + launcher.GDLauncherHookArg(h.dir) + " --target client --into ."
	version := inst["game_configuration"].(map[string]any)["version"].(map[string]any)
	loaders := version["modloaders"].([]any)
	if inst["_version"] != "1" || inst["name"] != "Friends: SMP" || inst["pre_launch_hook"] != wantHook || inst["icon"] != launcher.GDLauncherIconFile || version["release"] != "26.2" {
		t.Fatalf("instance.json: %v", inst)
	}
	if lv := loaders[0].(map[string]any); len(loaders) != 1 || lv["type"] != "Fabric" || lv["version"] != "0.17.3" {
		t.Fatalf("modloaders: %v", loaders)
	}
	if name, dir := launcher.Detect(gameDir); name != "gdlauncher" || dir != launcherDir {
		t.Fatalf("Detect = %q %q", name, dir)
	}

	inst["game_configuration"].(map[string]any)["memory"] = map[string]any{"min_mb": 1024, "max_mb": 8192}
	inst["icon"] = "mine.png"
	writeGDLInstance(t, instDir, inst)
	t.Run("hook", func(t *testing.T) {
		t.Chdir(gameDir)
		h.mustRun(t, "sync", h.dir, "--target", "client", "--into", ".")
	})
	if _, err := os.Stat(filepath.Join(gameDir, "mods", h.jars["sodium"].filename)); err != nil {
		t.Fatalf("the pre-launch sync target should hold the mods: %v", err)
	}
	if st := build.LoadState(gameDir); st.Source != h.dir {
		t.Fatalf("state origin: %+v", st.Origin)
	}

	stdout = h.mustRun(t, "link", "gdlauncher", "--launcher-dir", launcherDir, "--name", "Friends: SMP")
	if !strings.Contains(stdout, "updated instance Friends: SMP") || strings.Contains(stdout, "quit GDLauncher") {
		t.Fatalf("relink output:\n%s", stdout)
	}
	again := readGDLInstance(t, instDir)
	if again["icon"] != "mine.png" || again["game_configuration"].(map[string]any)["memory"] == nil || again["pre_launch_hook"] != wantHook {
		t.Fatalf("relink should keep the player's icon and settings: %v", again)
	}

	var listed struct {
		Data []linkEntry `json:"data"`
	}
	if err := json.Unmarshal([]byte(h.mustRun(t, "links", "--json")), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Data) != 1 || listed.Data[0].Launcher != "gdlauncher" || listed.Data[0].Dir != gameDir || listed.Data[0].Status != linkSynced {
		t.Fatalf("links: %+v", listed.Data)
	}

	statePath := filepath.Join(gameDir, build.StateFile)
	state := map[string]any{}
	if err := json.Unmarshal([]byte(readFile(t, statePath)), &state); err != nil {
		t.Fatal(err)
	}
	state["source"] = "/elsewhere"
	data, _ := json.Marshal(state)
	if err := os.WriteFile(statePath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ := h.run(t, "link", "gdlauncher", "--launcher-dir", launcherDir, "--name", "Friends: SMP", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "instance-exists" || !strings.Contains(e.Message, "/elsewhere") {
		t.Fatalf("linking over another source: exit %d %s", code, stdout)
	}
	h.mustRun(t, "link", "gdlauncher", "--launcher-dir", launcherDir, "--name", "Friends: SMP", "--force")

	code, stdout, _ = h.run(t, "link", "gdlauncher", "--launcher-dir", filepath.Join(launcherDir, "nope"), "--json")
	if code == 0 || failureCode(t, stdout).Code != "launcher-not-found" {
		t.Fatalf("missing launcher dir: exit %d %s", code, stdout)
	}

	if stdout := h.mustRun(t, "unlink", "gdlauncher"); !strings.Contains(stdout, "removed its pre-launch sync") || strings.Contains(stdout, "Quit GDLauncher") || strings.Contains(stdout, "--force") {
		t.Fatalf("unlink output:\n%s", stdout)
	}
	after := readGDLInstance(t, instDir)
	if _, ok := after["pre_launch_hook"]; ok || after["game_configuration"].(map[string]any)["memory"] == nil {
		t.Fatalf("unlink should drop only the hook: %v", after)
	}
}

func TestLinkGDLauncherNeoForge(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack", "--loader", "neoforge")

	launcherDir := t.TempDir()
	instDir := filepath.Join(launcherDir, "instances", "Neo")
	warnings := gdlWarnings(t, h.mustRun(t, "link", "gdlauncher", "--launcher-dir", launcherDir, "--name", "Neo", "--json"))
	if len(h.installs) != 0 {
		t.Fatalf("GDLauncher runs the NeoForge installer itself: %v", h.installs)
	}
	if release, lv := gdlLoader(t, instDir); release != "26.2" || lv["type"] != "Neoforge" || lv["version"] != "26.2.0.82" {
		t.Fatalf("a loader GDLauncher can't install yet should give way to the newest it has: %s %v", release, lv)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "GDLauncher can't install neoforge 26.2.0.87 yet, so the instance uses 26.2.0.82") {
		t.Fatalf("warnings: %q", warnings)
	}

	warnings = gdlWarnings(t, h.mustRun(t, "link", "gdlauncher", "--launcher-dir", launcherDir, "--name", "Neo", "--force", "--json"))
	if _, lv := gdlLoader(t, instDir); lv["version"] != "26.2.0.87" {
		t.Fatalf("--force should keep the locked loader: %v", lv)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "without --force it would use 26.2.0.82") {
		t.Fatalf("warnings: %q", warnings)
	}
	h.mustRun(t, "link", "gdlauncher", "--launcher-dir", launcherDir, "--name", "Neo")

	foreign := filepath.Join(launcherDir, "instances", "Mine")
	if err := os.MkdirAll(foreign, 0o755); err != nil {
		t.Fatal(err)
	}
	writeGDLInstance(t, foreign, map[string]any{"_version": "1", "name": "Mine", "game_configuration": map[string]any{}})
	code, stdout, _ := h.run(t, "link", "gdlauncher", "--launcher-dir", launcherDir, "--name", "Mine", "--json")
	if code == 0 || failureCode(t, stdout).Code != "instance-exists" {
		t.Fatalf("linking over the player's own instance: exit %d %s", code, stdout)
	}
	h.mustRun(t, "link", "gdlauncher", "--launcher-dir", launcherDir, "--name", "Mine", "--force")

	if r := unlinkJSON(t, h, "Neo"); len(r) != 1 || !strings.HasSuffix(r[0].Relink, " --force") {
		t.Fatalf("an instance that never synced needs --force to link again: %+v", r)
	}
	code, stdout, _ = h.run(t, "link", "gdlauncher", "--launcher-dir", launcherDir, "--name", "Neo", "--json")
	if code == 0 || failureCode(t, stdout).Code != "instance-exists" {
		t.Fatalf("relinking without --force: exit %d %s", code, stdout)
	}
}

func TestLinkGDLauncherForge(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack", "--loader", "forge")

	launcherDir := t.TempDir()
	warnings := gdlWarnings(t, h.mustRun(t, "link", "gdlauncher", "--launcher-dir", launcherDir, "--name", "Forge", "--json"))
	if release, lv := gdlLoader(t, filepath.Join(launcherDir, "instances", "Forge")); release != "26.2" || lv["type"] != "Forge" || lv["version"] != "26.2-65.1.3" {
		t.Fatalf("GDLauncher's meta names Forge builds <game>-<build>: %s %v", release, lv)
	}
	if len(warnings) != 0 || len(h.installs) != 0 {
		t.Fatalf("warnings %q, installs %v", warnings, h.installs)
	}
}

func gdlWarnings(t *testing.T, stdout string) []string {
	t.Helper()
	var env struct {
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatal(err)
	}
	return env.Warnings
}

func gdlLoader(t *testing.T, instDir string) (release string, loader map[string]any) {
	t.Helper()
	version := readGDLInstance(t, instDir)["game_configuration"].(map[string]any)["version"].(map[string]any)
	loaders := version["modloaders"].([]any)
	if len(loaders) != 1 {
		t.Fatalf("modloaders: %v", loaders)
	}
	release, _ = version["release"].(string)
	return release, loaders[0].(map[string]any)
}

func readGDLInstance(t *testing.T, instDir string) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal([]byte(readFile(t, filepath.Join(instDir, launcher.GDLauncherInstanceFile))), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func writeGDLInstance(t *testing.T, instDir string, v map[string]any) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(instDir, launcher.GDLauncherInstanceFile), data, 0o644); err != nil {
		t.Fatal(err)
	}
}
