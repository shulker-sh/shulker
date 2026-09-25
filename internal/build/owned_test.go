package build

import (
	"slices"
	"testing"

	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/packarchive"
)

func TestDropManifestOwnedKeepsWhatTheManifestDoesNotRender(t *testing.T) {
	overrides := func() []packarchive.Override {
		var o []packarchive.Override
		for _, p := range []string{"options.txt", PropertiesFile, EulaFile, WhitelistFile, "config/a.toml"} {
			o = append(o, packarchive.Override{Layer: "overrides", Path: p})
		}
		return o
	}
	paths := func(o []packarchive.Override) []string {
		var got []string
		for _, x := range o {
			got = append(got, x.Path)
		}
		return got
	}
	m := &manifest.Manifest{Client: &manifest.Client{}, Server: &manifest.Server{}}
	if got := paths(DropManifestOwned(m, overrides())); !slices.Equal(got, []string{"options.txt", EulaFile, WhitelistFile, "config/a.toml"}) {
		t.Fatalf("no options, no players: kept %v", got)
	}
	m = &manifest.Manifest{Client: &manifest.Client{Options: map[string]any{"fov": 90}}, Server: &manifest.Server{Players: &manifest.Players{}}}
	if got := paths(DropManifestOwned(m, overrides())); !slices.Equal(got, []string{EulaFile, "config/a.toml"}) {
		t.Fatalf("options and players: kept %v", got)
	}
}
