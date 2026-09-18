package launcher

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/instance"
)

// Shim is the file a Minecraft Launcher profile's javaDir names. The launcher execs it with the
// game's whole argument list, in an environment holding almost nothing, so both paths it needs are
// absolute and recorded in it rather than looked up.
type Shim struct {
	// Dir is the game directory, which is where .shulker/ lives.
	Dir string
	// Shulker is the binary recorded in settings.shulker. The shim hands the launch to it, and runs
	// Java itself when that binary is gone, so a deleted shulker costs the player updates, not the
	// game.
	Shulker string
	// Java is the Java to run without shulker: settings.java when set, else resolved.java.
	Java string
}

// ShimPath is the file a profile's javaDir names.
func ShimPath(dir string) string { return shimPath(dir, runtime.GOOS) }

func shimPath(dir, goos string) string {
	name := "java"
	if goos == "windows" {
		name = "javaw.exe"
	}
	return filepath.Join(dir, instance.Dir, name)
}

// IsShulkerShim reports whether a profile's javaDir is one shulker wrote, which is how reconcile
// tells its own value from the Java a player chose.
func IsShulkerShim(javaDir string) bool {
	for _, sep := range []string{"/", `\`} {
		if strings.HasSuffix(javaDir, instance.Dir+sep+"java") || strings.HasSuffix(javaDir, instance.Dir+sep+"javaw.exe") {
			return true
		}
	}
	return false
}

// WriteShim writes the shim for the running OS: an sh script, or on Windows an executable and the
// sidecar holding the same two paths the script embeds.
func WriteShim(s Shim) error {
	return writeShim(s, runtime.GOOS, machineShimBuild())
}

func writeShim(s Shim, goos string, b shimBuild) error {
	if err := os.MkdirAll(filepath.Join(s.Dir, instance.Dir), 0o755); err != nil {
		return err
	}
	if goos == "windows" {
		return writeWindowsShim(s, b)
	}
	path := shimPath(s.Dir, goos)
	if err := fsutil.Write(path, []byte(shimSh(s))); err != nil {
		return err
	}
	// The launcher runs the shim itself, so it has to be executable.
	return os.Chmod(path, 0o755)
}

// RemoveShim drops both shapes of the shim and the sidecar, for a switch turned off or an instance
// unlinked.
func RemoveShim(dir string) error {
	windows := shimPath(dir, "windows")
	// A shim moved aside by an earlier rewrite may still be running, and Windows won't delete one of
	// those; it is leftover bytes nothing points at, so failing to remove it is not a failure.
	os.Remove(windows + shimOldSuffix)
	for _, path := range []string{shimPath(dir, "unix"), windows, shimSidecarPath(windows)} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// ShimMode reports whether this binary is running as an instance's javaw.exe, which is what the
// fallback route leaves there: one copy of shulker with its subsystem byte patched. The name comes
// from GetModuleFileName, which os.Executable reads, so a caller can't spoof it the way it could
// argv[0].
func ShimMode() bool {
	if runtime.GOOS != "windows" {
		return false
	}
	exe, err := os.Executable()
	return err == nil && strings.EqualFold(filepath.Base(exe), filepath.Base(shimPath("", "windows")))
}

// RunShim hands the launch to the shulker the sidecar records, to the recorded Java when that binary
// is gone, and reports the child's exit code: a non-zero exit of its own would raise a launcher error
// dialog over shulker's business rather than the game's.
func RunShim() int {
	exe, err := os.Executable()
	if err != nil {
		return 0
	}
	program, arguments, err := shimLaunch(exe, commandLineTail())
	if err != nil || program == "" {
		return 0
	}
	return shimSpawn(filepath.Dir(filepath.Dir(exe)), program, arguments)
}

// shimLaunch is what the shim starts, from the sidecar beside it and the raw tail of its own command
// line. The tail passes through untouched: it carries the session access token, and rebuilding it
// from a parsed argv would corrupt the quoting.
func shimLaunch(exe, tail string) (program, arguments string, err error) {
	dir := filepath.Dir(filepath.Dir(exe))
	data, err := os.ReadFile(shimSidecarPath(exe))
	if err != nil {
		return "", "", err
	}
	shulker, java := sidecarPaths(data)
	if shulker == "" || !fileExists(shulker) {
		return java, tail, nil
	}
	arguments = `hook wrap -C "` + dir + `" --`
	if tail != "" {
		arguments += " " + tail
	}
	return shulker, arguments, nil
}

func sidecarPaths(data []byte) (shulker, java string) {
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	if len(lines) > 0 {
		shulker = strings.TrimSpace(lines[0])
	}
	if len(lines) > 1 {
		java = strings.TrimSpace(lines[1])
	}
	return shulker, java
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// cutProgramName drops the shim's own name from the front of a raw command line, quoted or bare,
// leaving the arguments exactly as the launcher wrote them.
func cutProgramName(line string) string {
	i := 0
	if strings.HasPrefix(line, `"`) {
		if end := strings.Index(line[1:], `"`); end >= 0 {
			i = end + 2
		} else {
			i = len(line)
		}
	} else {
		for i < len(line) && line[i] != ' ' && line[i] != '\t' {
			i++
		}
	}
	return strings.TrimLeft(line[i:], " \t")
}

// shimSh names the instance with -C because the launcher's version check runs the shim with no
// --gameDir in the argv, and passes the argv on after -- because it carries the session token and
// belongs to Java alone.
func shimSh(s Shim) string {
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	b.WriteString("# Generated by shulker. Edits are lost on the next sync; turn the hooks off in\n")
	b.WriteString("# .shulker/instance.json instead.\n\n")
	b.WriteString(`dir=$(cd "$(dirname "$0")/.." && pwd)` + "\n")
	fmt.Fprintf(&b, "shulker=%s\n", shSingleQuote(s.Shulker))
	fmt.Fprintf(&b, "java=%s\n\n", shSingleQuote(s.Java))
	b.WriteString(`if [ -n "$dir" ] && [ -x "$shulker" ]; then` + "\n")
	b.WriteString("\texec \"$shulker\" hook wrap -C \"$dir\" -- \"$@\"\nfi\n\n")
	b.WriteString(`exec "$java" "$@"` + "\n")
	return b.String()
}
