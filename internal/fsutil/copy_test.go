package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyPathCopiesAFileAndATree(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(filepath.Join(src, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(src, "a.txt"), []byte("a"), 0o644)
	os.WriteFile(filepath.Join(src, "nested", "b.txt"), []byte("b"), 0o644)
	if err := CopyPath(src, filepath.Join(dir, "tree")); err != nil {
		t.Fatal(err)
	}
	if err := CopyPath(filepath.Join(src, "a.txt"), filepath.Join(dir, "one", "a.txt")); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{"tree/a.txt": "a", "tree/nested/b.txt": "b", "one/a.txt": "a"} {
		if data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(path))); err != nil || string(data) != want {
			t.Errorf("%s = %q, %v", path, data, err)
		}
	}
}
