package resolve

import "shulker.sh/shulker/internal/lock"

type Snapshot struct {
	mods   map[string]lock.Mod
	listed map[string]bool
	packs  map[string]lock.Pack
}

type AddedMod struct {
	ID            string   `json:"id"`
	VersionNumber string   `json:"versionNumber"`
	Side          string   `json:"side"`
	Provider      string   `json:"provider"`
	RequiredBy    []string `json:"requiredBy"`
	AlreadyLocked bool     `json:"alreadyLocked,omitempty"`
}

type Change struct {
	ID           string `json:"id"`
	From         string `json:"from"`
	To           string `json:"to"`
	FromProvider string `json:"fromProvider,omitempty"`
	ToProvider   string `json:"toProvider,omitempty"`
}

type RemovedMod struct {
	ID            string   `json:"id"`
	VersionNumber string   `json:"versionNumber"`
	RequiredBy    []string `json:"requiredBy"`
	StillLocked   bool     `json:"stillLocked,omitempty"`
}

type Changes struct {
	Added   []AddedMod   `json:"added"`
	Updated []Change     `json:"updated"`
	Removed []RemovedMod `json:"removed"`
	Packs   []PackChange `json:"packs"`
}

func (c *Changes) Empty() bool {
	return len(c.Added)+len(c.Updated)+len(c.Removed)+len(c.Packs) == 0
}

// Snapshot copies each RequiredBy because the resolver filters those slices in place.
func (r *Resolver) Snapshot() Snapshot {
	s := Snapshot{mods: map[string]lock.Mod{}, listed: map[string]bool{}, packs: map[string]lock.Pack{}}
	for id, m := range r.Lock.Mods {
		m.RequiredBy = append([]string{}, m.RequiredBy...)
		s.mods[id] = m
	}
	for id := range r.Manifest.Mods {
		s.listed[id] = true
	}
	for source, pin := range r.Lock.Packs {
		s.packs[source] = pin
	}
	return s
}

func (r *Resolver) Changes(before Snapshot) *Changes {
	c := &Changes{Added: []AddedMod{}, Updated: []Change{}, Removed: []RemovedMod{}, Packs: PackChanges(before.packs, r.Lock.Packs)}
	for _, id := range r.lockIDs() {
		now := r.Lock.Mods[id]
		old, existed := before.mods[id]
		_, listed := r.Manifest.Mods[id]
		switch {
		case !existed || (listed && !before.listed[id]):
			c.Added = append(c.Added, AddedMod{ID: id, VersionNumber: now.VersionNumber, Side: now.Side, Provider: now.Provider, RequiredBy: nonNil(now.RequiredBy), AlreadyLocked: existed})
		case old.Sha512 != now.Sha512 || old.Provider != now.Provider:
			ch := Change{ID: id, From: old.VersionNumber, To: now.VersionNumber}
			if old.Provider != now.Provider {
				ch.FromProvider, ch.ToProvider = old.Provider, now.Provider
			}
			c.Updated = append(c.Updated, ch)
		}
	}
	for _, id := range sortedKeys(before.mods) {
		old := before.mods[id]
		now, locked := r.Lock.Mods[id]
		_, listed := r.Manifest.Mods[id]
		switch {
		case !locked:
			c.Removed = append(c.Removed, RemovedMod{ID: id, VersionNumber: old.VersionNumber, RequiredBy: nonNil(old.RequiredBy)})
		case before.listed[id] && !listed:
			c.Removed = append(c.Removed, RemovedMod{ID: id, VersionNumber: now.VersionNumber, RequiredBy: nonNil(now.RequiredBy), StillLocked: true})
		}
	}
	return c
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return append([]string{}, s...)
}
