package project

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"shulker.sh/shulker/internal/manifest"
)

func TestOwnPathsNamesTheProjectsFilesOnce(t *testing.T) {
	m := &manifest.Manifest{Icon: "icon.png", Requires: map[string]manifest.Require{
		"jei":   {File: "files/jei.jar"},
		"local": {Source: "./packs/local"},
		"git":   {Source: "https://example.com/pack.git"},
		"up":    {Source: "../outside"},
	}}
	got := OwnPaths(m)
	want := []string{".gitignore", "client-overrides", "files", "files/jei.jar", "icon.png", "overrides", "packs/local", "server-overrides", "shulker.lock"}
	if !slices.Equal(got, want) {
		t.Fatalf("paths = %v, want %v", got, want)
	}
}

func TestCopyOwnFilesSkipsMissingAndKeepsTheTargets(t *testing.T) {
	src, dir := t.TempDir(), t.TempDir()
	os.MkdirAll(filepath.Join(src, "overrides", "config"), 0o755)
	os.WriteFile(filepath.Join(src, "overrides", "config", "a.toml"), []byte("theirs"), 0o644)
	os.WriteFile(filepath.Join(src, "icon.png"), []byte("png"), 0o644)
	os.WriteFile(filepath.Join(dir, "icon.png"), []byte("mine"), 0o644)
	created, err := CopyOwnFiles(src, dir, []string{"icon.png", "missing", "overrides"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{filepath.Join(dir, "overrides")}; !slices.Equal(created, want) {
		t.Fatalf("created = %v, want %v", created, want)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "icon.png")); string(data) != "mine" {
		t.Fatalf("icon = %q, want the target's own", data)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "overrides", "config", "a.toml")); string(data) != "theirs" {
		t.Fatalf("override = %q", data)
	}
}
