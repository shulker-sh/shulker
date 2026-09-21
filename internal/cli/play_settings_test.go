package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/game"
	"shulker.sh/shulker/internal/instance"
)

// playedArgv launches the instance and returns the argv the fake java was handed, clearing the one
// an earlier launch left so that each call reads its own.
func playedArgv(t *testing.T, h *harness, gameDir string, args ...string) string {
	t.Helper()
	argsFile := filepath.Join(gameDir, "args.txt")
	os.Remove(argsFile)
	h.mustRun(t, append([]string{"-i", "pack", "play", "--no-sync"}, args...)...)
	return waitForFile(t, argsFile)
}

func TestPlayTakesEachLaunchSettingFromTheInstanceOverTheDefault(t *testing.T) {
	h := newHarness(t)
	_, gameDir := playHarness(t, h)
	h.mustRun(t, "accounts", "login", "--use")
	h.mustRun(t, "config", "set", "play.memory", "6G")
	h.mustRun(t, "config", "set", "play.jvmArgs", "--literal", `["-Dglobal=1"]`)
	h.mustRun(t, "config", "set", "play.window", "1024x768")

	argv := playedArgv(t, h, gameDir)
	for _, want := range []string{"-Xms6G\n-Xmx6G\n-Dglobal=1\nnet.minecraft.client.main.Main\n", "--width\n1024\n--height\n768\n"} {
		if !strings.Contains(argv, want) {
			t.Fatalf("the defaults in config.json reach the launch: %q is missing from\n%s", want, argv)
		}
	}

	h.mustRun(t, "-i", "pack", "instance", "set", "memory", "8G")
	h.mustRun(t, "-i", "pack", "instance", "set", "jvmArgs", "--literal", `["-Dmine=1"]`)
	argv = playedArgv(t, h, gameDir)
	if !strings.Contains(argv, "-Xms8G\n-Xmx8G\n-Dmine=1\nnet.minecraft.client.main.Main\n") {
		t.Fatalf("the instance's own keys win, after the version's JVM arguments:\n%s", argv)
	}
	if strings.Contains(argv, "-Dglobal=1") || strings.Contains(argv, "6G") {
		t.Fatalf("an instance's list replaces the default rather than adding to it:\n%s", argv)
	}
	if !strings.Contains(argv, "--width\n1024\n") {
		t.Fatalf("a key the instance leaves out still inherits:\n%s", argv)
	}
}

func TestPlayWindowFlagWinsForOneRun(t *testing.T) {
	h := newHarness(t)
	_, gameDir := playHarness(t, h)
	h.mustRun(t, "accounts", "login", "--use")
	h.mustRun(t, "config", "set", "play.window", "1024x768")
	h.mustRun(t, "-i", "pack", "instance", "set", "window", "1280x720")

	if argv := playedArgv(t, h, gameDir, "--window", "800x600"); !strings.Contains(argv, "--width\n800\n--height\n600\n") || strings.Contains(argv, "1280") {
		t.Fatalf("--window wins over both tiers:\n%s", argv)
	}
	if got := instanceSettings(t, gameDir); got.Window != "1280x720" {
		t.Fatalf("--window is for one run and writes nothing: %+v", got)
	}
	if argv := playedArgv(t, h, gameDir); !strings.Contains(argv, "--width\n1280\n--height\n720\n") {
		t.Fatalf("the next run is back to the instance's window:\n%s", argv)
	}
	if code, stdout, _ := h.run(t, "-i", "pack", "play", "--window", "big", "--json"); code != 2 || !strings.Contains(stdout, `"usage"`) {
		t.Fatalf("--window big: exit %d\n%s", code, stdout)
	}
}

func TestPlayRunsTheJavaAndWrapperEitherTierNames(t *testing.T) {
	h := newHarness(t)
	_, gameDir := playHarness(t, h)
	h.mustRun(t, "accounts", "login", "--use")
	global := filepath.Join(h.fakeJDK(t, "25.0.1", "0"), "bin", "java")
	mine := filepath.Join(h.fakeJDK(t, "25.0.1", "0"), "bin", "java")

	h.mustRun(t, "config", "set", "play.java", global)
	if got := playJSON(t, h, "-i", "pack", "play", "--dry-run"); got.Java != global {
		t.Fatalf("play.java is the default java: %q", got.Java)
	}
	h.mustRun(t, "-i", "pack", "instance", "set", "java", mine)
	if got := playJSON(t, h, "-i", "pack", "play", "--dry-run"); got.Java != mine {
		t.Fatalf("the instance's java wins: %q", got.Java)
	}

	dir := t.TempDir()
	wrapper, wrapperArgs := filepath.Join(dir, "wrapper"), filepath.Join(dir, "wrapper-args.txt")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"" + wrapperArgs + ".tmp\" && mv \"" + wrapperArgs + ".tmp\" \"" + wrapperArgs + "\"\nshift\nexec \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	h.mustRun(t, "config", "set", "play.wrapper", "--literal", `["`+wrapper+`", "--tag"]`)
	argv := playedArgv(t, h, gameDir)
	if got := waitForFile(t, wrapperArgs); !strings.HasPrefix(got, "--tag\n"+mine+"\n") {
		t.Fatalf("the wrapper runs first, with java after it:\n%s", got)
	}
	if !strings.Contains(argv, "net.minecraft.client.main.Main\n") {
		t.Fatalf("java still gets the argv through the wrapper:\n%s", argv)
	}
}

func TestPlayUsesTheInstancesPinnedAccount(t *testing.T) {
	h := newHarness(t)
	_, gameDir := playHarness(t, h)
	h.mustRun(t, "accounts", "login", "--use")
	h.msa.signsIn("jeb", "Jeb_", dinnerbone)
	h.mustRun(t, "accounts", "login")
	h.mustRun(t, "-i", "pack", "instance", "set", "account", "Jeb_")

	if res := playedJSON(t, h, "-i", "pack", "play", "--no-sync"); res.Account.Name != "Jeb_" {
		t.Fatalf("the pin wins over the default account: %+v", res.Account)
	}
	if argv := waitForFile(t, filepath.Join(gameDir, "args.txt")); !strings.Contains(argv, "--username\nJeb_\n") {
		t.Fatalf("the game played as someone else:\n%s", argv)
	}
	if res := playedJSON(t, h, "-i", "pack", "play", "--no-sync", "--account", "Notch"); res.Account.Name != "Notch" {
		t.Fatalf("--account wins over the pin for one run: %+v", res.Account)
	}

	h.mustRun(t, "accounts", "logout", "Jeb_", "--yes")
	env := h.runSetting(t, 1, "-i", "pack", "play", "--no-sync")
	if env.Error == nil || env.Error.Code != "account-not-found" || !strings.Contains(env.Error.Message, "pinned") {
		t.Fatalf("a pin whose account has gone fails rather than falling back: %+v", env.Error)
	}
	if _, _, stderr := h.run(t, "-i", "pack", "play", "--no-sync"); !strings.Contains(stderr, "shulker instance unset account") {
		t.Fatalf("the error names the way back to the default account:\n%s", stderr)
	}
}

func TestLaunchArgvSizesTheWindowThroughTheVersionsOwnArguments(t *testing.T) {
	sized := []game.Rule{{Action: "allow", Features: map[string]bool{"has_custom_resolution": true}}}
	v := game.Version{MainClass: "Main", Arguments: &game.Arguments{
		JVM: []game.Argument{{Values: []string{"-cp", "x"}}},
		Game: []game.Argument{
			{Values: []string{"--username", "Steve"}},
			{Values: []string{"--width", "${resolution_width}", "--height", "${resolution_height}"}, Rules: sized},
		},
	}}

	got := strings.Join(launchArgv(v, map[string]string{}, instance.Settings{Memory: "2G"}, "640x480", quickPlay{}), " ")
	if got != "-cp x -Xms2G -Xmx2G Main --username Steve --width 640 --height 480" {
		t.Fatalf("argv %q", got)
	}
	if got := strings.Join(launchArgv(v, map[string]string{}, instance.Settings{}, "", quickPlay{}), " "); got != "-cp x Main --username Steve" {
		t.Fatalf("no window asks for none: %q", got)
	}
}
