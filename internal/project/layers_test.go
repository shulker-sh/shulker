package project

import (
	"slices"
	"testing"

	"shulker.sh/shulker/internal/manifest"
)

func featureManifest() *manifest.Manifest {
	return &manifest.Manifest{Features: map[string]manifest.Feature{
		"shaders": {Overrides: manifest.FeatureOverrides{Both: "shaders-overrides", Client: "shaders-client-overrides"}},
		"admin":   {Overrides: manifest.FeatureOverrides{Server: "admin-server-overrides"}},
	}}
}

func TestOverrideLayersListsTheFixedThenEachFeatures(t *testing.T) {
	got := OverrideLayers(featureManifest())
	want := []string{"overrides", "client-overrides", "server-overrides", "admin-server-overrides", "shaders-overrides", "shaders-client-overrides"}
	if !slices.Equal(got, want) {
		t.Fatalf("layers = %v, want %v", got, want)
	}
}

func TestIsSideLayerKnowsAFeaturesSideFolders(t *testing.T) {
	m := featureManifest()
	cases := map[string]map[string]bool{
		"client": {"client-overrides": true, "shaders-client-overrides": true, "server-overrides": false, "shaders-overrides": false, "overrides": false},
		"server": {"server-overrides": true, "admin-server-overrides": true, "client-overrides": false, "shaders-client-overrides": false},
	}
	for side, layers := range cases {
		for layer, want := range layers {
			if got := IsSideLayer(m, side, layer); got != want {
				t.Errorf("IsSideLayer(%s, %s) = %v, want %v", side, layer, got, want)
			}
		}
	}
}
