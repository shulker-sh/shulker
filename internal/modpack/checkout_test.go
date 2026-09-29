package modpack

import (
	"path/filepath"
	"testing"

	"shulker.sh/shulker/internal/out"
)

func TestSubfolderStaysInsideTheCheckout(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{"../../.ssh", "packs/../../x", "/etc", `C:\x`} {
		if _, err := subfolder(root, path); out.CodeOf(err) != "path-outside" {
			t.Errorf("%s: want path-outside, got %v", path, err)
		}
	}
	if got, err := subfolder(root, "packs/survival"); err != nil || got != filepath.Join(root, "packs", "survival") {
		t.Fatalf("got %q, %v", got, err)
	}
	if got, err := subfolder(root, ""); err != nil || got != root {
		t.Fatalf("the root: %q, %v", got, err)
	}
}
