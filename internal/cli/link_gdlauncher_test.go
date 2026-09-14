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
		Data prismReport `json:"data"`
	}
	stdout := h.mustRun(t, "link", "gdlauncher", "--launcher-dir", unresolved, "--name", "Friends: SMP", "--json")
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatal(err)
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
	if !strings.Contains(stdout, "updated instance Friends: SMP") || !strings.Contains(stdout, "restart GDLauncher if it is open") {
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

	h.mustRun(t, "unlink", "gdlauncher")
	after := readGDLInstance(t, instDir)
	if _, ok := after["pre_launch_hook"]; ok || after["game_configuration"].(map[string]any)["memory"] == nil {
		t.Fatalf("unlink should drop only the hook: %v", after)
	}
}

func TestLinkGDLauncherNeoForge(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack", "--loader", "neoforge")

	launcherDir := t.TempDir()
	h.mustRun(t, "link", "gdlauncher", "--launcher-dir", launcherDir, "--name", "Neo")
	if len(h.installs) != 0 {
		t.Fatalf("GDLauncher runs the NeoForge installer itself: %v", h.installs)
	}
	version := readGDLInstance(t, filepath.Join(launcherDir, "instances", "Neo"))["game_configuration"].(map[string]any)["version"].(map[string]any)
	if lv := version["modloaders"].([]any)[0].(map[string]any); version["release"] != "26.2" || lv["type"] != "Neoforge" || lv["version"] != "26.2.0.87" {
		t.Fatalf("version: %v", version)
	}
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
