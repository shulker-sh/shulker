package project

import (
	"os"
	"path/filepath"
	"testing"

	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
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

func TestCreateRefusesAnOverrideOutsideItsFolder(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "b")
	overrides := []packarchive.Override{{Layer: "overrides", Path: "../../ESCAPED.txt", Data: []byte("x")}}
	err := Create(dir, &manifest.Manifest{}, lock.New(), overrides)
	if out.CodeOf(err) != "path-outside" {
		t.Fatalf("want path-outside, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "..", "ESCAPED.txt")); !os.IsNotExist(err) {
		t.Fatalf("written outside the project: %v", err)
	}
}

func TestWriteIconDropsTheKeyWhenTheArchiveHadNone(t *testing.T) {
	dir := t.TempDir()
	m := &manifest.Manifest{Icon: "assets/icon.png"}
	if err := WriteIcon(dir, m, nil); err != nil || m.Icon != "" {
		t.Fatalf("icon %q, %v", m.Icon, err)
	}
	m.Icon = "assets/icon.png"
	if err := WriteIcon(dir, m, []byte("png")); err != nil || m.Icon != "assets/icon.png" {
		t.Fatalf("icon %q, %v", m.Icon, err)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "assets", "icon.png")); err != nil || string(data) != "png" {
		t.Fatalf("written %q, %v", data, err)
	}
}

func TestDefaultAuthorsEndWithShulker(t *testing.T) {
	authors := DefaultAuthors()
	if n := len(authors); n < 1 || n > 2 || authors[n-1] != "shulker.sh" {
		t.Fatalf("authors = %v", authors)
	}
}

func TestNewManifestTakesEveryDefault(t *testing.T) {
	m := NewManifest("", "/tmp/My Pack", "", manifest.Loader{}, "client")
	if m.Name != "my-pack" || m.Minecraft != "*" || m.Loader.Type != "" || m.Client == nil || m.Server != nil {
		t.Fatalf("defaults: %+v", m)
	}
	if m.Schema != manifest.SchemaURL || m.Requires == nil || len(m.Authors) == 0 {
		t.Fatalf("shape: %+v", m)
	}
	m = NewManifest("srv", "/tmp/x", "1.21.1", manifest.Loader{Type: "neoforge", Version: "*"}, "server")
	if m.Name != "srv" || m.Minecraft != "1.21.1" || m.Loader.Type != "neoforge" || m.Client != nil || m.Server == nil {
		t.Fatalf("server: %+v", m)
	}
}
