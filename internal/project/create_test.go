package project

import (
	"os"
	"path/filepath"
	"testing"

	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/packarchive"
)

func TestCreateLaysOutScaffoldOverridesManifestAndLock(t *testing.T) {
	dir := t.TempDir()
	m := &manifest.Manifest{Schema: manifest.SchemaURL, Name: "pack", Version: "1.0", Authors: []string{"me"}, Minecraft: "26.2", Loader: manifest.Loader{Type: "fabric", Version: "0.17.3"}, Client: &manifest.Client{}, Requires: map[string]manifest.Require{}}
	l := lock.New()
	l.Minecraft = "26.2"
	l.Loader = lock.Loader{Type: "fabric", Version: "0.17.3"}
	l.Java = lock.Java{Component: "java-runtime-delta", Major: 21}
	overrides := []packarchive.Override{{Layer: "client-overrides", Path: "config/a.toml", Data: []byte("a")}}
	if err := Create(dir, m, l, overrides); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"overrides", ".gitignore", "client-overrides/config/a.toml", manifest.FileName, lock.FileName} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
			t.Errorf("%s: %v", rel, err)
		}
	}
	if p, err := Open(dir); err != nil || p.Manifest.Name != "pack" || p.Lock.Minecraft != "26.2" {
		t.Fatalf("reopen: %v", err)
	}
}
