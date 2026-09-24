package project

import (
	"maps"
	"slices"

	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/packarchive"
)

// OverrideLayers are a project's override folders: the three every project has, then each
// feature's.
func OverrideLayers(m *manifest.Manifest) []string {
	layers := slices.Clone(packarchive.Layers)
	for _, name := range slices.Sorted(maps.Keys(m.Features)) {
		o := m.Features[name].Overrides
		for _, layer := range []string{o.Both, o.Client, o.Server} {
			if layer != "" && !slices.Contains(layers, layer) {
				layers = append(layers, layer)
			}
		}
	}
	return layers
}
