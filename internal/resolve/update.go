package resolve

import (
	"context"
	"fmt"
	"sort"

	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/provider"
)

type Outdated struct {
	ID      string `json:"id"`
	Current string `json:"current"`
	Latest  string `json:"latest"`
	Pinned  bool   `json:"pinned"`
}

func (r *Resolver) Update(ctx context.Context, ids []string) error {
	targets, err := r.directTargets(ids)
	if err != nil {
		return err
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
			return err
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
	return nil
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
		versions, err := p.Versions(ctx, fmt.Sprint(m.Project), r.Lock.Minecraft, loader.ProviderLoaders(r.Lock.Loader.Type))
		if err != nil {
			return nil, err
		}
		newest, ok := provider.Newest(versions, r.channelFor(id), r.Lock.Loader.Type)
		if !ok || newest.ID == fmt.Sprint(m.Version) {
			continue
		}
		entry := r.Manifest.Mods()[id]
		res = append(res, Outdated{ID: id, Current: m.VersionNumber, Latest: newest.Number, Pinned: entry.Pin != nil})
	}
	if res == nil {
		res = []Outdated{}
	}
	return res, nil
}

func (r *Resolver) Pin(ctx context.Context, id string, version string) (string, error) {
	if _, err := r.directTargets([]string{id}); err != nil {
		return "", err
	}
	if version == "" {
		version = fmt.Sprint(r.Lock.Mods[id].Version)
	}
	entry := r.Manifest.Mods()[id]
	entry.Pin = lockID(r.Lock.Mods[id].Provider, version)
	r.Manifest.Requires[id] = entry
	return version, r.Update(ctx, []string{id})
}

func (r *Resolver) Unpin(ctx context.Context, id string) error {
	if _, err := r.directTargets([]string{id}); err != nil {
		return err
	}
	entry := r.Manifest.Mods()[id]
	if entry.Pin == nil {
		return out.Errorf("not-pinned", "%s is not pinned", id)
	}
	entry.Pin = nil
	r.Manifest.Requires[id] = entry
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
	if _, _, err := r.place(ctx, p, proj, v, id, "", entry.Side, entry.Channel, false); err != nil {
		return err
	}
	r.settle(id, entry.Side, entry.Channel)
	for _, name := range direct.packs {
		r.Lock.AddRequiredBy(id, name)
	}
	visited := map[string]bool{proj.ID: true}
	return r.addDeps(ctx, p, v, id, entry.Channel, visited)
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
			e.Items = m.RequiredBy
			return nil, e
		}
		e := out.Errorf("mod-not-found", "%s is not in the manifest", id)
		e.Candidates, e.Given = r.directIDs(), id
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
	mods := r.Manifest.Mods()
	visited := map[string]bool{}
	var walk func(string)
	walk = func(cur string) {
		if visited[cur] {
			return
		}
		visited[cur] = true
		if entry, direct := mods[cur]; direct {
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
