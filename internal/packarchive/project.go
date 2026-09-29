package packarchive

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/manifest"
)

// Manifest is the manifest a project imported from the archive starts from, with the warnings
// deriving it raised. Without a marker it is built from what the archive says; with one, the
// exporting project's own manifest is the base, and the archive wins where the two disagree,
// since the archive is what was shipped. name names the project; with a marker it may be empty,
// which keeps the exporting project's name.
func (a *Archive) Manifest(name string) (*manifest.Manifest, []string) {
	if a.Marker == nil {
		m := &manifest.Manifest{
			Schema:    manifest.SchemaURL,
			Name:      name,
			Version:   a.Version,
			Note:      a.Summary,
			Minecraft: a.Minecraft,
			Loader:    manifest.Loader{Type: a.Loader.Type, Version: a.Loader.Version},
			Requires:  map[string]manifest.Require{},
			Client:    &manifest.Client{Memory: a.Memory},
		}
		if len(a.Authors) > 0 {
			m.Authors = slices.Clone(a.Authors)
		}
		if a.NeedsServer() {
			m.Server = &manifest.Server{Memory: manifest.DefaultServerMemory}
		}
		return m, nil
	}
	var warnings []string
	copied := *a.Marker.Manifest
	m := &copied
	if name != "" {
		m.Name = name
	}
	ml := a.Marker.Lock
	if ml.Minecraft != a.Minecraft {
		warnings = append(warnings, fmt.Sprintf("the marker was locked to Minecraft %s but the pack is for %s; using %s.", ml.Minecraft, a.Minecraft, a.Minecraft))
		m.Minecraft = a.Minecraft
	}
	if ml.Loader.Type != a.Loader.Type || ml.Loader.Version != a.Loader.Version {
		warnings = append(warnings, fmt.Sprintf("the marker was locked to %s but the pack is for %s; using the pack's.", loader.Describe(ml.Loader.Type, ml.Loader.Version), loader.Describe(a.Loader.Type, a.Loader.Version)))
		m.Loader = manifest.Loader{Type: a.Loader.Type, Version: a.Loader.Version}
	}
	if m.Version != a.Version {
		m.Version = a.Version
	}
	if modpacks := m.Modpacks(); len(modpacks) > 0 {
		sources := make([]string, 0, len(modpacks))
		for _, name := range slices.Sorted(maps.Keys(modpacks)) {
			source := modpacks[name].Source + modpacks[name].File
			if source == "" {
				source = name
			}
			sources = append(sources, source)
		}
		warnings = append(warnings, fmt.Sprintf("pack layers were flattened into the overrides: %s.", strings.Join(sources, ", ")))
	}
	m.Requires = map[string]manifest.Require{}
	return m, warnings
}

// NeedsServer reports whether the archive ships anything for the server side alone, which a
// project importing it declares a server block for.
func (a *Archive) NeedsServer() bool {
	for _, f := range a.Files {
		if f.Side == "server" {
			return true
		}
	}
	for _, o := range a.Overrides {
		if LayerSide(o.Layer) == "server" {
			return true
		}
	}
	return false
}
