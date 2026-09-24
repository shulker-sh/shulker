package build

import (
	"maps"
	"slices"
	"testing"

	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
)

func TestPlacementsFollowFeatureDefaultsAndIgnoreOS(t *testing.T) {
	b := &Builder{
		Manifest: &manifest.Manifest{
			Features: map[string]manifest.Feature{
				"profiling": {Default: true},
				"shaders":   {},
			},
			Client: &manifest.Client{},
			Server: &manifest.Server{},
			Requires: map[string]manifest.Require{
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
		"lithium": {"client", "server"},
		"modmenu": {"client"},
		"spark":   {"client", "server"},
		"discord": {"client"},
		"iris":    nil,
		"cloth":   {"client"},
		"sodium":  nil,
	} {
		if !slices.Equal(got[id].Sides, want) {
			t.Errorf("%s lands in %v, want %v", id, got[id].Sides, want)
		}
	}
	if !slices.Equal(got["discord"].OS, manifest.StringList{"windows"}) || !slices.Equal(got["iris"].Feature, manifest.StringList{"shaders"}) {
		t.Errorf("conditions not carried: discord %v, iris %v", got["discord"], got["iris"])
	}
	if len(got["cloth"].Feature) != 0 || len(got["cloth"].OS) != 0 {
		t.Errorf("a dependency has no conditions of its own: %v", got["cloth"])
	}
}

func TestFeatureOverridesLayersFlagsOverDecisions(t *testing.T) {
	decisions := MergeDecisions(map[string]bool{"shaders": true, "extras": false}, map[string]bool{"extras": true})
	got := FeatureOverrides(decisions, []string{"lite"}, []string{"shaders"})
	want := map[string]bool{"shaders": false, "extras": true, "lite": true}
	if !maps.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if decisions["shaders"] != true || decisions["lite"] {
		t.Fatalf("the decisions given are left as they were: %v", decisions)
	}
}
