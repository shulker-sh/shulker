package auditlog

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func readEntries(t *testing.T, path string) []Entry {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var entries []Entry
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		var e Entry
		if err := json.Unmarshal(scan.Bytes(), &e); err != nil {
			t.Fatalf("line %q: %v", scan.Text(), err)
		}
		entries = append(entries, e)
	}
	return entries
}

func TestRunWritesItsEnvelopeAndWhatItShowed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shulker", FileName)
	clock := time.Date(2026, 9, 19, 18, 2, 11, 0, time.UTC)
	l := New(path, []string{"sync", "-i", "friends"})
	l.Now = func() time.Time { return clock }
	l.Start("sync", "launchers", "friends", map[string]string{"instance": "friends"})
	l.Warn("[friends] jei has no build for 26.2")
	clock = clock.Add(1500 * time.Millisecond)
	l.Error("git-fetch", "can't fetch the pack")
	l.End(1)

	entries := readEntries(t, path)
	if len(entries) != 4 {
		t.Fatalf("entries = %+v", entries)
	}
	start, warn, fail, end := entries[0], entries[1], entries[2], entries[3]
	if start.At != "2026-09-19T18:02:11.000Z" || start.Level != LevelInfo || start.Msg != "start" || start.Flags["instance"] != "friends" {
		t.Errorf("start = %+v", start)
	}
	for _, e := range entries {
		if e.Cmd != "sync" || e.Group != "launchers" || e.Instance != "friends" {
			t.Errorf("entry = %+v", e)
		}
	}
	if warn.Level != LevelWarn || warn.Code != "" || warn.Msg != "[friends] jei has no build for 26.2" {
		t.Errorf("warn = %+v", warn)
	}
	if fail.Level != LevelError || fail.Code != "git-fetch" || fail.Msg != "can't fetch the pack" {
		t.Errorf("error = %+v", fail)
	}
	if end.Msg != "end" || end.Exit == nil || *end.Exit != 1 || end.DurationMS == nil || *end.DurationMS != 1500 {
		t.Errorf("end = %+v", end)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("mode = %v", info.Mode().Perm())
		}
	}
}

func TestLaterRunsAppend(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	for range 2 {
		l := New(path, []string{"version"})
		l.Start("version", "shulker", "", nil)
		l.End(0)
	}
	if entries := readEntries(t, path); len(entries) != 4 {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestWriterRefusesTheGamesArgv(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	dir := "/instances/friends"
	token := "eyJhbGciOiJIUzI1NiJ9.session-token"
	args := []string{"hook", "wrap", "-C", dir, "--", "--gameDir", dir, "--username", "Steve1234", "--accessToken", token}
	l := New(path, args)
	l.Start("hook wrap", "launchers", dir, map[string]string{"dir": dir, "leak": token})
	l.Warn("game said --accessToken " + token + " for Steve1234")
	l.Error("launch-not-started", "can't run Java for "+dir+" with "+token)
	l.End(1)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{token, "Steve1234"} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("log carries %q:\n%s", secret, data)
		}
	}
	entries := readEntries(t, path)
	if entries[0].Flags["dir"] != dir || entries[0].Instance != dir || !strings.Contains(entries[2].Msg, dir) {
		t.Errorf("shulker's own -C was refused too: %+v", entries)
	}
	if !strings.Contains(entries[1].Msg, refused) {
		t.Errorf("warn = %q", entries[1].Msg)
	}
}

func TestUnwritableLogFailsOnce(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	var failures []error
	l := New(filepath.Join(blocker, FileName), nil)
	l.OnFail = func(err error) { failures = append(failures, err) }
	l.Start("sync", "launchers", "", nil)
	l.Warn("one")
	l.End(0)
	if len(failures) != 1 {
		t.Fatalf("failures = %v", failures)
	}
}

func TestNoPathFailsOnce(t *testing.T) {
	var failures []error
	l := New("", nil)
	l.OnFail = func(err error) { failures = append(failures, err) }
	l.Start("sync", "launchers", "", nil)
	l.End(0)
	if len(failures) != 1 || !errors.Is(failures[0], ErrNoPath) {
		t.Fatalf("failures = %v", failures)
	}
}
