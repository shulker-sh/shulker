package build

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"shulker.sh/shulker/internal/build/marker"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
)

// markerOn is whether a build into dir carries the marker mod. The layers are the directory's own
// settings.marker where its instance file names one, then the manifest's, then on. The builder
// resolves it from the directory it builds into, so no call site can leave it out and ship the jar
// into an instance that asked for none, and a sync that writes the instance file after the build
// resolves the same way on the run that creates it as on every run after.
func (b *Builder) markerOn(dir string) bool {
	if f, err := instance.Load(dir); err == nil && f.Settings.Marker != nil {
		return *f.Settings.Marker
	}
	return b.Manifest.UsesMarker()
}

// markerInfo is what side's marker says about the pack: the manifest's metadata, the mods sel
// placed, the conditions the build was made under, and the manifest and lock as they are on disk.
func (b *Builder) markerInfo(side string, cond conditions, sel selection, n *notices) (marker.Info, error) {
	lockData, err := os.ReadFile(b.LockPath)
	if err != nil {
		return marker.Info{}, err
	}
	manifestData, err := os.ReadFile(filepath.Join(b.Dir, manifest.FileName))
	if err != nil {
		return marker.Info{}, err
	}
	lockHash, err := lock.FileSha256(b.LockPath)
	if err != nil {
		return marker.Info{}, err
	}
	var direct, deps []string
	for id, m := range b.Lock.Mods {
		if !sel.included[id] || !m.PlacedOn(side) {
			continue
		}
		if _, ok := b.Manifest.Mods()[id]; ok {
			direct = append(direct, id)
		} else {
			deps = append(deps, id)
		}
	}
	sort.Strings(direct)
	sort.Strings(deps)
	return marker.Info{
		ID:          marker.ModID(b.Manifest.Name),
		Version:     marker.Version(b.Manifest.Version, lockHash),
		Name:        b.Manifest.DisplayName(side),
		Authors:     b.Manifest.Authors,
		License:     b.Manifest.License,
		Links:       b.Manifest.Links,
		Description: b.markerDescription(direct, deps, cond, n),
		Direct:      direct,
		Deps:        deps,
		Files:       map[string][]byte{manifest.FileName: manifestData, lock.FileName: lockData},
	}, nil
}

func (b *Builder) markerDescription(direct, deps []string, cond conditions, n *notices) marker.Description {
	entries := b.directEntries(cond)
	items := func(ids []string) []marker.Item {
		items := make([]marker.Item, len(ids))
		for i, id := range ids {
			items[i] = marker.Item{Text: id, Note: cond.admittedBy(entries[id])}
		}
		return items
	}
	d := marker.Description{
		Text:     b.Manifest.Description,
		Summary:  fmt.Sprintf("Minecraft %s • %s %s • %d mods", b.Lock.Minecraft, b.Lock.Loader.Type, b.Lock.Loader.Version, len(direct)+len(deps)),
		Sections: append(b.noticeSections(n), marker.Section{Title: "Mods", Items: items(direct)}, marker.Section{Title: "Dependencies", Items: items(deps)}),
	}
	if b.mentionsOS() {
		d.Labels = append(d.Labels, marker.Label{Name: "OS", Value: cond.osLabel()})
	}
	if on := cond.featureLabels(); len(on) > 0 {
		d.Labels = append(d.Labels, marker.Label{Name: "Features", Value: strings.Join(on, ", ")})
	}
	return d
}
