package resolve

import (
	"fmt"

	"shulker.sh/shulker/internal/provider"
)

// exactAsk is a version one mod asked for another by its provider version id, as Modrinth lets a
// dependency name one version rather than a project.
type exactAsk struct {
	by      string
	version *provider.Version
}

// movesToExact reports whether key, locked at another version than the one parent asks for by id,
// moves to it; one already locked at that version stays and says nothing. A mod this command locked moves, as the command settles on versions that fit each
// other; one the lock held moves only with --with-deps, and a pin or a locked modpack's never
// does. Two mods asking for different versions settle on the newer. Whatever stays warns, since
// the jar's own ranges often allow it and only the provider's listing names the one version.
func (r *Resolver) movesToExact(p provider.Provider, key, parent string, pv, want *provider.Version) bool {
	m := r.Lock.Mods[key]
	if m.Provider != p.Name() {
		return false
	}
	same := m.Version == want.ID
	asks := fmt.Sprintf("%s %s asks for %s %s", parent, pv.Number, key, want.Number)
	if pin := r.Manifest.Requires[key].Pin; pin != "" {
		if !same {
			r.Warnings = append(r.Warnings, fmt.Sprintf("%s, but %s is pinned to %s.", asks, key, m.VersionNumber))
		}
		return false
	}
	if prev, ok := r.exact[key]; ok {
		if same {
			return false
		}
		keeps := !want.Published.After(prev.version.Published)
		newer := prev.version.Number
		if !keeps {
			newer = want.Number
		}
		r.Warnings = append(r.Warnings, fmt.Sprintf("%s and %s asks for %s; the pack takes the newer, %s.", asks, prev.by, prev.version.Number, newer))
		if keeps {
			return false
		}
	} else if m.Modpack != "" && !r.locked[key] {
		if !same {
			r.Warnings = append(r.Warnings, fmt.Sprintf("%s, but modpack %s keeps %s.", asks, m.Modpack, m.VersionNumber))
		}
		return false
	} else if !r.locked[key] && !r.withDeps {
		if !same {
			r.Warnings = append(r.Warnings, fmt.Sprintf("%s, but the pack keeps %s; `shulker pin %s %s` locks it, or add with --with-deps.", asks, m.VersionNumber, key, want.ID))
		}
		return false
	}
	if r.exact == nil {
		r.exact = map[string]exactAsk{}
	}
	r.exact[key] = exactAsk{by: parent, version: want}
	return !same
}
