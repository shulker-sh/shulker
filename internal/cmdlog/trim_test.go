package cmdlog

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

var trimNow = time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

func writeAgedLog(t *testing.T, path string, ages ...time.Duration) {
	t.Helper()
	var b strings.Builder
	for i, age := range ages {
		at := trimNow.Add(-age).Format("2006-01-02T15:04:05.000Z07:00")
		fmt.Fprintf(&b, `{"at":%q,"group":"shulker","cmd":"version","level":"info","msg":"entry %d"}`+"\n", at, i)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}

func msgs(t *testing.T, path string) []string {
	t.Helper()
	var got []string
	for _, e := range readEntries(t, path) {
		got = append(got, e.Msg)
	}
	return got
}

const day = 24 * time.Hour

func TestTrimDropsEntriesOlderThanTheWindow(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	writeAgedLog(t, path, 40*day, 31*day, 29*day, time.Hour)
	if err := Trim(path, 30, trimNow); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(msgs(t, path), ","); got != "entry 2,entry 3" {
		t.Fatalf("kept %s", got)
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("stat = %v, %v", info, err)
		}
	}
}

func TestTrimLeavesAFreshLogAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	writeAgedLog(t, path, 29*day, time.Hour)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Trim(path, 30, trimNow); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("a log with nothing to drop was rewritten")
	}
}

func TestTrimOfNoLogIsNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	if err := Trim(path, 30, trimNow); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("stat = %v", err)
	}
}

func TestTrimPastTheCapDropsTheOldestInsideTheWindow(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	writeAgedLog(t, path, 5*day, 4*day, 3*day, 2*day, day)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	line := int64(len(data) / 5)
	if err := trim(path, 30, trimNow, 4*line); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(msgs(t, path), ","); got != "entry 2,entry 3,entry 4" {
		t.Fatalf("kept %s", got)
	}
}

func TestTrimDropsUnreadableLinesBeforeTheWindowOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	writeAgedLog(t, path, 40*day, time.Hour)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.SplitAfter(string(data), "\n")
	torn := "not an entry\n" + lines[0] + lines[1] + "{\"at\":\n"
	if err := os.WriteFile(path, []byte(torn), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Trim(path, 30, trimNow); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != lines[1]+"{\"at\":\n" {
		t.Fatalf("kept %q", got)
	}
}

func TestTrimFailureLeavesTheLogWritable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a write-only file is a unix mode")
	}
	path := filepath.Join(t.TempDir(), FileName)
	writeAgedLog(t, path, 40*day)
	if err := os.Chmod(path, 0o200); err != nil {
		t.Fatal(err)
	}
	if err := Trim(path, 30, trimNow); err == nil {
		t.Fatal("a log that can't be read trimmed")
	}
	l := New(path, nil)
	l.OnFail = func(err error) { t.Errorf("append failed: %v", err) }
	l.Start("version", "shulker", "", nil)
}
