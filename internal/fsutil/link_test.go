package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLinkDirResolvesRelativeTargetBesideLink(t *testing.T) {
	dir := t.TempDir()
	data := filepath.Join(dir, "data", "saves")
	os.MkdirAll(data, 0o755)
	os.WriteFile(filepath.Join(data, "level.dat"), []byte("x"), 0o644)
	os.MkdirAll(filepath.Join(dir, "build"), 0o755)
	link := filepath.Join(dir, "build", "saves")
	if err := LinkDir(filepath.Join("..", "data", "saves"), link); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(link, "level.dat")); err != nil {
		t.Fatalf("link does not reach its data: %v", err)
	}
	target, ok := ReadLink(link)
	if !ok || !SameTarget(link, target, data) {
		t.Fatalf("ReadLink = %q %v, want a link to %s", target, ok, data)
	}
	if err := UnlinkDir(link); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatalf("link still there: %v", err)
	}
	if _, err := os.Stat(filepath.Join(data, "level.dat")); err != nil {
		t.Fatalf("unlinking touched the data: %v", err)
	}
}

func TestReadLinkRefusesARealDirectory(t *testing.T) {
	if _, ok := ReadLink(t.TempDir()); ok {
		t.Fatal("a real directory read as a link")
	}
}

func TestSameTarget(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "build", "saves")
	want := filepath.Join(dir, "data", "saves")
	for _, tc := range []struct {
		target string
		same   bool
	}{
		{filepath.Join("..", "data", "saves"), true},
		{want, true},
		{want + string(filepath.Separator), true},
		{filepath.Join("..", "data", "logs"), false},
		{filepath.Join(dir, "elsewhere", "saves"), false},
	} {
		if got := SameTarget(link, tc.target, want); got != tc.same {
			t.Errorf("SameTarget(%q) = %v, want %v", tc.target, got, tc.same)
		}
	}
}
