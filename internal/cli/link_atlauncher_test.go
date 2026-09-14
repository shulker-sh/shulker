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

func TestLinkATLauncher(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "my-pack")
	h.mustRun(t, "add", "sodium")

	launcherDir := t.TempDir()
	var env struct {
		Data prismReport `json:"data"`
	}
	stdout := h.mustRun(t, "link", "atlauncher", "--launcher-dir", launcherDir, "--name", "Friends SMP", "--json")
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatal(err)
	}
	instDir := filepath.Join(launcherDir, "instances", "FriendsSMP")
	if rep := env.Data; rep.Launcher != "atlauncher" || rep.InstanceDir != instDir || rep.GameDir != instDir || !rep.Created || rep.Sync != nil {
		t.Fatalf("link report: %+v", rep)
	}
	if len(h.installs) != 0 {
		t.Fatalf("Fabric needs no installer: %v", h.installs)
	}

	inst := readATLInstance(t, instDir)
	settings := inst["launcher"].(map[string]any)
	exe, _ := os.Executable()
	wantCmd := launcher.CommandArg(exe) + " sync " + launcher.CommandArg(h.dir) + ` --target client --into "$INST_MC_DIR"`
	if inst["id"] != "26.2" || inst["mainClass"] != "net.fabricmc.loader.impl.launch.knot.KnotClient" || settings["name"] != "Friends SMP" || settings["preLaunchCommand"] != wantCmd || settings["enableCommands"] != true {
		t.Fatalf("instance.json: %v", inst)
	}
	if lv := settings["loaderVersion"].(map[string]any); lv["type"] != "Fabric" || lv["version"] != "0.17.3" {
		t.Fatalf("loaderVersion: %v", lv)
	}
	libs := inst["libraries"].([]any)
	artifact := libs[0].(map[string]any)["downloads"].(map[string]any)["artifact"].(map[string]any)
	if artifact["path"] != "net/fabricmc/fabric-loader/0.17.3/fabric-loader-0.17.3.jar" || libs[len(libs)-1].(map[string]any)["name"] != "com.mojang:brigadier:1.3.10" {
		t.Fatalf("libraries should be the loader's, converted, then vanilla's: %v", libs)
	}
	if name, dir := launcher.Detect(instDir); name != "atlauncher" || dir != launcherDir {
		t.Fatalf("Detect = %q %q", name, dir)
	}

	settings["maximumMemory"] = 8192
	writeATLInstance(t, instDir, inst)
	h.mustRun(t, "sync", "--instance", "Friends SMP")
	if _, err := os.Stat(filepath.Join(instDir, "mods", h.jars["sodium"].filename)); err != nil {
		t.Fatalf("the pre-launch sync target should hold the mods: %v", err)
	}
	if st := build.LoadState(instDir); st.Source != h.dir {
		t.Fatalf("state origin: %+v", st.Origin)
	}

	stdout = h.mustRun(t, "link", "atlauncher", "--launcher-dir", launcherDir, "--name", "Friends SMP")
	if !strings.Contains(stdout, "updated instance Friends SMP") || !strings.Contains(stdout, "restart ATLauncher if it is open") {
		t.Fatalf("relink output:\n%s", stdout)
	}
	again := readATLInstance(t, instDir)
	if again["uuid"] != inst["uuid"] || again["launcher"].(map[string]any)["maximumMemory"] != float64(8192) {
		t.Fatalf("relink should keep the uuid and player settings: %v", again)
	}

	var listed struct {
		Data []linkEntry `json:"data"`
	}
	if err := json.Unmarshal([]byte(h.mustRun(t, "links", "--json")), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Data) != 1 || listed.Data[0].Launcher != "atlauncher" || listed.Data[0].Dir != instDir || listed.Data[0].Status != linkSynced {
		t.Fatalf("links: %+v", listed.Data)
	}

	statePath := filepath.Join(instDir, build.StateFile)
	state := map[string]any{}
	if err := json.Unmarshal([]byte(readFile(t, statePath)), &state); err != nil {
		t.Fatal(err)
	}
	state["source"] = "/elsewhere"
	data, _ := json.Marshal(state)
	if err := os.WriteFile(statePath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ := h.run(t, "link", "atlauncher", "--launcher-dir", launcherDir, "--name", "Friends SMP", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "instance-exists" || !strings.Contains(e.Message, "/elsewhere") {
		t.Fatalf("linking over another source: exit %d %s", code, stdout)
	}
	h.mustRun(t, "link", "atlauncher", "--launcher-dir", launcherDir, "--name", "Friends SMP", "--force")

	code, stdout, _ = h.run(t, "link", "atlauncher", "--launcher-dir", filepath.Join(launcherDir, "nope"), "--json")
	if code == 0 || failureCode(t, stdout).Code != "launcher-not-found" {
		t.Fatalf("missing launcher dir: exit %d %s", code, stdout)
	}

	h.mustRun(t, "unlink", "Friends SMP")
	after := readATLInstance(t, instDir)["launcher"].(map[string]any)
	if _, ok := after["preLaunchCommand"]; ok || after["maximumMemory"] != float64(8192) {
		t.Fatalf("unlink should drop only the sync: %v", after)
	}
}

func TestLinkATLauncherNeoForge(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack", "--loader", "neoforge")

	launcherDir := t.TempDir()
	h.mustRun(t, "link", "atlauncher", "--launcher-dir", launcherDir, "--name", "Neo")
	if len(h.installs) != 1 || h.installs[0][0] != "--install-client" || !strings.Contains(h.installs[0][1], filepath.Join("atlauncher", "neoforge-26.2.0.87")) {
		t.Fatalf("the installer should run into a scratch directory in the cache: %v", h.installs)
	}
	if data, err := os.ReadFile(filepath.Join(launcherDir, "libraries", "fake", "neoforge-26.2.0.87", "client-patched.jar")); err != nil || string(data) != "patched" {
		t.Fatalf("the installer's libraries should be copied into ATLauncher's: %q %v", data, err)
	}
	instDir := filepath.Join(launcherDir, "instances", "Neo")
	inst := readATLInstance(t, instDir)
	libs := inst["libraries"].([]any)
	if inst["id"] != "26.2" || libs[0].(map[string]any)["name"] != "net.neoforged:neoforge:26.2.0.87:universal" {
		t.Fatalf("instance.json: %v", inst)
	}
	if lv := inst["launcher"].(map[string]any)["loaderVersion"].(map[string]any); lv["type"] != "NeoForge" || lv["version"] != "26.2.0.87" {
		t.Fatalf("loaderVersion: %v", lv)
	}

	h.mustRun(t, "link", "atlauncher", "--launcher-dir", launcherDir, "--name", "Neo")
	if len(h.installs) != 1 {
		t.Fatalf("a relink should reuse the scratch install: %v", h.installs)
	}

	foreign := filepath.Join(launcherDir, "instances", "Mine")
	if err := os.MkdirAll(foreign, 0o755); err != nil {
		t.Fatal(err)
	}
	writeATLInstance(t, foreign, map[string]any{"launcher": map[string]any{"name": "Mine"}})
	code, stdout, _ := h.run(t, "link", "atlauncher", "--launcher-dir", launcherDir, "--name", "Mine", "--json")
	if code == 0 || failureCode(t, stdout).Code != "instance-exists" {
		t.Fatalf("linking over the player's own instance: exit %d %s", code, stdout)
	}
	h.mustRun(t, "link", "atlauncher", "--launcher-dir", launcherDir, "--name", "Mine", "--force")

	if r := unlinkJSON(t, h, "Neo"); len(r) != 1 || !strings.HasSuffix(r[0].Relink, " --force") {
		t.Fatalf("an instance that never synced needs --force to link again: %+v", r)
	}
}

func readATLInstance(t *testing.T, instDir string) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal([]byte(readFile(t, filepath.Join(instDir, launcher.ATLauncherInstanceFile))), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func writeATLInstance(t *testing.T, instDir string, v map[string]any) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(instDir, launcher.ATLauncherInstanceFile), data, 0o644); err != nil {
		t.Fatal(err)
	}
}
