package resolve

import (
	"context"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"sort"

	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/pack"
)

type ModpackChange struct {
	Name string `json:"name"`
	From string `json:"from"`
	To   string `json:"to"`
}

type directMod struct {
	entry manifest.Require
	packs []string
	// locked names the locked modpack this mod is copied from, so resolution
	// leaves it alone. Empty when the project resolves it itself.
	locked string
}

func (r *Resolver) directMods() map[string]directMod {
	own := r.Manifest.Mods()
	d := map[string]directMod{}
	for id, e := range own {
		d[id] = directMod{entry: e}
	}
	for _, p := range r.Packs {
		for _, id := range packMods(p) {
			cur, ok := d[id]
			if !ok {
				cur = directMod{entry: p.Manifest.Requires[id]}
			}
			cur.packs = append(cur.packs, p.Name)
			if _, project := own[id]; p.UsesLock && !project && cur.locked == "" {
				cur.locked = p.Name
			}
			d[id] = cur
		}
	}
	return d
}

// packMods lists the mod ids a modpack provides: the entries of its own lock
// when it is locked, dependencies included, and its manifest's otherwise.
func packMods(p *pack.Loaded) []string {
	if p.UsesLock && p.Lock != nil {
		return sortedKeys(p.Lock.Mods)
	}
	return sortedKeys(p.Manifest.Mods())
}

// directIDs lists the mods this project resolves itself. Mods a locked modpack
// pins are left out: they are copied from its lock, never resolved here.
func (r *Resolver) directIDs() []string {
	var ids []string
	for id, d := range r.directMods() {
		if d.locked != "" {
			continue
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// CheckPacks refuses modpacks that don't fit the project's platform, or that disagree about a mod
// the project doesn't list itself.
func (r *Resolver) CheckPacks() error {
	own := r.Manifest.Mods()
	owner := map[string]*pack.Loaded{}
	for _, p := range r.Packs {
		// With no platform yet, the locked modpacks are what supplies one, so there is
		// nothing to check them against until the next pass.
		if r.Lock.Minecraft != "" {
			if err := pack.Compatible(p, r.Lock.Minecraft, r.Lock.Loader); err != nil {
				return err
			}
		}
		for _, id := range packMods(p) {
			if _, project := own[id]; project {
				continue
			}
			prev, ok := owner[id]
			if !ok {
				owner[id] = p
				continue
			}
			if err := packConflict(prev, p, id); err != nil {
				return err
			}
		}
	}
	return nil
}

// packConflict reports two modpacks that disagree about one mod: locked ones by
// the version each pins, floating ones by the settings each lists. A locked
// modpack and a floating one don't conflict, because the locked version wins.
func packConflict(prev, p *pack.Loaded, id string) error {
	if prev.UsesLock || p.UsesLock {
		if !prev.UsesLock || !p.UsesLock {
			return nil
		}
		was, now := prev.Lock.Mods[id], p.Lock.Mods[id]
		if was.Sha512 != now.Sha512 {
			return packConflictError(fmt.Sprintf("locked modpacks %s and %s pin %s at different versions (%s and %s)", prev.Name, p.Name, id, was.VersionNumber, now.VersionNumber), id)
		}
		return nil
	}
	if !reflect.DeepEqual(prev.Manifest.Requires[id], p.Manifest.Requires[id]) {
		return packConflictError(fmt.Sprintf("modpacks %s and %s both list %s with different settings", prev.Name, p.Name, id), id)
	}
	return nil
}

func packConflictError(headline, id string) *out.Error {
	e := out.Errorf("modpack-conflict", "%s", headline)
	e.Help = fmt.Sprintf("list %s in shulker.json to decide", id)
	return e
}

func (r *Resolver) AddPack(ctx context.Context, l *pack.Loaded) error {
	before := r.directMods()
	r.Packs = append(r.Packs, l)
	if err := r.CheckPacks(); err != nil {
		r.Packs = r.Packs[:len(r.Packs)-1]
		return err
	}
	r.Lock.Modpacks[l.Name] = l.Pin
	if l.UsesLock {
		if err := r.applyLockedPacks(); err != nil {
			return err
		}
		for _, id := range sortedKeys(l.Manifest.Mods()) {
			r.Lock.AddRequiredBy(id, l.Name)
		}
		return nil
	}
	var targets []string
	for _, id := range sortedKeys(l.Manifest.Mods()) {
		if _, existed := before[id]; existed {
			r.Lock.AddRequiredBy(id, l.Name)
			continue
		}
		targets = append(targets, id)
	}
	if len(targets) == 0 {
		return nil
	}
	return r.Update(ctx, targets)
}

// applyLockedPacks copies every locked modpack's own lock entries into this
// project's lock, each marked with the modpack it came from, and caches the
// local files they name. A mod listed in shulker.json is skipped, because the
// project's own entry is resolved here and wins over what a modpack pins.
func (r *Resolver) applyLockedPacks() error {
	own := r.Manifest.Mods()
	for _, p := range r.Packs {
		if !p.UsesLock || p.Lock == nil {
			continue
		}
		if err := r.cachePackFiles(p); err != nil {
			return err
		}
		for _, id := range sortedKeys(p.Lock.Mods) {
			if _, project := own[id]; project {
				continue
			}
			entry := p.Lock.Mods[id]
			entry.Modpack = p.Name
			entry.RequiredBy = append([]string{}, entry.RequiredBy...)
			r.Lock.Mods[id] = entry
		}
		for _, key := range sortedKeys(p.Lock.ResourcePacks) {
			if _, listed := r.Manifest.Requires[key]; listed {
				continue
			}
			entry := p.Lock.ResourcePacks[key]
			entry.Modpack = p.Name
			r.Lock.ResourcePacks[key] = entry
		}
		for _, key := range sortedKeys(p.Lock.Shaders) {
			if _, listed := r.Manifest.Requires[key]; listed {
				continue
			}
			entry := p.Lock.Shaders[key]
			entry.Modpack = p.Name
			r.Lock.Shaders[key] = entry
		}
	}
	return nil
}

func (r *Resolver) RemovePack(name string) error {
	modpacks := r.Manifest.Modpacks()
	if _, ok := modpacks[name]; !ok {
		e := out.Errorf("modpack-not-found", "modpack %s is not in the manifest", name)
		e.Given, e.Candidates = name, slices.Sorted(maps.Keys(modpacks))
		return e
	}
	delete(r.Manifest.Requires, name)
	delete(r.Lock.Modpacks, name)
	kept := r.Packs[:0]
	for _, p := range r.Packs {
		if p.Name != name {
			kept = append(kept, p)
		}
	}
	r.Packs = kept
	r.dropRequiredBy(name)
	r.pruneOrphans()
	return nil
}

// RefreshPacks makes loaded the project's modpacks, pinning each in the lock and dropping the pins of
// any no longer loaded.
func (r *Resolver) RefreshPacks(loaded []*pack.Loaded) error {
	r.Packs = loaded
	if err := r.CheckPacks(); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, l := range loaded {
		seen[l.Name] = true
		r.Lock.Modpacks[l.Name] = l.Pin
	}
	for name := range r.Lock.Modpacks {
		if seen[name] {
			continue
		}
		delete(r.Lock.Modpacks, name)
		r.dropRequiredBy(name)
	}
	return nil
}

// ModpackChanges lists the modpack pins that differ between two locks.
func ModpackChanges(before, after map[string]lock.Modpack) []ModpackChange {
	changes := []ModpackChange{}
	for name, now := range after {
		old, had := before[name]
		if !had || old.Label() != now.Label() {
			changes = append(changes, ModpackChange{Name: name, From: old.Label(), To: now.Label()})
		}
	}
	for name, old := range before {
		if _, still := after[name]; !still {
			changes = append(changes, ModpackChange{Name: name, From: old.Label()})
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Name < changes[j].Name })
	return changes
}

func (r *Resolver) dropRequiredBy(name string) {
	for id, m := range r.Lock.Mods {
		kept := m.RequiredBy[:0]
		for _, by := range m.RequiredBy {
			if by != name {
				kept = append(kept, by)
			}
		}
		m.RequiredBy = kept
		r.Lock.Mods[id] = m
	}
}
