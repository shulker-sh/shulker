package fsutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLinkDirReachesPastMaxPath(t *testing.T) {
	dir := t.TempDir()
	deep := filepath.Join(dir, strings.Repeat("a", 100), strings.Repeat("b", 100), strings.Repeat("c", 100))
	data := filepath.Join(deep, "data", "saves")
	if err := os.MkdirAll(data, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "level.dat"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(deep, "build"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(deep, "build", "saves")
	if len(link) <= 260 {
		t.Fatalf("the link must pass MAX_PATH to test it: %d characters", len(link))
	}
	if err := LinkDir(filepath.Join("..", "data", "saves"), link); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(link, "level.dat")); err != nil {
		t.Fatalf("link does not reach its data: %v", err)
	}
	if target, ok := ReadLink(link); !ok || !SameTarget(link, target, data) {
		t.Fatalf("ReadLink = %q %v, want a link to %s", target, ok, data)
	}
}
