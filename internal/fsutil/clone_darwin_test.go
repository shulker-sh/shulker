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
