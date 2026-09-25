package fsutil

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

func TestSHA1HashesTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.jar")
	if err := Write(path, []byte("abc")); err != nil {
		t.Fatal(err)
	}
	if sum, err := SHA1(path); err != nil || sum != "a9993e364706816aba3e25717850c26c9cd0d89d" {
		t.Fatalf("sha1 = %q, %v", sum, err)
	}
	if _, err := SHA1(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("a missing file has no hash")
	}
}

func TestReplaceKeepsOldBytesAsReplaced(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shulker.lock")
	if err := Write(path, []byte("old")); err != nil {
		t.Fatal(err)
	}
	kept, err := Replace(path, []byte("new"))
	if err != nil {
		t.Fatal(err)
	}
	if kept != path+".replaced" {
		t.Fatalf("kept = %q", kept)
	}
	if data, _ := os.ReadFile(path); string(data) != "new" {
		t.Fatalf("content = %q", data)
	}
	if data, _ := os.ReadFile(kept); string(data) != "old" {
		t.Fatalf("replaced = %q", data)
	}
}

func TestReplaceKeepsTheOldMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Replace(path, []byte("new")); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", info.Mode().Perm())
	}
}

func TestReplaceOverwritesAnOlderReplaced(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	for _, s := range []string{"first", "second", "third"} {
		if _, err := Replace(path, []byte(s)); err != nil {
			t.Fatal(err)
		}
	}
	if data, _ := os.ReadFile(path + ".replaced"); string(data) != "second" {
		t.Fatalf("replaced = %q", data)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Fatalf("entries = %v", entries)
	}
}

func TestReplaceWithNothingThereJustWrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.json")
	kept, err := Replace(path, []byte("new"))
	if err != nil || kept != "" {
		t.Fatalf("kept = %q, err = %v", kept, err)
	}
	if data, _ := os.ReadFile(path); string(data) != "new" {
		t.Fatalf("content = %q", data)
	}
	if _, err := os.Lstat(path + ".replaced"); !os.IsNotExist(err) {
		t.Fatalf("replaced exists: %v", err)
	}
}

func TestReplaceFollowsASymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "real.json")
	link := filepath.Join(dir, "link.json")
	if err := Write(target, []byte("old")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	kept, err := Replace(link, []byte("new"))
	if err != nil {
		t.Fatal(err)
	}
	if want, _ := filepath.EvalSymlinks(target); kept != want+".replaced" {
		t.Fatalf("kept = %q", kept)
	}
	if data, _ := os.ReadFile(target); string(data) != "new" {
		t.Fatalf("target = %q", data)
	}
	if data, _ := os.ReadFile(kept); string(data) != "old" {
		t.Fatalf("replaced = %q", data)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link = %v, %v", info, err)
	}
}

func TestReadTailKeepsTheLastLinesWhole(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log")
	var b strings.Builder
	for i := range 3000 {
		fmt.Fprintf(&b, "line %d %s\n", i, strings.Repeat("x", 40))
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	tail, err := ReadTail(f, 5)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(tail, "line 2999 "+strings.Repeat("x", 40)+"\n") || strings.Count(tail, "\n") < 6 || len(tail) > 2*tailChunk {
		t.Fatalf("the tail holds the last lines whole and no more than a chunk past them: %d bytes ending %q", len(tail), tail[max(0, len(tail)-60):])
	}
	if pos, _ := f.Seek(0, io.SeekCurrent); pos != int64(b.Len()) {
		t.Fatalf("the file is left at its end, not %d", pos)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if all, err := ReadTail(f, 0); err != nil || all != b.String() {
		t.Fatalf("no limit is the whole file: %v", err)
	}
}
