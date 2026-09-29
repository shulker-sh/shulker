package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/out"
)

func TestPreLaunchReportsTheSync(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	prismDir := t.TempDir()
	h.mustRun(t, "link", "prism", "--launcher-dir", prismDir, "--name", "Friends")
	gameDir := filepath.Join(prismDir, "instances", "shulker-friends", "minecraft")

	code, stdout, stderr := h.run(t, "hook", "pre-launch", "-C", gameDir)
	if code != 0 || !strings.Contains(stdout, "Synced client") {
		t.Fatalf("pre-launch should report its sync: code=%d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(gameDir, "mods", h.jars["sodium"].filename)); err != nil {
		t.Fatalf("pre-launch should sync the instance: %v", err)
	}
	in := readInstances(t, h)[0]
	if in.LastSync == "" || in.LastError != "" {
		t.Fatalf("pre-launch should stamp the row: %+v", in)
	}
	if f := readIntent(t, gameDir); f.Resolved == nil || f.Resolved.LastSyncAt != in.LastSync || f.Resolved.LastResult != instance.ResultOK {
		t.Fatalf("pre-launch should stamp the instance file: %+v", f.Resolved)
	}
}

const fakeGame = `#!/bin/sh
printf '%s\n' "$@" > "@ARGS@.tmp" && mv "@ARGS@.tmp" "@ARGS@"
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
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
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
	if code != 0 || !strings.Contains(stdout, "Synced client") {
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

func TestWrapPassesTheGamesExitCodeBack(t *testing.T) {
	h, gameDir, _ := wrappedInstance(t, "3", nil)
	code, stdout, stderr := h.run(t, "hook", "wrap", "-C", gameDir, "--", "--gameDir", gameDir, "--accessToken", accessToken)
	if code != 3 || !strings.Contains(stderr, "Game exited with status 3") {
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

func TestWrapFallsBackToJavaWhenTheWrapperCantRun(t *testing.T) {
	h, gameDir, argsFile := wrappedInstance(t, "0", func(f *instance.File) {
		f.Settings.Wrapper = []string{"shulker-no-such-wrapper", "--tag"}
	})
	code, stdout, stderr := h.run(t, "hook", "wrap", "-C", gameDir, "--", "--gameDir", gameDir, "--accessToken", accessToken)
	if code != 0 {
		t.Fatalf("a wrapper that can't run must not fail the launch: code=%d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, `Can't run the wrapper "shulker-no-such-wrapper", so the game starts with Java alone`) {
		t.Fatalf("the fallback should warn:\n%s", stderr)
	}
	if got := readArgs(t, argsFile); got != "--gameDir\n"+gameDir+"\n--accessToken\n"+accessToken+"\n" {
		t.Fatalf("java should get the argv untouched, got %q", got)
	}
	records := instance.LoadLaunches(gameDir)
	if len(records) != 1 || records[0].Outcome != instance.OutcomeOK {
		t.Fatalf("the game ran, so the record is a normal one, got %+v", records)
	}
	if strings.Contains(stderr, accessToken) {
		t.Fatalf("the warning repeats the game argv:\n%s", stderr)
	}
}

func TestWrapRecordsALaunchThatNeverStarted(t *testing.T) {
	h, gameDir, argsFile := wrappedInstance(t, "0", nil)
	java := readIntent(t, gameDir).Resolved.Java
	if err := os.Remove(java); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := h.run(t, "hook", "wrap", "-C", gameDir, "--", "--gameDir", gameDir, "--accessToken", accessToken)
	if code == 0 {
		t.Fatalf("no game started, so the launcher needs a non-zero exit to show an error\nstdout: %s\nstderr: %s", stdout, stderr)
	}
	if !strings.Contains(stderr, "Can't run Java at "+java+", so the game didn't start") {
		t.Fatalf("the failure should name the Java it couldn't run:\n%s", stderr)
	}
	if _, err := os.Stat(argsFile); err == nil {
		t.Fatal("the game must not have run")
	}
	records := instance.LoadLaunches(gameDir)
	if len(records) != 1 {
		t.Fatalf("a launch that never started is still one record, got %+v", records)
	}
	rec := records[0]
	if rec.Outcome != instance.OutcomeNotStarted || rec.EndedAt == "" || rec.StartedAt != rec.EndedAt {
		t.Fatalf("the record should close on the spot as not-started, got %+v", rec)
	}
	if !strings.Contains(rec.Error, java) {
		t.Fatalf("the record should name the Java it couldn't run, got %q", rec.Error)
	}
	launches, err := os.ReadFile(instance.LaunchesPath(gameDir))
	if err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"stderr": stderr, "launches.json": string(launches)} {
		if strings.Contains(text, accessToken) {
			t.Fatalf("%s repeats the game argv:\n%s", name, text)
		}
	}
}

func TestWrapRecordsALaunchThatNeverStartedWithoutThePreLaunchHook(t *testing.T) {
	off := false
	h, gameDir, _ := wrappedInstance(t, "0", func(f *instance.File) {
		f.Settings.Hooks.PreLaunch, f.Settings.Hooks.PostExit = &off, &off
	})
	if err := os.Remove(readIntent(t, gameDir).Resolved.Java); err != nil {
		t.Fatal(err)
	}
	abandoned := instance.Launch{StartedAt: "2026-09-01T00:00:00Z"}
	if err := instance.SaveLaunches(gameDir, []instance.Launch{abandoned}, 5); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := h.run(t, "hook", "wrap", "-C", gameDir, "--", "--gameDir", gameDir); code == 0 {
		t.Fatal("no game started, so the exit is non-zero even with both hooks off")
	}
	records := instance.LoadLaunches(gameDir)
	if len(records) != 2 || records[1].Outcome != instance.OutcomeNotStarted {
		t.Fatalf("no record was stamped, so the failure opens its own, got %+v", records)
	}
	if records[0] != abandoned {
		t.Fatalf("an open record this run didn't stamp is an abandoned run, got %+v", records[0])
	}
}

func TestWrapKeepsNoRecordWhenTheHistoryIsOff(t *testing.T) {
	none := 0
	h, gameDir, _ := wrappedInstance(t, "0", func(f *instance.File) {
		f.Settings.LaunchHistory = &none
	})
	if err := os.Remove(readIntent(t, gameDir).Resolved.Java); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := h.run(t, "hook", "wrap", "-C", gameDir, "--", "--gameDir", gameDir); code == 0 {
		t.Fatal("a launch that never started still exits non-zero with no history to record it in")
	}
	if records := instance.LoadLaunches(gameDir); len(records) != 0 {
		t.Fatalf("launchHistory 0 records nothing at all, got %+v", records)
	}
}

func TestWrapSaysSoWhenTheInstanceFileCantBeRead(t *testing.T) {
	h, gameDir, argsFile := wrappedInstance(t, "0", nil)
	if err := os.WriteFile(instance.Path(gameDir), []byte("{ broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := h.run(t, "hook", "wrap", "-C", gameDir, "--", "--gameDir", gameDir, "--accessToken", accessToken)
	if code == 0 {
		t.Fatalf("an instance shulker can't read leaves no launch, so the launcher needs an error\nstdout: %s\nstderr: %s", stdout, stderr)
	}
	if !strings.Contains(stderr, instance.Path(gameDir)) {
		t.Fatalf("the failure should name the file it couldn't read:\n%s", stderr)
	}
	if strings.Contains(stderr, accessToken) {
		t.Fatalf("the failure repeats the game argv:\n%s", stderr)
	}
	if _, err := os.Stat(argsFile); err == nil {
		t.Fatal("the game must not have run")
	}
}

func TestWrapWithoutGameDirStillFallsBackPastTheWrapper(t *testing.T) {
	h, gameDir, argsFile := wrappedInstance(t, "0", func(f *instance.File) {
		f.Settings.Wrapper = []string{"shulker-no-such-wrapper"}
	})
	h.mustRun(t, "hook", "wrap", "-C", gameDir, "--", "-jar", ".")
	if got := readArgs(t, argsFile); got != "-jar\n.\n" {
		t.Fatalf("the version check should still reach java, got %q", got)
	}
	if records := instance.LoadLaunches(gameDir); len(records) != 0 {
		t.Fatalf("the version check is not a launch, got %d records", len(records))
	}
}

func TestPreLaunchKeepsThePlayersFileOnAConflict(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	overrides := filepath.Join(h.dir, "overrides")
	writeFile(t, filepath.Join(overrides, "options.txt"), "renderDistance:8\n")
	prismDir := t.TempDir()
	h.mustRun(t, "link", "prism", "--launcher-dir", prismDir, "--name", "Friends")
	gameDir := filepath.Join(prismDir, "instances", "shulker-friends", "minecraft")

	writeFile(t, filepath.Join(gameDir, "options.txt"), "renderDistance:16\n")
	writeFile(t, filepath.Join(gameDir, "config", "extra.json"), "mine\n")
	writeFile(t, filepath.Join(overrides, "options.txt"), "renderDistance:32\n")
	writeFile(t, filepath.Join(overrides, "config", "extra.json"), "pack\n")
	writeFile(t, filepath.Join(overrides, "config", "new.json"), "new\n")
	h.mustRun(t, "add", "sodium")

	code, stdout, _ := h.run(t, "sync", "-i", "friends", "--json")
	if code == 0 || failureCode(t, stdout).Code != "build-conflict" {
		t.Fatalf("an explicit sync still fails on a conflict: code=%d\n%s", code, stdout)
	}

	code, stdout, stderr := h.run(t, "hook", "pre-launch", "-C", gameDir)
	if code != 0 || !strings.Contains(stdout, "Synced client") {
		t.Fatalf("a launch-time sync goes on past a conflict: code=%d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	for _, want := range []string{"options.txt (changed in place and in the source)", "config/extra.json (not written by shulker)", "shulker pull keeps yours", "shulker sync -i friends --force"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("the warning should name %q:\n%s", want, stderr)
		}
	}
	for rel, want := range map[string]string{"options.txt": "renderDistance:16\n", "config/extra.json": "mine\n", "config/new.json": "new\n"} {
		if got := readFile(t, filepath.Join(gameDir, filepath.FromSlash(rel))); got != want {
			t.Fatalf("%s = %q, want %q", rel, got, want)
		}
	}
	if _, err := os.Stat(filepath.Join(gameDir, "mods", h.jars["sodium"].filename)); err != nil {
		t.Fatalf("the rest of the update applies: %v", err)
	}

	code, stdout, stderr = h.run(t, "hook", "pre-launch", "-C", gameDir)
	if code != 0 || strings.Contains(stderr, "options.txt") || strings.Contains(stderr, "extra.json") {
		t.Fatalf("a kept file is edited in place from then on, not warned again: code=%d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	h.mustRun(t, "sync", "-i", "friends")
}

func TestPreLaunchOutsideAnInstancePrintsAnEnvelope(t *testing.T) {
	code, stdout, stderr := run(t, "hook", "pre-launch", "-C", t.TempDir(), "--json")
	var env out.Envelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil || code != out.ExitOK || stderr != "" {
		t.Fatalf("code=%d err=%v stdout=%q stderr=%q", code, err, stdout, stderr)
	}
	if !env.OK || env.Command != "hook pre-launch" || len(env.Warnings) != 1 {
		t.Fatalf("envelope %+v", env)
	}
}

func TestHooksLeaveALaunchAloneWhenTheirDirectoryIsGone(t *testing.T) {
	h := newHarness(t)
	gone := filepath.Join(t.TempDir(), "gone")
	for _, kind := range []string{"pre-launch", "post-exit"} {
		if code, stdout, stderr := h.run(t, "hook", kind, "-C", gone); code != 0 {
			t.Errorf("%s on a missing directory should warn and exit 0: code=%d\nstdout: %s\nstderr: %s", kind, code, stdout, stderr)
		}
	}
}
