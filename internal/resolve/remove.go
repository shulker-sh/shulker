package resolve

import (
	"sort"
	"strings"

	"shulker.sh/shulker/internal/out"
)

type Removed struct {
	Removed []string `json:"removed"`
	Pruned  []string `json:"pruned"`
}

func (r *Resolver) Remove(ids []string) (*Removed, error) {
	direct := r.directMods()
	for _, id := range ids {
		if _, ok := r.Manifest.Mods[id]; ok {
			continue
		}
		if d, ok := direct[id]; ok {
			return nil, out.Errorf("pack-provided", "%s is provided by pack %s; remove the pack or list the mod in shulker.json yourself", id, strings.Join(d.packs, ", "))
		}
		if m, ok := r.Lock.Mods[id]; ok {
			e := out.Errorf("not-direct", "%s is not in the manifest; it is required by %v", id, m.RequiredBy)
			e.Items = m.RequiredBy
			return nil, e
		}
		e := out.Errorf("mod-not-found", "%s is not in the manifest", id)
		e.Candidates = r.manifestIDs()
		return nil, e
	}
	res := &Removed{Removed: []string{}, Pruned: []string{}}
	for _, id := range ids {
		if contains(res.Removed, id) {
			continue
		}
		delete(r.Manifest.Mods, id)
		res.Removed = append(res.Removed, id)
	}
	direct = r.directMods()
	for _, id := range res.Removed {
		if _, stillDirect := direct[id]; stillDirect {
			continue
		}
		if _, locked := r.Lock.Mods[id]; locked {
			r.dropLocked(id)
		}
	}
	res.Pruned = r.pruneOrphans()
	return res, nil
}

func (r *Resolver) pruneOrphans() []string {
	pruned := []string{}
	direct := r.directMods()
	for changed := true; changed; {
		changed = false
		for _, id := range r.lockIDs() {
			if _, isDirect := direct[id]; isDirect || len(r.Lock.Mods[id].RequiredBy) > 0 {
				continue
			}
			r.dropLocked(id)
			pruned = append(pruned, id)
			changed = true
		}
	}
	sort.Strings(pruned)
	return pruned
}

func (r *Resolver) dropLocked(id string) {
	delete(r.Lock.Mods, id)
	r.dropRequiredBy(id)
}

func (r *Resolver) manifestIDs() []string {
	ids := make([]string, 0, len(r.Manifest.Mods))
	for id := range r.Manifest.Mods {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (r *Resolver) lockIDs() []string {
	ids := make([]string, 0, len(r.Lock.Mods))
	for id := range r.Lock.Mods {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
