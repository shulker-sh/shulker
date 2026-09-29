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

func TestCopyPathSkipsSymlinks(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(dir, "id_ed25519")
	if err := os.WriteFile(secret, []byte("private key"), 0o600); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "real.txt"), []byte("real"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(src, "key")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(dir, "linked.jar")); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(dir, "dst")
	if err := CopyPath(src, dst); err != nil {
		t.Fatal(err)
	}
	if err := CopyPath(filepath.Join(dir, "linked.jar"), filepath.Join(dir, "out.jar")); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(dst, "key"), filepath.Join(dir, "out.jar")} {
		if _, err := os.Lstat(p); err == nil {
			t.Fatalf("%s: a symlink's target must not be copied", p)
		}
	}
	if data, err := os.ReadFile(filepath.Join(dst, "real.txt")); err != nil || string(data) != "real" {
		t.Fatalf("regular file: %q, %v", data, err)
	}
}
