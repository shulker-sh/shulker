package project

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
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

func TestCopySourceLeavesOutTheOtherSidesFolders(t *testing.T) {
	src, dir := t.TempDir(), t.TempDir()
	for _, layer := range []string{"overrides", "client-overrides", "server-overrides", "admin-server-overrides"} {
		os.MkdirAll(filepath.Join(src, layer), 0o755)
		os.WriteFile(filepath.Join(src, layer, "a.txt"), []byte(layer), 0o644)
	}
	created, leftOut, _, err := CopySource(src, dir, featureManifest(), "client")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"admin-server-overrides", "server-overrides"}; !slices.Equal(leftOut, want) {
		t.Fatalf("leftOut = %v, want %v", leftOut, want)
	}
	if want := []string{filepath.Join(dir, "client-overrides"), filepath.Join(dir, "overrides")}; !slices.Equal(created, want) {
		t.Fatalf("created = %v, want %v", created, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "server-overrides")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("server-overrides should not have been copied")
	}
}

func TestCopySourceUndoRemovesWhatAFailedCopyMade(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root reads unreadable files")
	}
	src, dir := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(src, ".gitignore"), []byte("/build/\n"), 0o644)
	os.MkdirAll(filepath.Join(src, "overrides"), 0o755)
	unreadable := filepath.Join(src, "overrides", "secret.txt")
	os.WriteFile(unreadable, []byte("x"), 0o000)
	t.Cleanup(func() { os.Chmod(unreadable, 0o644) })
	created, _, undo, err := CopySource(src, dir, &manifest.Manifest{}, "")
	if err == nil {
		t.Fatal("the copy should fail on the unreadable file")
	}
	if want := []string{filepath.Join(dir, ".gitignore"), filepath.Join(dir, "overrides")}; !slices.Equal(created, want) {
		t.Fatalf("created = %v, want %v", created, want)
	}
	os.WriteFile(filepath.Join(dir, manifest.FileName), []byte("{}"), 0o644)
	undo()
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("undo left %v", entries)
	}
}

func TestCopyOwnFilesRefusesAPathOutsideTheSource(t *testing.T) {
	root := t.TempDir()
	src, dir := filepath.Join(root, "a", "src"), filepath.Join(root, "b", "dst")
	if err := os.WriteFile(filepath.Join(root, "secret"), []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{src, dir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	created, err := CopyOwnFiles(src, dir, []string{"../../secret"})
	if out.CodeOf(err) != "path-outside" || len(created) != 0 {
		t.Fatalf("want path-outside and nothing created, got %v %v", created, err)
	}
}
