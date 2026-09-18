package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/instance"
)

func TestPreLaunchReportsTheSync(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	prismDir := t.TempDir()
	h.mustRun(t, "link", "prism", "--launcher-dir", prismDir, "--name", "Friends")
	gameDir := filepath.Join(prismDir, "instances", "shulker-friends", "minecraft")

	code, stdout, stderr := h.run(t, "hook", "pre-launch", "-C", gameDir)
	if code != 0 || !strings.Contains(stdout, "synced client") {
		t.Fatalf("pre-launch should report its sync: code=%d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(gameDir, "mods", h.jars["sodium"].filename)); err != nil {
		t.Fatalf("pre-launch should sync the instance: %v", err)
	}
}

const fakeGame = `#!/bin/sh
printf '%s\n' "$@" > "@ARGS@"
exit @EXIT@
`

// fakeGameExe is a Java that records its argv and exits with the given status.
func fakeGameExe(t *testing.T, exit string) (java, argsFile string) {
	t.Helper()
	dir := t.TempDir()
	java, argsFile = filepath.Join(dir, "java"), filepath.Join(dir, "args.txt")
	script := strings.NewReplacer("@ARGS@", argsFile, "@EXIT@", exit).Replace(fakeGame)
	if err := os.WriteFile(java, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return java, argsFile
}

func readArgs(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the game should have run: %v", err)
	}
	return string(data)
}

// wrappedInstance links a Prism instance and records a fake Java as its managed runtime, which is
// what hook wrap runs when settings.java is unset.
func wrappedInstance(t *testing.T, exit string, edit func(f *instance.File)) (h *harness, gameDir, argsFile string) {
	t.Helper()
	h = newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	prismDir := t.TempDir()
	h.mustRun(t, "link", "prism", "--launcher-dir", prismDir, "--name", "Friends")
	gameDir = filepath.Join(prismDir, "instances", "shulker-friends", "minecraft")
	java, argsFile := fakeGameExe(t, exit)
	f, err := instance.Load(gameDir)
	if err != nil {
		t.Fatal(err)
	}
	f.Resolved = &instance.Resolved{Java: java}
	if edit != nil {
		edit(f)
	}
	if err := f.Save(gameDir); err != nil {
		t.Fatal(err)
	}
	return h, gameDir, argsFile
}

const accessToken = "eyJ-session-token-never-shown"

func TestWrapWithoutGameDirOnlyRunsJava(t *testing.T) {
	h, gameDir, argsFile := wrappedInstance(t, "0", nil)
	code, stdout, stderr := h.run(t, "hook", "wrap", "-C", gameDir, "--", "-jar", ".")
	if code != 0 || stderr != "" {
		t.Fatalf("the version check should pass through: code=%d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if got := readArgs(t, argsFile); got != "-jar\n.\n" {
		t.Fatalf("java should get the argv untouched, got %q", got)
	}
	if strings.Contains(stdout, "synced") {
		t.Fatalf("no --gameDir should mean no sync:\n%s", stdout)
	}
	if records := instance.LoadLaunches(gameDir); len(records) != 0 {
		t.Fatalf("the version check is not a launch, got %d records", len(records))
	}
}

func TestWrapSyncsThenLaunchesAndKeepsTheArgvToItself(t *testing.T) {
	h, gameDir, argsFile := wrappedInstance(t, "0", nil)
	code, stdout, stderr := h.run(t, "hook", "wrap", "-C", gameDir, "--", "--gameDir", gameDir, "--accessToken", accessToken)
	if code != 0 || !strings.Contains(stdout, "synced client") {
		t.Fatalf("wrap should sync before the game: code=%d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if got := readArgs(t, argsFile); got != "--gameDir\n"+gameDir+"\n--accessToken\n"+accessToken+"\n" {
		t.Fatalf("java should get the argv untouched, got %q", got)
	}
	records := instance.LoadLaunches(gameDir)
	if len(records) != 1 || records[0].EndedAt == "" || records[0].Outcome != instance.OutcomeOK {
		t.Fatalf("wrap should open and close one launch record, got %+v", records)
	}
	launches, err := os.ReadFile(instance.LaunchesPath(gameDir))
	if err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"stdout": stdout, "stderr": stderr, "launches.json": string(launches)} {
		if strings.Contains(text, accessToken) {
			t.Fatalf("%s repeats the game argv:\n%s", name, text)
		}
	}
}

func TestWrapLaunchesWhenTheSyncFails(t *testing.T) {
	h, gameDir, argsFile := wrappedInstance(t, "0", nil)
	if err := os.Remove(filepath.Join(h.dir, "shulker.lock")); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := h.run(t, "hook", "wrap", "-C", gameDir, "--", "--gameDir", gameDir, "--accessToken", accessToken)
	if code != 0 || !strings.Contains(stderr, "shulker.lock") {
		t.Fatalf("a sync failure should be a warning: code=%d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if !strings.Contains(readArgs(t, argsFile), "--gameDir") {
		t.Fatal("the game should still start")
	}
	if strings.Contains(stderr, accessToken) {
		t.Fatalf("the warning repeats the game argv:\n%s", stderr)
	}
}

func TestWrapPrependsTheWrapper(t *testing.T) {
	dir := t.TempDir()
	wrapper, wrapperArgs := filepath.Join(dir, "wrapper"), filepath.Join(dir, "wrapper-args.txt")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"" + wrapperArgs + "\"\nshift\nexec \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	h, gameDir, argsFile := wrappedInstance(t, "0", func(f *instance.File) {
		f.Settings.Wrapper = []string{wrapper, "--tag"}
	})
	h.mustRun(t, "hook", "wrap", "-C", gameDir, "--", "--gameDir", gameDir)
	f, err := instance.Load(gameDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := readArgs(t, wrapperArgs); got != "--tag\n"+f.Resolved.Java+"\n--gameDir\n"+gameDir+"\n" {
		t.Fatalf("the wrapper should run first with Java after it, got %q", got)
	}
	if got := readArgs(t, argsFile); got != "--gameDir\n"+gameDir+"\n" {
		t.Fatalf("java should still get the argv, got %q", got)
	}
}

func TestWrapPassesTheGamesExitCodeBack(t *testing.T) {
	h, gameDir, _ := wrappedInstance(t, "3", nil)
	code, stdout, stderr := h.run(t, "hook", "wrap", "-C", gameDir, "--", "--gameDir", gameDir, "--accessToken", accessToken)
	if code != 3 || !strings.Contains(stderr, "game exited with status 3") {
		t.Fatalf("wrap should exit as the game did: code=%d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if strings.Contains(stderr, accessToken) {
		t.Fatalf("the error repeats the game argv:\n%s", stderr)
	}
	if records := instance.LoadLaunches(gameDir); len(records) != 1 || records[0].EndedAt == "" {
		t.Fatalf("a failed run is still recorded, got %+v", records)
	}
}

func TestWrapHonoursTheHookSwitches(t *testing.T) {
	off := false
	h, gameDir, argsFile := wrappedInstance(t, "0", func(f *instance.File) {
		f.Settings.Hooks.PreLaunch = &off
	})
	stdout := h.mustRun(t, "hook", "wrap", "-C", gameDir, "--", "--gameDir", gameDir)
	if strings.Contains(stdout, "synced") || len(instance.LoadLaunches(gameDir)) != 0 {
		t.Fatalf("preLaunch off should skip the sync and the record:\n%s", stdout)
	}
	readArgs(t, argsFile)

	h, gameDir, _ = wrappedInstance(t, "0", func(f *instance.File) {
		f.Settings.Hooks.PostExit = &off
	})
	h.mustRun(t, "hook", "wrap", "-C", gameDir, "--", "--gameDir", gameDir)
	if records := instance.LoadLaunches(gameDir); len(records) != 1 || records[0].EndedAt != "" {
		t.Fatalf("postExit off should leave the record open, got %+v", records)
	}
}

func TestGameDirOf(t *testing.T) {
	for _, tc := range []struct {
		argv []string
		dir  string
		ok   bool
	}{
		{[]string{"-jar", "."}, "", false},
		{[]string{"--gameDir"}, "", false},
		{[]string{"-Xmx2G", "--gameDir", "/g", "--accessToken", "x"}, "/g", true},
		{[]string{"--gameDir=/g"}, "/g", true},
	} {
		if dir, ok := gameDirOf(tc.argv); dir != tc.dir || ok != tc.ok {
			t.Errorf("gameDirOf(%q) = %q, %v; want %q, %v", tc.argv, dir, ok, tc.dir, tc.ok)
		}
	}
}
