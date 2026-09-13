package resolve

import (
	"context"
	"reflect"
	"sort"

	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/pack"
)

type PackChange struct {
	Name string `json:"name"`
	From string `json:"from"`
	To   string `json:"to"`
}

type directMod struct {
	entry manifest.Mod
	packs []string
}

func (r *Resolver) directMods() map[string]directMod {
	d := map[string]directMod{}
	for id, e := range r.Manifest.Mods {
		d[id] = directMod{entry: e}
	}
	for _, p := range r.Packs {
		for id, e := range p.Manifest.Mods {
			cur, ok := d[id]
			if !ok {
				cur = directMod{entry: e}
			}
			cur.packs = append(cur.packs, p.Name)
			d[id] = cur
		}
	}
	return d
}

func (r *Resolver) directIDs() []string {
	return sortedKeys(r.directMods())
}

func (r *Resolver) CheckPacks() error {
	owner := map[string]*pack.Loaded{}
	for _, p := range r.Packs {
		if err := pack.Compatible(p, r.Lock.Minecraft, r.Lock.Loader); err != nil {
			return err
		}
		for _, id := range sortedKeys(p.Manifest.Mods) {
			if _, project := r.Manifest.Mods[id]; project {
				continue
			}
			prev, ok := owner[id]
			if !ok {
				owner[id] = p
				continue
			}
			if !reflect.DeepEqual(prev.Manifest.Mods[id], p.Manifest.Mods[id]) {
				return out.Errorf("pack-conflict", "packs %s and %s both list %s with different settings; list %s in shulker.json to decide", prev.Name, p.Name, id, id)
			}
		}
	}
	return nil
}

func (r *Resolver) AddPack(ctx context.Context, l *pack.Loaded) error {
	before := r.directMods()
	r.Packs = append(r.Packs, l)
	if err := r.CheckPacks(); err != nil {
		r.Packs = r.Packs[:len(r.Packs)-1]
		return err
	}
	r.Lock.Packs[l.Source] = l.Pin
	var targets []string
	for _, id := range sortedKeys(l.Manifest.Mods) {
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

func (r *Resolver) RemovePack(name string) error {
	idx := -1
	for i, p := range r.Manifest.Packs {
		n, err := pack.Name(p)
		if err != nil {
			return err
		}
		if n == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		e := out.Errorf("pack-not-found", "pack %s is not in the manifest", name)
		e.Given = name
		for _, p := range r.Manifest.Packs {
			if n, err := pack.Name(p); err == nil {
				e.Candidates = append(e.Candidates, n)
			}
		}
		return e
	}
	source := r.Manifest.Packs[idx].Source
	r.Manifest.Packs = append(r.Manifest.Packs[:idx:idx], r.Manifest.Packs[idx+1:]...)
	if len(r.Manifest.Packs) == 0 {
		r.Manifest.Packs = nil
	}
	delete(r.Lock.Packs, source)
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

func (r *Resolver) RefreshPacks(loaded []*pack.Loaded) error {
	r.Packs = loaded
	if err := r.CheckPacks(); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, l := range loaded {
		seen[l.Source] = true
		r.Lock.Packs[l.Source] = l.Pin
	}
	for source, old := range r.Lock.Packs {
		if seen[source] {
			continue
		}
		delete(r.Lock.Packs, source)
		r.dropRequiredBy(old.Name)
	}
	return nil
}

func PackChanges(before, after map[string]lock.Pack) []PackChange {
	changes := []PackChange{}
	for source, now := range after {
		old, had := before[source]
		if !had || old.Label() != now.Label() {
			changes = append(changes, PackChange{Name: now.Name, From: old.Label(), To: now.Label()})
		}
	}
	for source, old := range before {
		if _, still := after[source]; !still {
			changes = append(changes, PackChange{Name: old.Name, From: old.Label()})
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
