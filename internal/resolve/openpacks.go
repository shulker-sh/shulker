package resolve

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/pack"
	"shulker.sh/shulker/internal/project"
)

// PackMode is how OpenPacks reads a project's modpacks. A relock reads local packs as they are on
// disk, since they have no version to hold back. Linked is the modpack a link just pointed the
// project at, resolved without the warning a moved modpack gets.
type PackMode struct {
	IsRelocking bool
	Linked      string
}

// OpenPacks reads p's modpacks through store and records them on p for the command's later steps.
// A new or moved modpack is resolved afresh, and so, on a relock, is one rereads says to take from
// disk; the rest open at their pins. The warnings say which modpacks were resolved and why.
func OpenPacks(ctx context.Context, store *pack.Store, p *project.Project, mode PackMode) ([]*pack.Loaded, []string, error) {
	prior := p.Packs
	if prior != nil && (prior.IsRelocking || !mode.IsRelocking) {
		return prior.Loaded, nil, nil
	}
	var warnings []string
	modpacks := p.Manifest.Modpacks()
	loaded := []*pack.Loaded{}
	for _, name := range slices.Sorted(maps.Keys(modpacks)) {
		mp := modpacks[name]
		pinned, ok := p.Lock.Modpacks[name]
		moved := ok && (pinned.Source != mp.Source || pinned.Path != mp.Path || pinned.File != mp.File || (mp.IsHosted() && len(project.HostedDifferences(name, mp, pinned)) > 0))
		isFresh := !ok || moved
		// An earlier read already resolved a new or moved modpack, and holds the rest at the pins a
		// relock reads them at too, so a relock reads again only what it would take from disk.
		if prior != nil && (isFresh || !rereads(p.Dir, mp, pinned)) {
			if i := slices.IndexFunc(prior.Loaded, func(l *pack.Loaded) bool { return l.Name == name }); i >= 0 {
				loaded = append(loaded, prior.Loaded[i])
				continue
			}
		}
		if isFresh || (mode.IsRelocking && rereads(p.Dir, mp, pinned)) {
			switch {
			case name == mode.Linked:
			case !ok && len(p.Lock.Modpacks) > 0:
				warnings = append(warnings, fmt.Sprintf("modpack %s is not in the lock yet; resolving it", name))
			case moved && mp.IsHosted():
				warnings = append(warnings, fmt.Sprintf("modpack %s has changed since the lock; resolving it", name))
			case moved:
				warnings = append(warnings, fmt.Sprintf("modpack %s has a new source since the lock; resolving it", name))
			}
			l, err := store.Resolve(ctx, name, mp)
			if err != nil {
				return nil, nil, err
			}
			warnings = append(warnings, scoped(name, l.Warnings)...)
			loaded = append(loaded, l)
			continue
		}
		l, warning, err := store.Open(ctx, name, mp, pinned)
		if err != nil {
			return nil, nil, err
		}
		if warning != "" {
			warnings = append(warnings, warning)
		}
		if mode.IsRelocking {
			store.WarnRawURL(l)
		}
		loaded = append(loaded, l)
	}
	p.Packs = &project.OpenedPacks{Loaded: loaded, IsRelocking: mode.IsRelocking}
	return loaded, warnings, nil
}

// scoped prefixes each warning with the modpack it is about.
func scoped(name string, warnings []string) []string {
	named := make([]string, 0, len(warnings))
	for _, w := range warnings {
		named = append(named, name+": "+w)
	}
	return named
}

// rereads reports whether a relock reads a modpack afresh rather than at its pin: a local directory
// always, and an archive when archiveMoved says so, taking changed bytes only when it follows them.
func rereads(dir string, mp manifest.Require, pinned lock.Modpack) bool {
	switch pack.KindOf(mp) {
	case pack.Local:
		return true
	case pack.File:
		return ArchiveMoved(dir, mp, pinned, mp.AutoUpdates())
	}
	return false
}

// ArchiveMoved reports whether an archive modpack has to be read again: it was locked or unlocked
// since, which the lock alone can't rebuild, or its bytes changed and followsBytes says to take them.
func ArchiveMoved(dir string, mp manifest.Require, pinned lock.Modpack, followsBytes bool) bool {
	if isLocked := mp.Locked == nil || *mp.Locked; isLocked != pinned.UsesLock {
		return true
	}
	return followsBytes && len(project.FileDifferences(dir, "", mp.File, pinned.File, pinned.Size, pinned.Sha512)) > 0
}
