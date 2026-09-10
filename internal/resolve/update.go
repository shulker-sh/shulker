package resolve

import (
	"context"
	"fmt"
	"sort"

	"github.com/shulker-sh/shulker/internal/lock"
	"github.com/shulker-sh/shulker/internal/out"
	"github.com/shulker-sh/shulker/internal/provider"
)

type Change struct {
	ID   string `json:"id"`
	From string `json:"from"`
	To   string `json:"to"`
}

type Updated struct {
	Updated []Change     `json:"updated"`
	Added   []string     `json:"added"`
	Removed []string     `json:"removed"`
	Packs   []PackChange `json:"packs"`
}

type Outdated struct {
	ID      string `json:"id"`
	Current string `json:"current"`
	Latest  string `json:"latest"`
	Pinned  bool   `json:"pinned"`
}

func (r *Resolver) Update(ctx context.Context, ids []string) (*Updated, error) {
	targets, err := r.directTargets(ids)
	if err != nil {
		return nil, err
	}
	scope := r.scope(targets)
	before := map[string]lock.Mod{}
	for id := range r.Lock.Mods {
		before[id] = r.Lock.Mods[id]
	}
	for id := range scope {
		delete(r.Lock.Mods, id)
	}
	for id, m := range r.Lock.Mods {
		kept := m.RequiredBy[:0]
		for _, by := range m.RequiredBy {
			if _, inScope := scope[by]; !inScope {
				kept = append(kept, by)
			}
		}
		m.RequiredBy = kept
		r.Lock.Mods[id] = m
	}
	for _, id := range targets {
		if err := r.relock(ctx, id); err != nil {
			return nil, err
		}
	}
	for id := range scope {
		old := before[id]
		var kept []string
		for _, by := range old.RequiredBy {
			if _, inScope := scope[by]; inScope {
				continue
			}
			if _, present := r.Lock.Mods[by]; present {
				kept = append(kept, by)
			}
		}
		if _, placed := r.Lock.Mods[id]; placed {
			for _, by := range kept {
				r.Lock.AddRequiredBy(id, by)
			}
			continue
		}
		if len(kept) > 0 {
			old.RequiredBy = kept
			r.Lock.Mods[id] = old
		}
	}
	r.pruneOrphans()
	res := &Updated{Updated: []Change{}, Added: []string{}, Removed: []string{}, Packs: []PackChange{}}
	for _, id := range r.lockIDs() {
		old, existed := before[id]
		switch {
		case !existed:
			res.Added = append(res.Added, id)
		case old.Sha512 != r.Lock.Mods[id].Sha512:
			res.Updated = append(res.Updated, Change{ID: id, From: old.VersionNumber, To: r.Lock.Mods[id].VersionNumber})
		}
	}
	for _, id := range sortedKeys(before) {
		if _, present := r.Lock.Mods[id]; !present {
			res.Removed = append(res.Removed, id)
		}
	}
	return res, nil
}

func (r *Resolver) Outdated(ctx context.Context, ids []string) ([]Outdated, error) {
	targets, err := r.directTargets(ids)
	if err != nil {
		return nil, err
	}
	scope := r.scope(targets)
	var res []Outdated
	for _, id := range sortedKeys(scope) {
		m := r.Lock.Mods[id]
		p, err := r.provider(m.Provider)
		if err != nil {
			return nil, err
		}
		versions, err := p.Versions(ctx, fmt.Sprint(m.Project), r.Lock.Minecraft, r.Lock.Loader.Type)
		if err != nil {
			return nil, err
		}
		newest, ok := provider.Newest(versions, r.channelFor(id))
		if !ok || newest.ID == fmt.Sprint(m.Version) {
			continue
		}
		entry := r.Manifest.Mods[id]
		res = append(res, Outdated{ID: id, Current: m.VersionNumber, Latest: newest.Number, Pinned: entry.Pin != nil})
	}
	if res == nil {
		res = []Outdated{}
	}
	return res, nil
}

func (r *Resolver) Pin(ctx context.Context, id string, version string) (*Updated, string, error) {
	if _, err := r.directTargets([]string{id}); err != nil {
		return nil, "", err
	}
	if version == "" {
		version = fmt.Sprint(r.Lock.Mods[id].Version)
	}
	entry := r.Manifest.Mods[id]
	entry.Pin = lockID(r.Lock.Mods[id].Provider, version)
	r.Manifest.Mods[id] = entry
	res, err := r.Update(ctx, []string{id})
	return res, version, err
}

func (r *Resolver) Unpin(ctx context.Context, id string) (*Updated, error) {
	if _, err := r.directTargets([]string{id}); err != nil {
		return nil, err
	}
	entry := r.Manifest.Mods[id]
	if entry.Pin == nil {
		return nil, out.Errorf("not-pinned", "%s is not pinned", id)
	}
	entry.Pin = nil
	r.Manifest.Mods[id] = entry
	return r.Update(ctx, []string{id})
}

func (r *Resolver) relock(ctx context.Context, id string) error {
	direct := r.directMods()[id]
	entry := direct.entry
	p, err := r.provider(entry.Provider)
	if err != nil {
		return err
	}
	key := id
	if entry.Project != nil {
		key = fmt.Sprint(entry.Project)
	}
	proj, err := p.Project(ctx, key)
	if err != nil {
		return err
	}
	pin := ""
	if entry.Pin != nil {
		pin = fmt.Sprint(entry.Pin)
	}
	v, err := r.pick(ctx, p, proj, pin, entry.Channel)
	if err != nil {
		return err
	}
	placed, _, err := r.place(ctx, p, proj, v, "", entry.Side)
	if err != nil {
		return err
	}
	if placed != id {
		return out.Errorf("id-changed", "%s %s identifies itself as %s; remove it and add it again", id, v.Number, placed)
	}
	for _, name := range direct.packs {
		r.Lock.AddRequiredBy(id, name)
	}
	visited := map[string]bool{proj.ID: true}
	return r.addDeps(ctx, p, v, id, entry.Channel, &Added{ID: id}, visited)
}

func (r *Resolver) directTargets(ids []string) ([]string, error) {
	if len(ids) == 0 {
		return r.directIDs(), nil
	}
	direct := r.directMods()
	var targets []string
	for _, id := range ids {
		if _, ok := direct[id]; ok {
			if !contains(targets, id) {
				targets = append(targets, id)
			}
			continue
		}
		if m, ok := r.Lock.Mods[id]; ok {
			e := out.Errorf("not-direct", "%s is not in the manifest; it is required by %v", id, m.RequiredBy)
			e.Candidates = m.RequiredBy
			return nil, e
		}
		e := out.Errorf("not-found", "%s is not in the manifest", id)
		e.Candidates = r.directIDs()
		return nil, e
	}
	sort.Strings(targets)
	return targets, nil
}

func (r *Resolver) scope(targets []string) map[string]lock.Mod {
	scope := map[string]lock.Mod{}
	direct := r.directMods()
	for _, id := range targets {
		if m, ok := r.Lock.Mods[id]; ok {
			scope[id] = m
		}
	}
	for changed := true; changed; {
		changed = false
		for id, m := range r.Lock.Mods {
			if _, done := scope[id]; done {
				continue
			}
			if _, isDirect := direct[id]; isDirect {
				continue
			}
			for _, by := range m.RequiredBy {
				if _, inScope := scope[by]; inScope {
					scope[id] = m
					changed = true
					break
				}
			}
		}
	}
	return scope
}

func (r *Resolver) channelFor(id string) string {
	best := ""
	visited := map[string]bool{}
	var walk func(string)
	walk = func(cur string) {
		if visited[cur] {
			return
		}
		visited[cur] = true
		if entry, direct := r.Manifest.Mods[cur]; direct {
			if channelRank(entry.Channel) > channelRank(best) {
				best = entry.Channel
			}
		}
		for _, by := range r.Lock.Mods[cur].RequiredBy {
			walk(by)
		}
	}
	walk(id)
	return best
}

func channelRank(channel string) int {
	switch channel {
	case "alpha":
		return 2
	case "beta":
		return 1
	}
	return 0
}
