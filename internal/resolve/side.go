package resolve

import (
	"slices"

	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/packarchive"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/server"
)

// KeepSide narrows a project to one side: the other side's block, its entries and its override
// folders, a feature's included, go, and what went is returned by key and by override path.
func KeepSide(m *manifest.Manifest, l *lock.Lock, overrides []packarchive.Override, side string) (kept []packarchive.Override, leftOut []string) {
	other := project.OtherSide(side)
	leftOut = []string{}
	switch side {
	case "client":
		m.Server = nil
		l.Players = []lock.Player{}
		if m.Client == nil {
			m.Client = &manifest.Client{}
		}
	case "server":
		m.Client = nil
		if m.Server == nil {
			m.Server = &manifest.Server{Memory: server.DefaultMemory}
		}
	}
	for key, req := range m.Requires {
		if req.Side == other {
			delete(m.Requires, key)
			leftOut = append(leftOut, key)
		}
	}
	for key, mod := range l.Mods {
		if mod.Side == other {
			delete(l.Mods, key)
			delete(m.Requires, key)
			leftOut = append(leftOut, key)
		}
	}
	for _, kind := range manifest.PackKinds {
		packs := l.Packs(kind)
		for key, p := range packs {
			if p.Side == other {
				delete(packs, key)
				delete(m.Requires, key)
				leftOut = append(leftOut, key)
			}
		}
	}
	kept = overrides[:0]
	for _, o := range overrides {
		if project.IsSideLayer(m, other, o.Layer) {
			leftOut = append(leftOut, o.Layer+"/"+o.Path)
			continue
		}
		kept = append(kept, o)
	}
	slices.Sort(leftOut)
	return kept, slices.Compact(leftOut)
}
