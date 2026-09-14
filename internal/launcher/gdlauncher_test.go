package launcher

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestGDLauncherFolder(t *testing.T) {
	for name, want := range map[string]string{
		"Friends SMP":     "Friends SMP",
		"  Pack  ":        "Pack",
		`a/b:c\d<e>*|"?^`: "a_b_c_d_e______",
		".hidden~":        "_hidden_",
		"CON":             "_CON",
		" con":            "con",
		"Café":            "Café",
	} {
		if got := GDLauncherFolder(name); got != want {
			t.Errorf("GDLauncherFolder(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestGDLauncherHookArg(t *testing.T) {
	if got := gdlauncherHookArg(`/Users/me/My "Packs"/shulker`, "darwin"); got != `"/Users/me/My \"Packs\"/shulker"` {
		t.Errorf("posix: %s", got)
	}
	if got := gdlauncherHookArg(`C:\Program Files\shulker\shulker.exe`, "windows"); got != `"C:\Program Files\shulker\shulker.exe"` {
		t.Errorf("windows: %s", got)
	}
	if !IsSyncCommand(`"/bin/shulker" sync "/pack" --target client --into .`) {
		t.Error("a GDLauncher hook should read as a shulker sync")
	}
}

func TestDefaultGDLauncherDirOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("APPDATA", home)
	appData, err := gdlauncherAppData()
	if err != nil {
		t.Fatal(err)
	}
	if dir, err := DefaultGDLauncherDir(); err != nil || dir != filepath.Join(appData, "data") {
		t.Fatalf("default: %q %v", dir, err)
	}
	if err := os.MkdirAll(appData, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appData, "runtime_path_override"), []byte("/Games/GDLauncher"), 0o644); err != nil {
		t.Fatal(err)
	}
	if dir, err := DefaultGDLauncherDir(); err != nil || dir != "/Games/GDLauncher" {
		t.Fatalf("override: %q %v", dir, err)
	}
}

func TestGDLauncherWriteInstanceKeepsSettings(t *testing.T) {
	l := &GDLauncher{Dir: t.TempDir()}
	inst := GDLauncherInstance{Name: "Pack", Minecraft: "26.2", LoaderType: "neoforge", LoaderVersion: "26.2.0.87", PreLaunch: `"/bin/shulker" sync "/pack" --target client --into .`}
	res, err := l.WriteInstance(inst)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(l.Dir, "instances", "Pack"); res.Dir != want || res.GameDir != filepath.Join(want, "instance") || !res.Created {
		t.Fatalf("result: %+v", res)
	}
	if info, err := os.Stat(res.GameDir); err != nil || !info.IsDir() {
		t.Fatalf("game dir: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(res.Dir, GDLauncherIconFile)); err != nil || !bytes.Equal(data, GDLauncherIcon) {
		t.Fatalf("new instance should get the shulker icon: %v", err)
	}
	path := filepath.Join(res.Dir, GDLauncherInstanceFile)
	first := readInstanceJSON(t, path)
	if first["_version"] != "1" || first["icon"] != GDLauncherIconFile {
		t.Fatalf("instance.json: %v", first)
	}
	first["game_configuration"].(map[string]any)["memory"] = map[string]any{"min_mb": 1024, "max_mb": 8192}
	first["icon"] = "mine.png"
	first["seconds_played"] = 60
	first["post_exit_hook"] = "echo bye"
	data, _ := json.Marshal(first)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	inst.Minecraft, inst.LoaderVersion = "26.3", "26.3.0.1"
	if res, err = l.WriteInstance(inst); err != nil || res.Created {
		t.Fatalf("relink: %+v %v", res, err)
	}
	again := readInstanceJSON(t, path)
	config := again["game_configuration"].(map[string]any)
	version := config["version"].(map[string]any)
	loader := version["modloaders"].([]any)[0].(map[string]any)
	if version["release"] != "26.3" || loader["type"] != "Neoforge" || loader["version"] != "26.3.0.1" {
		t.Fatalf("version: %v", version)
	}
	if config["memory"] == nil || again["icon"] != "mine.png" || again["seconds_played"] != float64(60) || again["post_exit_hook"] != "echo bye" {
		t.Fatalf("relink should keep the player's settings: %v", again)
	}

	if removed, err := RemoveGDLauncherPreLaunch(res.Dir); err != nil || !removed {
		t.Fatalf("remove: %v %v", removed, err)
	}
	after := readInstanceJSON(t, path)
	if _, ok := after["pre_launch_hook"]; ok || after["post_exit_hook"] != "echo bye" {
		t.Fatalf("after unlink: %v", after)
	}

	after["pre_launch_hook"] = "./prepare.sh"
	data, _ = json.Marshal(after)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if removed, err := RemoveGDLauncherPreLaunch(res.Dir); err != nil || removed {
		t.Fatalf("a hook that isn't shulker's should stay: %v %v", removed, err)
	}
}

func TestGDLauncherForgeVersion(t *testing.T) {
	l := &GDLauncher{Dir: t.TempDir()}
	res, err := l.WriteInstance(GDLauncherInstance{Name: "Forge", Minecraft: "26.2", LoaderType: "forge", LoaderVersion: "65.1.3"})
	if err != nil {
		t.Fatal(err)
	}
	version := readInstanceJSON(t, filepath.Join(res.Dir, GDLauncherInstanceFile))["game_configuration"].(map[string]any)["version"].(map[string]any)
	if lv := version["modloaders"].([]any)[0].(map[string]any); lv["type"] != "Forge" || lv["version"] != "26.2-65.1.3" {
		t.Fatalf("GDLauncher's meta names Forge builds <game>-<build>: %v", version)
	}
}

func TestGDLauncherRelinkLeavesDefaultIcon(t *testing.T) {
	l := &GDLauncher{Dir: t.TempDir()}
	inst := GDLauncherInstance{Name: "Pack", Minecraft: "26.2", LoaderType: "fabric", LoaderVersion: "0.17.3"}
	res, err := l.WriteInstance(inst)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(res.Dir, GDLauncherInstanceFile)
	reset := readInstanceJSON(t, path)
	reset["icon"] = nil
	data, _ := json.Marshal(reset)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := l.WriteInstance(inst); err != nil {
		t.Fatal(err)
	}
	if got := readInstanceJSON(t, path); got["icon"] != nil {
		t.Fatalf("a relink should leave an icon reset to default alone: %v", got)
	}
}
