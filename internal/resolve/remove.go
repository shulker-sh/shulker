package resolve

import (
	"sort"

	"github.com/andrewmast/shulker/internal/out"
)

type Removed struct {
	Removed []string `json:"removed"`
	Pruned  []string `json:"pruned"`
}

func (r *Resolver) Remove(ids []string) (*Removed, error) {
	for _, id := range ids {
		if _, ok := r.Manifest.Mods[id]; ok {
			continue
		}
		if m, ok := r.Lock.Mods[id]; ok {
			e := out.Errorf("not-direct", "%s is not in the manifest; it is required by %v", id, m.RequiredBy)
			e.Candidates = m.RequiredBy
			return nil, e
		}
		e := out.Errorf("not-found", "%s is not in the manifest", id)
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
	for _, id := range res.Removed {
		if _, locked := r.Lock.Mods[id]; locked {
			r.dropLocked(id)
		}
	}
	for changed := true; changed; {
		changed = false
		for _, id := range r.lockIDs() {
			if _, direct := r.Manifest.Mods[id]; direct || len(r.Lock.Mods[id].RequiredBy) > 0 {
				continue
			}
			r.dropLocked(id)
			res.Pruned = append(res.Pruned, id)
			changed = true
		}
	}
	sort.Strings(res.Pruned)
	return res, nil
}

func (r *Resolver) dropLocked(id string) {
	delete(r.Lock.Mods, id)
	for other, m := range r.Lock.Mods {
		kept := m.RequiredBy[:0]
		for _, by := range m.RequiredBy {
			if by != id {
				kept = append(kept, by)
			}
		}
		m.RequiredBy = kept
		r.Lock.Mods[other] = m
	}
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
