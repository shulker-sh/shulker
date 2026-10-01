package launcher

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/instance"
)

// slotRow is the registry row for an instance folder: the slot functions take the row, since the
// Minecraft launcher's slot lives in its own directory rather than beside the game.
func slotRow(name, instanceDir string) config.Instance {
	in := config.Instance{Launcher: name, LauncherDir: instanceDir, Dir: instanceDir}
	if e := Find(name); !e.gameDirIsInstance {
		in.Dir = filepath.Join(instanceDir, "minecraft")
	}
	return in
}

func TestSlotCommandReachesTheScriptThroughTheLaunchersToken(t *testing.T) {
	for _, tc := range []struct {
		launcher, dir, goos, want string
	}{
		{"prism", "/games/cozy/.minecraft", "darwin", `sh "$INST_MC_DIR/.shulker/pre-launch"`},
		{"multimc", "/games/cozy/minecraft", "darwin", `sh "$INST_MC_DIR/.shulker/pre-launch"`},
		{"atlauncher", "/games/cozy", "darwin", `sh "$INST_DIR/.shulker/pre-launch"`},
		{"gdlauncher", "/games/cozy/instance", "darwin", `sh "/games/cozy/instance/.shulker/pre-launch"`},
		{"gdlauncher", `C:\games\cozy\instance`, "windows", `cmd /c "C:\games\cozy\instance\.shulker\pre-launch.cmd"`},
		{"gdlauncher", `/Users/me/My "Games"/cozy\instance`, "darwin", `sh "/Users/me/My \"Games\"/cozy\\instance/.shulker/pre-launch"`},
		{"prism", `/Users/me/My "Games"/.minecraft`, "darwin", `sh "$INST_MC_DIR/.shulker/pre-launch"`},
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
	if err := WriteSlots(e, slotRow(e.Name, dir), Slots{PreLaunch: "sh pre", PostExit: "sh post"}); err != nil {
		t.Fatal(err)
	}
	got := atlauncherFixture(t, dir)
	if got.Launcher.Enable == nil || !*got.Launcher.Enable {
		t.Fatalf("reconcile must turn commands on per instance, or they never run: %+v", got)
	}
	if got.Launcher.Pre != "sh pre" || got.Launcher.Post != "sh post" || got.Launcher.Memory != 4096 || got.Name != "Cozy" {
		t.Fatalf("slots written, settings kept: %+v", got)
	}
	if slots, found, err := ReadSlots(e, slotRow(e.Name, dir)); err != nil || !found || slots.PreLaunch != "sh pre" || slots.PostExit != "sh post" {
		t.Fatalf("read back: %+v found=%v err=%v", slots, found, err)
	}
	if err := WriteSlots(e, slotRow(e.Name, dir), Slots{}); err != nil {
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
	write(t, filepath.Join(dir, PrismInstanceFile), "[General]\nConfigVersion=1.3\nname=Cozy\n")
	e := Find("prism")
	if err := WriteSlots(e, slotRow(e.Name, dir), Slots{PreLaunch: "sh pre", PostExit: "sh post"}); err != nil {
		t.Fatal(err)
	}
	values, err := readINI(filepath.Join(dir, PrismInstanceFile), prismUnescape)
	if err != nil {
		t.Fatal(err)
	}
	if values["OverrideCommands"] != "true" || values["PreLaunchCommand"] != "sh pre" || values["PostExitCommand"] != "sh post" {
		t.Fatalf("Prism ignores commands without the override: %+v", values)
	}
	if values["name"] != "Cozy" || values["ConfigVersion"] != "1.3" {
		t.Fatalf("the instance's own keys must survive: %+v", values)
	}
	if err := WriteSlots(e, slotRow(e.Name, dir), Slots{PreLaunch: "sh pre"}); err != nil {
		t.Fatal(err)
	}
	values, err = readINI(filepath.Join(dir, PrismInstanceFile), prismUnescape)
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
	if err := WriteSlots(e, slotRow(e.Name, dir), Slots{PreLaunch: "sh pre", PostExit: "sh post"}); err != nil {
		t.Fatal(err)
	}
	slots, found, err := ReadSlots(e, slotRow(e.Name, dir))
	if err != nil || !found || slots.PreLaunch != "sh pre" || slots.PostExit != "sh post" {
		t.Fatalf("read back: %+v found=%v err=%v", slots, found, err)
	}
	if err := WriteSlots(e, slotRow(e.Name, dir), Slots{}); err != nil {
		t.Fatal(err)
	}
	if slots, _, err := ReadSlots(e, slotRow(e.Name, dir)); err != nil || slots.PreLaunch != "" || slots.PostExit != "" {
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
		if err := WriteSlots(Find(name), slotRow(name, dir), Slots{PreLaunch: "sh pre"}); err != nil {
			t.Fatalf("%s: writing slots for an instance that isn't there: %v", name, err)
		}
		if _, found, err := ReadSlots(Find(name), slotRow(name, dir)); found || err != nil {
			t.Fatalf("%s: found=%v err=%v", name, found, err)
		}
	}
	if slot := Find("mojang").Slot; slot == nil || !slot.UsesShim {
		t.Fatalf("the Minecraft launcher fills the profile's Java: %+v", slot)
	}
	if slot := Find("prism").Slot; slot.UsesShim {
		t.Fatal("a launcher with command slots must not take the profile's Java")
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

// posixSplit reads a command the way GDLauncher's shlex does: whitespace separates words, double
// quotes group them, and a backslash escapes the character after it.
func posixSplit(command string) []string {
	var words []string
	var word []rune
	inWord, quoted := false, false
	for i := 0; i < len(command); i++ {
		c := rune(command[i])
		switch {
		case c == '\\' && i+1 < len(command):
			i++
			word = append(word, rune(command[i]))
			inWord = true
		case c == '"':
			quoted, inWord = !quoted, true
		case c == ' ' && !quoted:
			if inWord {
				words = append(words, string(word))
				word, inWord = nil, false
			}
		default:
			word = append(word, c)
			inWord = true
		}
	}
	if inWord {
		words = append(words, string(word))
	}
	return words
}

func TestGDLauncherSlotSurvivesAQuoteInTheLauncherDir(t *testing.T) {
	dir := `/Users/me/My "Games"/cozy\instance`
	command := slotCommand("gdlauncher", dir, HookPreLaunch, "darwin")
	words := posixSplit(command)
	if len(words) != 2 || words[0] != "sh" || words[1] != dir+"/.shulker/pre-launch" {
		t.Fatalf("GDLauncher would split %q into %q", command, words)
	}
	if !IsShulkerSlot(command) {
		t.Fatalf("shulker must recognise its escaped slot: %q", command)
	}
}

func TestWrapperSlotIsFilledButNeverCleared(t *testing.T) {
	prism := t.TempDir()
	write(t, filepath.Join(prism, PrismInstanceFile), "[General]\nConfigVersion=1.3\nWrapperCommand=mangohud\n")
	e := Find("prism")
	if err := WriteSlots(e, slotRow(e.Name, prism), Slots{PreLaunch: "sh pre", Wrapper: "gamemoderun"}); err != nil {
		t.Fatal(err)
	}
	values, err := readINI(filepath.Join(prism, PrismInstanceFile), prismUnescape)
	if err != nil {
		t.Fatal(err)
	}
	if values["WrapperCommand"] != "gamemoderun" || values["OverrideCommands"] != "true" {
		t.Fatalf("Prism reads the wrapper only with the override on: %+v", values)
	}
	if err := WriteSlots(e, slotRow(e.Name, prism), Slots{PreLaunch: "sh pre"}); err != nil {
		t.Fatal(err)
	}
	if values, err = readINI(filepath.Join(prism, PrismInstanceFile), prismUnescape); err != nil {
		t.Fatal(err)
	}
	if values["WrapperCommand"] != "gamemoderun" {
		t.Fatalf("an unset wrapper is the player's, not shulker's to clear: %+v", values)
	}

	atl := t.TempDir()
	write(t, filepath.Join(atl, ATLauncherInstanceFile), `{"name":"Cozy","launcher":{"maximumMemory":4096}}`)
	e = Find("atlauncher")
	if err := WriteSlots(e, slotRow(e.Name, atl), Slots{Wrapper: "gamemoderun"}); err != nil {
		t.Fatal(err)
	}
	if slots, found, err := ReadSlots(e, slotRow(e.Name, atl)); err != nil || !found || slots.Wrapper != "gamemoderun" {
		t.Fatalf("read back: %+v found=%v err=%v", slots, found, err)
	}
	if got := atlauncherFixture(t, atl); got.Launcher.Enable == nil || !*got.Launcher.Enable {
		t.Fatalf("ATLauncher drops the wrapper unless commands are enabled: %+v", got)
	}
	if err := WriteSlots(e, slotRow(e.Name, atl), Slots{}); err != nil {
		t.Fatal(err)
	}
	if got := atlauncherFixture(t, atl); got.Launcher.Enable == nil || !*got.Launcher.Enable {
		t.Fatalf("a wrapper left in place keeps the switch that runs it: %+v", got)
	}
	if slots, _, err := ReadSlots(e, slotRow(e.Name, atl)); err != nil || slots.Wrapper != "gamemoderun" {
		t.Fatalf("the wrapper survives clearing the command slots: %+v err=%v", slots, err)
	}

	gd := t.TempDir()
	write(t, filepath.Join(gd, GDLauncherInstanceFile), `{"name":"Cozy","_version":"1"}`)
	e = Find("gdlauncher")
	if err := WriteSlots(e, slotRow(e.Name, gd), Slots{Wrapper: "gamemoderun"}); err != nil {
		t.Fatal(err)
	}
	if slots, found, err := ReadSlots(e, slotRow(e.Name, gd)); err != nil || !found || slots.Wrapper != "gamemoderun" {
		t.Fatalf("read back: %+v found=%v err=%v", slots, found, err)
	}
	if err := WriteSlots(e, slotRow(e.Name, gd), Slots{}); err != nil {
		t.Fatal(err)
	}
	if slots, _, err := ReadSlots(e, slotRow(e.Name, gd)); err != nil || slots.Wrapper != "gamemoderun" {
		t.Fatalf("the wrapper survives clearing the hooks: %+v err=%v", slots, err)
	}
}

func TestWrapperCommandQuotesTheWayEachParserReads(t *testing.T) {
	words := []string{"/opt/My Tools/gamemoderun", "--dlsym"}
	if got := wrapperCommand("prism", words, "darwin"); got != `"/opt/My Tools/gamemoderun" --dlsym` {
		t.Fatalf("prism: %q", got)
	}
	command := wrapperCommand("gdlauncher", words, "darwin")
	if got := posixSplit(command); len(got) != 2 || got[0] != words[0] || got[1] != words[1] {
		t.Fatalf("GDLauncher would split %q into %q", command, got)
	}
	// ATLauncher splits on whitespace and ignores quotes, so quoting a word would only corrupt it.
	if got := wrapperCommand("atlauncher", words, "darwin"); got != "/opt/My Tools/gamemoderun --dlsym" {
		t.Fatalf("atlauncher: %q", got)
	}
	if got := wrapperCommand("prism", []string{"gamemoderun"}, "darwin"); got != "gamemoderun" {
		t.Fatalf("a word needing no quotes must not get any: %q", got)
	}
	if got := wrapperCommand("prism", nil, "darwin"); got != "" {
		t.Fatalf("no wrapper, no command: %q", got)
	}
}

// Only a write that leaves ATLauncher's commands on turns them back on: with both hooks off and no
// wrapper, reconcile drops enableCommands, which is no switch turned on.
func TestReconcileSaysWhenItTurnsATLauncherCommandsBackOn(t *testing.T) {
	for _, tc := range []struct {
		name string
		off  bool
		want bool
	}{
		{"hooks on", false, true},
		{"hooks off", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, filepath.Join(dir, ATLauncherInstanceFile), `{"launcher":{"enableCommands":false}}`)
			f := instance.New()
			if tc.off {
				f.Settings.Hooks.PreLaunch, f.Settings.Hooks.PostExit = instance.Off(), instance.Off()
			}
			r, err := Reconcile(Find("atlauncher"), slotRow("atlauncher", dir), f, "/bin/shulker", false)
			if err != nil || r.CommandsOn != tc.want {
				t.Fatalf("CommandsOn = %v, %v; want %v", r.CommandsOn, err, tc.want)
			}
		})
	}
}
