package launcher

import (
	"encoding/binary"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fakePE is the smallest file patchSubsystem will look at: a DOS header pointing at a PE header whose
// optional header carries a subsystem byte.
func fakePE(subsystem byte) []byte {
	pe := make([]byte, 0x100)
	pe[0], pe[1] = 'M', 'Z'
	binary.LittleEndian.PutUint32(pe[peOffsetAt:], 0x80)
	copy(pe[0x80:], "PE\x00\x00")
	pe[0x80+peSubsystemAt] = subsystem
	return pe
}

// fakeCsc stands in for the compiler: it writes the source it was handed to the /out: path, so the
// output carries the marker the same way a real assembly carries it in its metadata.
func fakeCsc(t *testing.T, exitCode int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "csc.sh")
	write(t, path, `#!/bin/sh
out=""
src=""
for arg in "$@"; do
	case "$arg" in
	/out:*) out=${arg#/out:} ;;
	*.cs) src=$arg ;;
	esac
done
`+"exit_code="+strconv.Itoa(exitCode)+`
if [ "$exit_code" -ne 0 ]; then echo "error CS9999: no" >&2; exit "$exit_code"; fi
cp "$src" "$out"
`)
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCscProbeTakesThe64BitCompilerFirst(t *testing.T) {
	windir := t.TempDir()
	both := map[string]bool{}
	for _, framework := range []string{"Framework64", "Framework"} {
		both[filepath.Join(windir, "Microsoft.NET", framework, cscDir, "csc.exe")] = true
	}
	exists := func(path string) bool { return both[path] }
	if got, want := cscPath(windir, exists), filepath.Join(windir, "Microsoft.NET", "Framework64", cscDir, "csc.exe"); got != want {
		t.Fatalf("probe = %q, want %q", got, want)
	}
	only32 := filepath.Join(windir, "Microsoft.NET", "Framework", cscDir, "csc.exe")
	if got := cscPath(windir, func(path string) bool { return path == only32 }); got != only32 {
		t.Fatalf("probe = %q, want the 32-bit compiler %q", got, only32)
	}
	if got := cscPath(windir, func(string) bool { return false }); got != "" {
		t.Fatalf("a machine with no compiler takes the copy route: %q", got)
	}
	if got := cscPath("", exists); got != "" {
		t.Fatalf("no WINDIR, no compiler: %q", got)
	}
}

func TestPatchSubsystemChangesOnlyThatByte(t *testing.T) {
	console := fakePE(peSubsystemCUI)
	patched, err := patchSubsystem(console)
	if err != nil {
		t.Fatal(err)
	}
	at := 0x80 + peSubsystemAt
	if patched[at] != peSubsystemGUI {
		t.Fatalf("subsystem = %d, want %d", patched[at], peSubsystemGUI)
	}
	for i := range console {
		if i != at && console[i] != patched[i] {
			t.Fatalf("byte %d changed too: %d -> %d", i, console[i], patched[i])
		}
	}
	if console[at] != peSubsystemCUI {
		t.Fatal("the running binary's own bytes must not be patched in place")
	}
	gui := fakePE(peSubsystemGUI)
	if again, err := patchSubsystem(gui); err != nil || again[at] != peSubsystemGUI {
		t.Fatalf("a shulker already built for the GUI subsystem needs no patch: %v", err)
	}
	if _, err := patchSubsystem([]byte("not an executable at all")); err == nil {
		t.Fatal("a file that isn't a PE must not be copied into a profile's javaDir")
	}
	notPE := fakePE(peSubsystemCUI)
	copy(notPE[0x80:], "ELF\x00")
	if _, err := patchSubsystem(notPE); err == nil {
		t.Fatal("the PE signature has to be there")
	}
}

func TestShimMarkerFollowsTheSourceAndNotTheVersion(t *testing.T) {
	marker := shimMarker()
	if !strings.HasPrefix(marker, shimMarkerPrefix) || len(marker) != len(shimMarkerPrefix)+8 {
		t.Fatalf("marker = %q", marker)
	}
	if marker != markerFor(shimCS) {
		t.Fatalf("the marker is the digest of the embedded source: %q", marker)
	}
	if markerFor(shimCS+"\n") == marker {
		t.Fatal("a changed shim source has to move the marker")
	}
	// Nothing but the digest is in it, so a release that leaves the shim alone recompiles nothing.
	if _, err := hex.DecodeString(strings.TrimPrefix(marker, shimMarkerPrefix)); err != nil {
		t.Fatalf("marker = %q: %v", marker, err)
	}
	if !strings.Contains(shimCS, "@MARKER@") {
		t.Fatal("the source carries the marker into the assembly")
	}
}

func TestWindowsShimCompilesAndKeepsACurrentOne(t *testing.T) {
	gameDir := t.TempDir()
	build := shimBuild{csc: fakeCsc(t, 0), self: func() ([]byte, error) { return fakePE(peSubsystemCUI), nil }}
	s := Shim{Dir: gameDir, Shulker: `C:\shulker\shulker.exe`, Java: `C:\java\bin\javaw.exe`}
	if err := writeShim(s, "windows", build); err != nil {
		t.Fatal(err)
	}
	exe := shimPath(gameDir, "windows")
	if filepath.Base(exe) != "javaw.exe" {
		t.Fatalf("the shim is the launcher's javaw: %q", exe)
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if !shimHasMarker(data, shimMarker()) {
		t.Fatal("the compiled shim carries the current marker")
	}
	if sidecar := readFileString(t, shimSidecarPath(exe)); sidecar != s.Shulker+"\r\n"+s.Java+"\r\n" {
		t.Fatalf("the sidecar holds the two paths and nothing else: %q", sidecar)
	}

	// A compiled shim that is current is left alone: csc output isn't byte-deterministic, so the
	// marker is the only thing worth testing, and a compile on every sync would run before every game.
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(exe, old, old); err != nil {
		t.Fatal(err)
	}
	s.Java = `C:\java-21\bin\javaw.exe`
	if err := writeShim(s, "windows", shimBuild{csc: fakeCsc(t, 1), self: build.self}); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(exe); err != nil || !info.ModTime().Equal(old) {
		t.Fatalf("the shim was rewritten: %v %v", info, err)
	}
	if sidecar := readFileString(t, shimSidecarPath(exe)); !strings.Contains(sidecar, "java-21") {
		t.Fatalf("a recorded path that changed rewrites the sidecar: %q", sidecar)
	}
}

func TestWindowsShimFallsBackToThePatchedCopy(t *testing.T) {
	for _, csc := range []string{"", fakeCsc(t, 1), filepath.Join(t.TempDir(), "missing-csc")} {
		gameDir := t.TempDir()
		build := shimBuild{csc: csc, self: func() ([]byte, error) { return fakePE(peSubsystemCUI), nil }}
		// No warning either way: both routes produce a shim that behaves the same.
		if err := writeShim(Shim{Dir: gameDir, Shulker: `C:\s.exe`, Java: `C:\j.exe`}, "windows", build); err != nil {
			t.Fatalf("csc %q: %v", csc, err)
		}
		exe := shimPath(gameDir, "windows")
		data, err := os.ReadFile(exe)
		if err != nil {
			t.Fatalf("csc %q: %v", csc, err)
		}
		if data[0x80+peSubsystemAt] != peSubsystemGUI {
			t.Fatalf("csc %q: the copy is patched to the GUI subsystem", csc)
		}

		old := time.Now().Add(-time.Hour)
		if err := os.Chtimes(exe, old, old); err != nil {
			t.Fatal(err)
		}
		if err := writeShim(Shim{Dir: gameDir, Shulker: `C:\s.exe`, Java: `C:\j.exe`}, "windows", build); err != nil {
			t.Fatal(err)
		}
		if info, err := os.Stat(exe); err != nil || !info.ModTime().Equal(old) {
			t.Fatalf("csc %q: a copy that is already the running binary must not be written again: %v %v", csc, info, err)
		}
		if err := RemoveShim(gameDir); err != nil {
			t.Fatal(err)
		}
		for _, path := range []string{exe, shimSidecarPath(exe)} {
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("%s should be gone: %v", filepath.Base(path), err)
			}
		}
	}
}

func TestShimLaunchPrefersShulkerAndPassesTheTailThrough(t *testing.T) {
	gameDir := t.TempDir()
	shulker := filepath.Join(gameDir, "shulker.exe")
	write(t, shulker, "")
	exe := shimPath(gameDir, "windows")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, shimSidecarPath(exe), shulker+"\r\n"+`C:\java\bin\javaw.exe`+"\r\n")

	tail := `-Xmx2G net.minecraft.client.main.Main --gameDir "` + gameDir + `" --accessToken secret`
	program, arguments, err := shimLaunch(exe, tail)
	if err != nil {
		t.Fatal(err)
	}
	if program != shulker || arguments != `hook wrap -C "`+gameDir+`" -- `+tail {
		t.Fatalf("program %q arguments %q", program, arguments)
	}
	if program, arguments, err = shimLaunch(exe, ""); err != nil || arguments != `hook wrap -C "`+gameDir+`" --` {
		t.Fatalf("the launcher's version check runs the shim with no arguments: %q %q %v", program, arguments, err)
	}
	// A player who deletes shulker keeps their game: it stops updating, it doesn't stop starting.
	if err := os.Remove(shulker); err != nil {
		t.Fatal(err)
	}
	if program, arguments, err = shimLaunch(exe, tail); err != nil || program != `C:\java\bin\javaw.exe` || arguments != tail {
		t.Fatalf("program %q arguments %q err %v", program, arguments, err)
	}
	if _, _, err = shimLaunch(filepath.Join(t.TempDir(), "javaw.exe"), tail); err == nil {
		t.Fatal("no sidecar, nothing to run")
	}
}

func TestCutProgramNameLeavesTheArgumentsUntouched(t *testing.T) {
	for _, tc := range []struct{ line, want string }{
		{`"C:\Games\My Pack\.shulker\javaw.exe" -Xmx2G  -cp "a;b"`, `-Xmx2G  -cp "a;b"`},
		{`javaw.exe --accessToken secret`, `--accessToken secret`},
		{`"C:\p\javaw.exe"`, ``},
		{`javaw.exe`, ``},
		{`"C:\unbalanced\javaw.exe -Xmx2G`, ``},
		{``, ``},
	} {
		if got := cutProgramName(tc.line); got != tc.want {
			t.Fatalf("cutProgramName(%q) = %q, want %q", tc.line, got, tc.want)
		}
	}
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
