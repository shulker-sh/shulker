package player

import "shulker.sh/shulker/internal/manifest"

// RefsOf is the manifest's server players as refs.
func RefsOf(m *manifest.Manifest) []Ref {
	var refs []Ref
	if m.Server == nil {
		return refs
	}
	for _, p := range m.Server.Players.All() {
		refs = append(refs, Ref{Name: p.Name, UUID: p.UUID})
	}
	return refs
}
