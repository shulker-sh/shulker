package project

import (
	"maps"
	"os"
	"slices"

	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/pack"
)

// LinkSource is what a link makes an instance follow: the checkout it was read from, the name
// the instance's manifest and registry row record it under, and its project. An authored source
// is a link's answers rather than a checkout: a project with no directory yet, which the link
// writes into the instance it creates.
type LinkSource struct {
	*pack.Checkout
	Name     string
	Project  *Project
	IsAuthor bool
}

// NewInstance writes the instance manifest. It pins no platform and lists no feature: the pack is
// locked, so the relock inherits all of that, and a pack that moves platform is followed rather
// than fought. What it does copy is the two preferences only the pack's author can weigh, its
// history retention and whether builds carry the marker mod; from then on both are the player's.
func NewInstance(gameDir, id, display string, src *LinkSource) (*Project, error) {
	followed := src.Project.Manifest
	m := &manifest.Manifest{
		Schema:   manifest.SchemaURL,
		Name:     id,
		Requires: map[string]manifest.Require{followed.Name: {Source: src.Name, Ref: src.Ref, Path: src.Path}},
		Client:   &manifest.Client{Name: display, Build: "."},
	}
	if followed.History != nil {
		keep := *followed.History
		m.History = &keep
	}
	if followed.Marker != nil {
		marker := *followed.Marker
		m.Marker = &marker
	}
	if err := os.MkdirAll(gameDir, 0o755); err != nil {
		return nil, err
	}
	p := &Project{Dir: gameDir, Manifest: m, Lock: lock.New()}
	return p, p.SaveManifest()
}

// ModpackKey is the key an instance follows a link's source under: the source manifest's name
// when the link wrote the entry, and whatever an earlier link or a hand edit chose when it didn't.
// Empty where no single entry is the link's: with several packs required, none of them is the one.
func ModpackKey(m *manifest.Manifest, source string) string {
	keys := slices.Sorted(maps.Keys(m.Modpacks()))
	for _, key := range keys {
		if m.Requires[key].Source == source {
			return key
		}
	}
	if len(keys) == 1 {
		return keys[0]
	}
	return ""
}
