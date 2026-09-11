package fsutil

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteReplacesAndLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.json")
	if err := Write(path, []byte("one")); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, []byte("two")); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); string(data) != "two" {
		t.Fatalf("content = %q", data)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("entries = %v", entries)
	}
}

func TestWriteKeepsModeAndFollowsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no unix modes or unprivileged symlinks")
	}
	dir := t.TempDir()
	real := filepath.Join(dir, "real.txt")
	os.WriteFile(real, []byte("old"), 0o600)
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if err := Write(link, []byte("new")); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link replaced: %v %v", info, err)
	}
	info, _ := os.Stat(real)
	if data, _ := os.ReadFile(real); string(data) != "new" || info.Mode().Perm() != 0o600 {
		t.Fatalf("target = %q %v", data, info.Mode())
	}
	fresh := filepath.Join(dir, "fresh.txt")
	if err := Write(fresh, nil); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(fresh); info.Mode().Perm() != 0o644 {
		t.Fatalf("new file mode = %v", info.Mode())
	}
	os.Chmod(real, 0o444)
	if err := Write(link, []byte("refused")); err == nil {
		t.Fatal("a read-only file must not be replaced")
	}
}

func TestMarshalJSON(t *testing.T) {
	data, err := MarshalJSON(map[string]string{"a": "<&>"})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "{\n  \"a\": \"<&>\"\n}\n" {
		t.Fatalf("%q", data)
	}
}
