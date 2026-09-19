package launcher

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const recordArgv = `#!/bin/sh
printf '%s\n' "$@" > "@OUT@"
`

// recorder is a program that writes the argv it was called with, standing in for shulker and for
// the Java the shim falls back to.
func recorder(t *testing.T, name string) (path, argv string) {
	t.Helper()
	dir := t.TempDir()
	path, argv = filepath.Join(dir, name), filepath.Join(dir, name+".argv")
	write(t, path, strings.ReplaceAll(recordArgv, "@OUT@", argv))
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	return path, argv
}

func readArgv(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("nothing ran: %v", err)
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

func sameDir(t *testing.T, a, b string) bool {
	t.Helper()
	ra, err := filepath.EvalSymlinks(a)
	if err != nil {
		return false
	}
	rb, err := filepath.EvalSymlinks(b)
	return err == nil && ra == rb
}

func TestShimHandsTheLaunchToShulkerAndFallsBackToJava(t *testing.T) {
	gameDir := t.TempDir()
	shulker, shulkerArgv := recorder(t, "shulker")
	java, javaArgv := recorder(t, "java")
	if err := WriteShim(Shim{Dir: gameDir, Shulker: shulker, Java: java}); err != nil {
		t.Fatal(err)
	}
	shim := ShimPath(gameDir)
	info, err := os.Stat(shim)
	if err != nil || info.Mode()&0o111 == 0 {
		t.Fatalf("the launcher runs the shim itself, so it has to be executable: %v %v", info, err)
	}

	argv := []string{"-Xmx2G", "net.minecraft.client.main.Main", "--gameDir", gameDir, "--accessToken", "secret"}
	if out, err := exec.Command(shim, argv...).CombinedOutput(); err != nil {
		t.Fatalf("shim: %v %s", err, out)
	}
	got := readArgv(t, shulkerArgv)
	if len(got) < 5 || got[0] != "hook" || got[1] != "wrap" || got[2] != "-C" || !sameDir(t, got[3], gameDir) || got[4] != "--" {
		t.Fatalf("the shim names the instance before the game's own argv: %q", got)
	}
	if strings.Join(got[5:], "\n") != strings.Join(argv, "\n") {
		t.Fatalf("the game's argv should pass through whole: %q", got[5:])
	}
	if _, err := os.Stat(javaArgv); err == nil {
		t.Fatal("shulker ran, so the shim must not start Java itself as well")
	}

	// A player who deletes shulker keeps their game: it stops updating, it doesn't stop starting.
	if err := os.Remove(shulker); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(shim, argv...).CombinedOutput(); err != nil {
		t.Fatalf("shim without shulker: %v %s", err, out)
	}
	if got := strings.Join(readArgv(t, javaArgv), "\n"); got != strings.Join(argv, "\n") {
		t.Fatalf("the recorded Java should get the argv unchanged: %q", got)
	}
}

func TestShimIsRewrittenAndRemoved(t *testing.T) {
	gameDir := t.TempDir()
	if err := WriteShim(Shim{Dir: gameDir, Shulker: "/old/shulker", Java: "/old/java"}); err != nil {
		t.Fatal(err)
	}
	shim := ShimPath(gameDir)
	if !IsShulkerShim(shim) {
		t.Fatalf("shulker must recognise its own shim: %q", shim)
	}
	if IsShulkerShim("/Library/Java/JavaVirtualMachines/temurin-21.jdk/Contents/Home/bin/java") {
		t.Fatal("a player's own Java must not read as the shim")
	}
	if err := WriteShim(Shim{Dir: gameDir, Shulker: "/new/shulker", Java: "/new/java"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(shim)
	if err != nil {
		t.Fatal(err)
	}
	if body := string(data); strings.Contains(body, "/old/") || !strings.Contains(body, "'/new/shulker'") || !strings.Contains(body, "'/new/java'") {
		t.Fatalf("a rewrite replaces both paths: %s", body)
	}
	if info, err := os.Stat(shim); err != nil || info.Mode()&0o111 == 0 {
		t.Fatalf("a rewritten shim stays executable: %v %v", info, err)
	}
	if err := RemoveShim(gameDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(shim); !os.IsNotExist(err) {
		t.Fatalf("the shim should be gone: %v", err)
	}
	if err := RemoveShim(gameDir); err != nil {
		t.Fatalf("removing a shim that is already gone: %v", err)
	}
}

// shimDir is an instance whose .shulker/ folder is there for a sidecar to go in.
func shimDir(t *testing.T) (gameDir, exe string) {
	t.Helper()
	gameDir = t.TempDir()
	exe = shimPath(gameDir, "windows")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	return gameDir, exe
}

func TestShimSaysWhyWhenThereIsNothingToRun(t *testing.T) {
	_, exe := shimDir(t)
	started := false
	spawn := func(string, string, string) (int, error) { started = true; return 0, nil }
	nothingToRun := func(name string) {
		t.Helper()
		var stderr strings.Builder
		if code := runShim(exe, "", &stderr, spawn); code == 0 {
			t.Fatalf("%s: a shim that started no game must not report success", name)
		}
		if started {
			t.Fatalf("%s: there was nothing to start", name)
		}
		got := stderr.String()
		if !strings.Contains(got, shimSidecarPath(exe)) || !strings.Contains(got, "shulker instances repair") {
			t.Fatalf("%s: the shim names the file and what to do about it: %q", name, got)
		}
	}

	nothingToRun("no sidecar at all")
	for _, tc := range []struct{ name, sidecar string }{
		{"an empty sidecar", ""},
		{"one line and no Java", `C:\gone\shulker.exe` + "\r\n"},
		{"a blank second line", `C:\gone\shulker.exe` + "\r\n \r\n"},
	} {
		write(t, shimSidecarPath(exe), tc.sidecar)
		nothingToRun(tc.name)
	}

	// An unreadable sidecar records no Java either, so it is the same failure as a missing one.
	if err := os.Remove(shimSidecarPath(exe)); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(shimSidecarPath(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	nothingToRun("an unreadable sidecar")
}

func TestShimRunsTheRecordedJavaAndPassesItsCodeOn(t *testing.T) {
	_, exe := shimDir(t)
	java := `C:\java\bin\javaw.exe`
	write(t, shimSidecarPath(exe), "\r\n"+java+"\r\n")

	var stderr strings.Builder
	var ran string
	if code := runShim(exe, `-Xmx2G --accessToken secret`, &stderr, func(_, program, arguments string) (int, error) {
		ran = program + " " + arguments
		return 3, nil
	}); code != 3 {
		t.Fatalf("the game's own status goes back to the launcher: %d", code)
	}
	if ran != java+" -Xmx2G --accessToken secret" {
		t.Fatalf("the recorded Java runs with the tail untouched: %q", ran)
	}
	if stderr.String() != "" {
		t.Fatalf("a game that ran says nothing: %q", stderr.String())
	}

	// A recorded Java that is gone is the other way to end up with no window and no message.
	stderr.Reset()
	code := runShim(exe, "", &stderr, func(string, string, string) (int, error) {
		return 0, errors.New("file does not exist")
	})
	if code == 0 || !strings.Contains(stderr.String(), java) {
		t.Fatalf("a Java that never started: code %d, stderr %q", code, stderr.String())
	}
}
