package project

import (
	"strings"
	"testing"

	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
)

func TestLockDifferencesIgnoresModpackEntries(t *testing.T) {
	p := &Project{
		Manifest: &manifest.Manifest{
			Minecraft: "26.2",
			Loader:    manifest.Loader{Type: "fabric", Version: "*"},
			Requires: map[string]manifest.Require{
				"mp": {Source: "https://example.com/mp.git"},
			},
		},
		Lock: &lock.Lock{
			Minecraft: "26.2",
			Loader:    lock.Loader{Type: "fabric", Version: "0.17.3"},
			Modpacks:  map[string]lock.Modpack{"mp": {Source: "https://example.com/mp.git", UsesLock: true}},
			Mods: map[string]lock.Mod{
				"modmenu":         {Provider: "modrinth", Modpack: "mp", RequiredBy: []string{}},
				"placeholder-api": {Provider: "modrinth", Modpack: "mp", RequiredBy: []string{"modmenu"}},
			},
		},
	}
	if diffs := p.LockDifferences(); len(diffs) > 0 {
		t.Fatalf("LockDifferences() = %q, want none", diffs)
	}
}

func TestAPinMatchesTheFileLocked(t *testing.T) {
	pin, locked := "5000001", "5000001"
	diffs := ModDifferences(t.TempDir(), "jei", manifest.Require{Pin: pin}, lock.Mod{Provider: "curseforge", Version: locked})
	diffs = append(diffs, HostedDifferences("pack", manifest.Require{Pin: pin}, lock.Modpack{Provider: "curseforge", Version: locked})...)
	for _, d := range diffs {
		if strings.Contains(d, "pinned") {
			t.Fatalf("differences = %q, want the pin to match", diffs)
		}
	}
}
