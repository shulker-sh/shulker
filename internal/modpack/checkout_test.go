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

func TestSecureGitSource(t *testing.T) {
	for _, source := range []string{"http://example.com/pack.git", "HTTP://example.com/pack.git", "git://example.com/pack.git", "git+http://example.com/pack.git"} {
		if err := secureGitSource(source); out.CodeOf(err) != "url-insecure" {
			t.Errorf("%s: want url-insecure, got %v", source, err)
		}
	}
	for _, source := range []string{"https://example.com/pack.git", "ssh://git@example.com/pack.git", "git@example.com:me/pack.git", "file:///srv/pack.git", "/srv/pack.git", "../pack"} {
		if err := secureGitSource(source); err != nil {
			t.Errorf("%s: %v", source, err)
		}
	}
}
