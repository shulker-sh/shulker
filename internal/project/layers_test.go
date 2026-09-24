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
