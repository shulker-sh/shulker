package launcher

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSlotCommandReachesTheScriptThroughTheLaunchersToken(t *testing.T) {
	for _, tc := range []struct {
		launcher, dir, goos, want string
	}{
		{"prism", "/games/cozy/.minecraft", "darwin", `sh "$INST_MC_DIR/.shulker/pre-launch"`},
		{"multimc", "/games/cozy/minecraft", "darwin", `sh "$INST_MC_DIR/.shulker/pre-launch"`},
		{"atlauncher", "/games/cozy", "darwin", `sh "$INST_DIR/.shulker/pre-launch"`},
		{"gdlauncher", "/games/cozy/instance", "darwin", `sh "/games/cozy/instance/.shulker/pre-launch"`},
		{"gdlauncher", `C:\games\cozy\instance`, "windows", `cmd /c "C:\games\cozy\instance\.shulker\pre-launch.cmd"`},
		{"prism", `C:\games\cozy\.minecraft`, "windows", `cmd /c "$INST_MC_DIR\.shulker\pre-launch.cmd"`},
	} {
		if got := slotCommand(tc.launcher, tc.dir, HookPreLaunch, tc.goos); got != tc.want {
			t.Fatalf("%s on %s: got %q, want %q", tc.launcher, tc.goos, got, tc.want)
		}
		if !IsShulkerSlot(tc.want) {
			t.Fatalf("shulker must recognise its own slot: %q", tc.want)
		}
	}
	// GDLauncher substitutes nothing, so its slot carries the path it was linked at.
	if got := slotCommand("gdlauncher", "/games/cozy/instance", HookPostExit, "darwin"); got != `sh "/games/cozy/instance/.shulker/post-exit"` {
		t.Fatalf("post-exit slot: %q", got)
	}
	if IsShulkerSlot(`/usr/bin/say "launching"`) {
		t.Fatal("a player's own command must not read as shulker's")
	}
}

func TestATLauncherSlotsToggleEnableCommands(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, ATLauncherInstanceFile), `{"name":"Cozy","launcher":{"maximumMemory":4096}}`)
	e := Find("atlauncher")
	if err := WriteSlots(e, dir, Slots{PreLaunch: "sh pre", PostExit: "sh post"}); err != nil {
		t.Fatal(err)
	}
	got := atlauncherFixture(t, dir)
	if got.Launcher.Enable == nil || !*got.Launcher.Enable {
		t.Fatalf("reconcile must turn commands on per instance, or they never run: %+v", got)
	}
	if got.Launcher.Pre != "sh pre" || got.Launcher.Post != "sh post" || got.Launcher.Memory != 4096 || got.Name != "Cozy" {
		t.Fatalf("slots written, settings kept: %+v", got)
	}
	if slots, found, err := ReadSlots(e, dir); err != nil || !found || slots.PreLaunch != "sh pre" || slots.PostExit != "sh post" {
		t.Fatalf("read back: %+v found=%v err=%v", slots, found, err)
	}
	if err := WriteSlots(e, dir, Slots{}); err != nil {
		t.Fatal(err)
	}
	got = atlauncherFixture(t, dir)
	if got.Launcher.Enable != nil || got.Launcher.Pre != "" || got.Launcher.Post != "" {
		t.Fatalf("clearing both slots drops the switch too: %+v", got)
	}
	if got.Launcher.Memory != 4096 {
		t.Fatalf("clearing slots must not touch the player's settings: %+v", got)
	}
}

func TestPrismSlotsWriteOverrideCommands(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, InstanceConfigFile), "[General]\nConfigVersion=1.3\nname=Cozy\n")
	e := Find("prism")
	if err := WriteSlots(e, dir, Slots{PreLaunch: "sh pre", PostExit: "sh post"}); err != nil {
		t.Fatal(err)
	}
	values, err := readINI(filepath.Join(dir, InstanceConfigFile), false)
	if err != nil {
		t.Fatal(err)
	}
	if values["OverrideCommands"] != "true" || values["PreLaunchCommand"] != "sh pre" || values["PostExitCommand"] != "sh post" {
		t.Fatalf("Prism ignores commands without the override: %+v", values)
	}
	if values["name"] != "Cozy" || values["ConfigVersion"] != "1.3" {
		t.Fatalf("the instance's own keys must survive: %+v", values)
	}
	if err := WriteSlots(e, dir, Slots{PreLaunch: "sh pre"}); err != nil {
		t.Fatal(err)
	}
	values, err = readINI(filepath.Join(dir, InstanceConfigFile), false)
	if err != nil {
		t.Fatal(err)
	}
	if _, has := values["PostExitCommand"]; has || values["PreLaunchCommand"] != "sh pre" {
		t.Fatalf("one slot cleared, the other kept: %+v", values)
	}
}

func TestGDLauncherSlotsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, GDLauncherInstanceFile), `{"name":"Cozy","_version":"1"}`)
	e := Find("gdlauncher")
	if err := WriteSlots(e, dir, Slots{PreLaunch: "sh pre", PostExit: "sh post"}); err != nil {
		t.Fatal(err)
	}
	slots, found, err := ReadSlots(e, dir)
	if err != nil || !found || slots.PreLaunch != "sh pre" || slots.PostExit != "sh post" {
		t.Fatalf("read back: %+v found=%v err=%v", slots, found, err)
	}
	if err := WriteSlots(e, dir, Slots{}); err != nil {
		t.Fatal(err)
	}
	if slots, _, err := ReadSlots(e, dir); err != nil || slots.PreLaunch != "" || slots.PostExit != "" {
		t.Fatalf("slots cleared: %+v err=%v", slots, err)
	}
	var got struct {
		Name string `json:"name"`
	}
	data, err := os.ReadFile(filepath.Join(dir, GDLauncherInstanceFile))
	if err != nil {
		t.Fatal(err)
	}
	if json.Unmarshal(data, &got) != nil || got.Name != "Cozy" {
		t.Fatalf("the instance's own keys must survive: %s", data)
	}
}

func TestWriteSlotsWithNoInstanceIsNoOp(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"atlauncher", "gdlauncher"} {
		if err := WriteSlots(Find(name), dir, Slots{PreLaunch: "sh pre"}); err != nil {
			t.Fatalf("%s: writing slots for an instance that isn't there: %v", name, err)
		}
		if _, found, err := ReadSlots(Find(name), dir); found || err != nil {
			t.Fatalf("%s: found=%v err=%v", name, found, err)
		}
	}
	if _, ok := SlotOf("mojang"); ok {
		t.Fatal("the Minecraft launcher has no slot to fill until the shim lands")
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

type atlauncherFile struct {
	Name     string `json:"name"`
	Launcher struct {
		Memory int    `json:"maximumMemory"`
		Enable *bool  `json:"enableCommands"`
		Pre    string `json:"preLaunchCommand"`
		Post   string `json:"postExitCommand"`
	} `json:"launcher"`
}

func atlauncherFixture(t *testing.T, dir string) atlauncherFile {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, ATLauncherInstanceFile))
	if err != nil {
		t.Fatal(err)
	}
	var got atlauncherFile
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	return got
}
