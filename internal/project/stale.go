package project

import (
	"fmt"
	"maps"
	"slices"
	"strconv"

	"shulker.sh/shulker/internal/loaderver"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/mcver"
	"shulker.sh/shulker/internal/pack"
)

func (p *Project) LockStale() bool {
	return p.Lock == nil || len(p.LockDifferences()) > 0
}

func (p *Project) LockDifferences() []string {
	if p.Lock == nil {
		return []string{"no shulker.lock"}
	}
	m, l := p.Manifest, p.Lock
	diffs := append(PlatformDifferences(m, l), ProviderDifferences(m, l)...)
	diffs = append(diffs, PackDifferences(m, l)...)
	for _, id := range slices.Sorted(maps.Keys(m.Mods)) {
		lm, ok := l.Mods[id]
		if !ok {
			diffs = append(diffs, id+": in shulker.json, not in shulker.lock")
			continue
		}
		diffs = append(diffs, ModDifferences(id, m.Mods[id], lm)...)
	}
	for _, id := range slices.Sorted(maps.Keys(l.Mods)) {
		if _, listed := m.Mods[id]; !listed && len(l.Mods[id].RequiredBy) == 0 {
			diffs = append(diffs, id+": in shulker.lock, not in shulker.json")
		}
	}
	return diffs
}

func PlatformDifferences(m *manifest.Manifest, l *lock.Lock) []string {
	var diffs []string
	switch {
	case l.Minecraft == "":
		diffs = append(diffs, "minecraft: not in shulker.lock")
	case !minecraftMatches(m.Minecraft, l.Minecraft):
		diffs = append(diffs, fmt.Sprintf("minecraft: locked %s is outside %s", l.Minecraft, m.Minecraft))
	}
	switch {
	case l.Loader.Type == "":
		diffs = append(diffs, "loader: not in shulker.lock")
	case l.Loader.Type != m.Loader.Type:
		diffs = append(diffs, fmt.Sprintf("loader: locked %s, shulker.json asks for %s", l.Loader.Type, m.Loader.Type))
	case !loaderMatches(m.Loader.Version, l.Loader.Version):
		diffs = append(diffs, fmt.Sprintf("loader: locked %s %s is outside %s", l.Loader.Type, l.Loader.Version, m.Loader.Version))
	}
	return diffs
}

func ProviderDifferences(m *manifest.Manifest, l *lock.Lock) []string {
	order := m.ProviderOrder()
	var diffs []string
	for _, id := range slices.Sorted(maps.Keys(l.Mods)) {
		if name := l.Mods[id].Provider; !slices.Contains(order, name) {
			diffs = append(diffs, fmt.Sprintf("providers: %s is locked from %s, which shulker.json does not list", id, name))
		}
	}
	return diffs
}

func PackDifferences(m *manifest.Manifest, l *lock.Lock) []string {
	var diffs []string
	listed := map[string]bool{}
	for _, mp := range m.Packs {
		listed[mp.Source] = true
		name, err := pack.Name(mp)
		if err != nil {
			name = mp.Source
		}
		lp, ok := l.Packs[mp.Source]
		switch {
		case !ok:
			diffs = append(diffs, fmt.Sprintf("pack %s: in shulker.json, not in shulker.lock", name))
		case lp.Ref != mp.Ref:
			diffs = append(diffs, fmt.Sprintf("pack %s: ref %q -> %q", name, lp.Ref, mp.Ref))
		case lp.Name != name:
			diffs = append(diffs, fmt.Sprintf("pack %s: name %s -> %s", name, lp.Name, name))
		}
	}
	for _, source := range slices.Sorted(maps.Keys(l.Packs)) {
		if !listed[source] {
			diffs = append(diffs, fmt.Sprintf("pack %s: in shulker.lock, not in shulker.json", l.Packs[source].Name))
		}
	}
	return diffs
}

func ModDifferences(id string, e manifest.Mod, lm lock.Mod) []string {
	var diffs []string
	channel := e.Channel
	if channel == "" {
		channel = "release"
	}
	if channel != lm.Channel {
		diffs = append(diffs, fmt.Sprintf("%s: channel %s -> %s", id, lm.Channel, channel))
	}
	if e.Pin != nil && fmt.Sprint(e.Pin) != fmt.Sprint(lm.Version) {
		diffs = append(diffs, fmt.Sprintf("%s: pinned to %v, locked %v", id, e.Pin, lm.Version))
	}
	if e.Side != "" && e.Side != lm.Side {
		diffs = append(diffs, fmt.Sprintf("%s: side %s -> %s", id, lm.Side, e.Side))
	}
	provider := e.Provider
	if provider == "" {
		provider = lm.Provider
	}
	project, ok := lockedProject(lm, provider)
	switch {
	case !ok:
		diffs = append(diffs, fmt.Sprintf("%s: provider %s -> %s", id, lm.Provider, provider))
	case e.Project != nil && fmt.Sprint(e.Project) != project:
		diffs = append(diffs, fmt.Sprintf("%s: project %s -> %v", id, project, e.Project))
	}
	return diffs
}

func lockedProject(lm lock.Mod, provider string) (string, bool) {
	switch {
	case provider == lm.Provider:
		return fmt.Sprint(lm.Project), true
	case provider == "modrinth" && lm.Aliases.Modrinth != "":
		return lm.Aliases.Modrinth, true
	case provider == "curseforge" && lm.Aliases.CurseForge != 0:
		return strconv.Itoa(lm.Aliases.CurseForge), true
	}
	return "", false
}

func minecraftMatches(raw, id string) bool {
	rng, err := mcver.ParseRange(raw)
	if err != nil {
		return false
	}
	v, err := mcver.Parse(id)
	return err == nil && rng.Matches(v)
}

func loaderMatches(raw, id string) bool {
	rng, err := loaderver.ParseRange(raw)
	if err != nil {
		return false
	}
	v, err := loaderver.Parse(id)
	return err == nil && rng.Matches(v)
}
