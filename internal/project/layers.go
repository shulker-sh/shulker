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

// IsSideLayer reports whether layer is one of side's own override folders, a feature's included.
func IsSideLayer(m *manifest.Manifest, side, layer string) bool {
	layers := []string{side + "-overrides"}
	for _, f := range m.Features {
		if side == "client" && f.Overrides.Client != "" {
			layers = append(layers, f.Overrides.Client)
		}
		if side == "server" && f.Overrides.Server != "" {
			layers = append(layers, f.Overrides.Server)
		}
	}
	return slices.Contains(layers, layer)
}
