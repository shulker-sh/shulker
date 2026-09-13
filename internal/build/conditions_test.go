package build

import (
	"slices"
	"testing"

	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
)

func TestPlacementsFollowTargetDefaultsAndIgnoreOS(t *testing.T) {
	b := &Builder{
		Manifest: &manifest.Manifest{
			Targets: map[string]manifest.Target{
				"client": {Side: "client"},
				"dev":    {Side: "client", Features: []string{"profiling"}},
				"server": {Side: "server"},
			},
			Mods: map[string]manifest.Mod{
				"lithium": {},
				"modmenu": {},
				"spark":   {Feature: manifest.StringList{"profiling"}},
				"discord": {OS: manifest.StringList{"windows"}},
				"iris":    {Feature: manifest.StringList{"shaders"}},
			},
		},
		Lock: &lock.Lock{Mods: map[string]lock.Mod{
			"lithium": {Side: "both"},
			"modmenu": {Side: "client"},
			"spark":   {Side: "both"},
			"discord": {Side: "client"},
			"iris":    {Side: "client"},
			"cloth":   {Side: "client", RequiredBy: []string{"modmenu"}},
			"sodium":  {Side: "client", RequiredBy: []string{"iris"}},
		}},
	}
	got := b.Placements()
	for id, want := range map[string][]string{
		"lithium": {"client", "dev", "server"},
		"modmenu": {"client", "dev"},
		"spark":   {"dev"},
		"discord": {"client", "dev"},
		"iris":    nil,
		"cloth":   {"client", "dev"},
		"sodium":  nil,
	} {
		if !slices.Equal(got[id].Targets, want) {
			t.Errorf("%s lands in %v, want %v", id, got[id].Targets, want)
		}
	}
	if !slices.Equal(got["discord"].OS, manifest.StringList{"windows"}) || !slices.Equal(got["iris"].Feature, manifest.StringList{"shaders"}) {
		t.Errorf("conditions not carried: discord %v, iris %v", got["discord"], got["iris"])
	}
	if len(got["cloth"].Feature) != 0 || len(got["cloth"].OS) != 0 {
		t.Errorf("a dependency has no conditions of its own: %v", got["cloth"])
	}
}
