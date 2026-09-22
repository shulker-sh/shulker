package cli

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/auditlog"
	"shulker.sh/shulker/internal/out"
)

// isolatedLog points the config, and so the log, at a folder of the test's own.
func isolatedLog(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SHULKER_CONFIG", filepath.Join(dir, "config.json"))
	return filepath.Join(dir, auditlog.FileName)
}

func logEntries(t *testing.T, path string) []auditlog.Entry {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var entries []auditlog.Entry
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		var e auditlog.Entry
		if err := json.Unmarshal(scan.Bytes(), &e); err != nil {
			t.Fatalf("line %q: %v", scan.Text(), err)
		}
		entries = append(entries, e)
	}
	return entries
}

func TestRunIsLoggedFromStartToEnd(t *testing.T) {
	path := isolatedLog(t)
	if code, _, _ := run(t, "version", "--no-color"); code != out.ExitOK {
		t.Fatalf("exit %d", code)
	}
	entries := logEntries(t, path)
	if len(entries) != 2 {
		t.Fatalf("entries = %+v", entries)
	}
	start, end := entries[0], entries[1]
	if start.Cmd != "version" || start.Group != "shulker" || start.Msg != "start" || start.Flags["no-color"] != "true" {
		t.Errorf("start = %+v", start)
	}
	if end.Cmd != "version" || end.Msg != "end" || end.Exit == nil || *end.Exit != 0 || end.DurationMS == nil {
		t.Errorf("end = %+v", end)
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("stat = %v, %v", info, err)
		}
	}
}

func TestFailedRunLogsItsError(t *testing.T) {
	path := isolatedLog(t)
	code, _, _ := run(t, "--json", "nope")
	entries := logEntries(t, path)
	if len(entries) != 3 {
		t.Fatalf("entries = %+v", entries)
	}
	if e := entries[1]; e.Level != auditlog.LevelError || e.Code != "usage" || !strings.Contains(e.Msg, "nope") {
		t.Errorf("error = %+v", e)
	}
	if end := entries[2]; end.Exit == nil || *end.Exit != code || code != out.ExitUsage {
		t.Errorf("end = %+v, exit %d", end, code)
	}
}

func TestHookWrapLogsItsFlagsAndNeverTheArgv(t *testing.T) {
	path := isolatedLog(t)
	dir := t.TempDir()
	token := "eyJhbGciOiJIUzI1NiJ9.session-token"
	code, _, _ := run(t, "hook", "wrap", "-C", dir, "--", "--gameDir", dir, "--accessToken", token)
	if code == out.ExitOK {
		t.Fatalf("exit %d", code)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), token) || strings.Contains(string(data), "accessToken") {
		t.Fatalf("log carries the argv:\n%s", data)
	}
	entries := logEntries(t, path)
	start := entries[0]
	if start.Cmd != "hook wrap" || start.Group != "launchers" || start.Flags["dir"] != dir || start.Instance != dir {
		t.Errorf("start = %+v", start)
	}
	if e := entries[1]; e.Code != "launch-not-started" {
		t.Errorf("error = %+v", e)
	}
}

func TestUnwritableLogWarnsOnceOutsideHooks(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHULKER_CONFIG", filepath.Join(blocker, "config.json"))

	code, _, stderr := run(t, "version")
	if code != out.ExitOK || strings.Count(stderr, "can't write shulker's log") != 1 {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	code, _, stderr = run(t, "hook", "post-exit", "-C", t.TempDir())
	if code != out.ExitOK || strings.Contains(stderr, "log") {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
}

func TestBrokenConfigIsStillLogged(t *testing.T) {
	path := isolatedLog(t)
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "config.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := run(t, "--json", "instances"); code == out.ExitOK {
		t.Fatalf("exit %d", code)
	}
	entries := logEntries(t, path)
	if len(entries) != 3 || entries[1].Level != auditlog.LevelError || entries[2].Exit == nil || *entries[2].Exit == 0 {
		t.Fatalf("entries = %+v", entries)
	}
}
