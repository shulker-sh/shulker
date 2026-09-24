package project

import (
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
