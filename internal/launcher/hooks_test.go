package launcher

import (
	"os"
	"strings"
	"testing"
)

func TestHookScriptPrefersRecordedPathThenPATH(t *testing.T) {
	body := hookBody(Hook{Dir: "/games/cozy", Kind: HookPreLaunch, Shulker: "/opt/homebrew/bin/shulker"}, "unix")
	for _, want := range []string{
		`shulker='/opt/homebrew/bin/shulker'`,
		`if [ ! -x "$shulker" ]; then shulker=$(command -v shulker || true); fi`,
		`echo 'shulker isn'\''t installed, get it at shulker.sh to receive pack updates' >&2`,
		`"$shulker" hook pre-launch -C "$dir" || true`,
		"exit 0",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("script is missing %q:\n%s", want, body)
		}
	}
	if !strings.Contains(body, `dir=$(cd "$(dirname "$0")/.." && pwd)`) {
		t.Fatalf("the script must locate its own instance so a moved folder keeps working:\n%s", body)
	}
}

func TestHookScriptWithDeadlinePassesTheStatusOn(t *testing.T) {
	body := hookBody(Hook{Dir: "/games/cozy", Kind: HookPreLaunch, Shulker: "/usr/local/bin/shulker", Deadline: "4m"}, "unix")
	if !strings.Contains(body, `"$shulker" hook pre-launch -C "$dir" --deadline 4m`) || !strings.Contains(body, "exit $?") {
		t.Fatalf("GDLauncher's script must pass the status on so the paused message is shown:\n%s", body)
	}
	// The PATH fallback's own `|| true` is fine; the hook invocation is what must not swallow it.
	if strings.Contains(body, `hook pre-launch -C "$dir" || true`) {
		t.Fatalf("a deadline script must not swallow the status:\n%s", body)
	}
}

func TestHookScriptRunsAdoptedCommandFirst(t *testing.T) {
	h := Hook{
		Dir:     "/games/cozy",
		Kind:    HookPreLaunch,
		Shulker: "/usr/local/bin/shulker",
		Adopted: `/usr/bin/say "launching"`,
		Tokens:  map[string]string{"INST_NAME": "Cozy's Pack", "INST_DIR": "/games/cozy"},
	}
	body := hookBody(h, "unix")
	adopted := strings.Index(body, `/usr/bin/say "launching"`)
	exports := strings.Index(body, "INST_DIR=")
	shulker := strings.Index(body, `"$shulker" hook`)
	if exports < 0 || adopted < 0 || shulker < 0 || !(exports < adopted && adopted < shulker) {
		t.Fatalf("exports, then the adopted command, then shulker:\n%s", body)
	}
	if strings.Index(body, "INST_DIR=") > strings.Index(body, "INST_NAME=") {
		t.Fatalf("exports must be written in a stable order:\n%s", body)
	}
	// The launcher stops substituting its own tokens once the command lives in the script, and a
	// name with an apostrophe must survive the quoting.
	if !strings.Contains(body, `INST_NAME='Cozy'\''s Pack'; export INST_NAME`) {
		t.Fatalf("token exports must be quoted:\n%s", body)
	}
	if !strings.Contains(body, `if [ "$adopted" -ne 0 ]; then exit "$adopted"; fi`) {
		t.Fatalf("an adopted command's non-zero exit must still abort the launch:\n%s", body)
	}
}

func TestWriteHookKeepsOnlyTheRunningOSShape(t *testing.T) {
	dir := t.TempDir()
	h := Hook{Dir: dir, Kind: HookPreLaunch, Shulker: "/usr/local/bin/shulker"}
	if err := writeHook(h, "windows"); err != nil {
		t.Fatal(err)
	}
	if err := writeHook(h, "unix"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(hookPath(dir, HookPreLaunch, "unix")); err != nil {
		t.Fatalf("the running OS's script must exist: %v", err)
	}
	if _, err := os.Stat(hookPath(dir, HookPreLaunch, "windows")); !os.IsNotExist(err) {
		t.Fatalf("the other shape must be removed, since a slot names only one: %v", err)
	}
	if err := RemoveHook(dir, HookPreLaunch); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(hookPath(dir, HookPreLaunch, "unix")); !os.IsNotExist(err) {
		t.Fatalf("removing a hook must drop its script: %v", err)
	}
	if err := RemoveHook(dir, HookPreLaunch); err != nil {
		t.Fatalf("removing an absent hook must be no error: %v", err)
	}
}

func TestHookScriptCmdShape(t *testing.T) {
	body := hookBody(Hook{Dir: `C:\games\cozy`, Kind: HookPostExit, Shulker: `C:\shulker\shulker.exe`}, "windows")
	for _, want := range []string{
		`for %%i in ("%~dp0..") do set "dir=%%~fi"`,
		`set "shulker=C:\shulker\shulker.exe"`,
		`if not exist "%shulker%" set "shulker=shulker"`,
		`"%shulker%" hook post-exit -C "%dir%"`,
		"exit /b 0",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("cmd script is missing %q:\n%s", want, body)
		}
	}
}
