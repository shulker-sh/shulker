package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCloneFileClonesOnAPFS(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "object")
	if err := os.WriteFile(src, []byte("a mod"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := cloneFile(src, filepath.Join(dir, "clone")); err != nil {
		t.Skipf("the temp dir can't clone: %v", err)
	}
	if err := cloneFile(src, filepath.Join(dir, "clone")); err == nil {
		t.Fatal("cloned over a path that exists")
	}
}

func TestCanCloneTriesAClone(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "object")
	if err := os.WriteFile(src, []byte("a mod"), 0o600); err != nil {
		t.Fatal(err)
	}
	if cloneFile(src, filepath.Join(dir, "probe")) != nil {
		t.Skip("the temp dir can't clone")
	}
	if !CanClone(src, filepath.Join(dir, "instances", "not-yet")) {
		t.Fatal("a folder that doesn't exist yet should be judged by the nearest one above it")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("the probe left files behind: %v", entries)
	}
	if CanClone(filepath.Join(dir, "missing"), dir) {
		t.Fatal("a missing source cloned")
	}
}
