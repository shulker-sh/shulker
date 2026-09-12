package resolve

import (
	"strings"

	"shulker.sh/shulker/internal/lock"
)

type Snapshot struct {
	minecraft string
	loader    lock.Loader
	mods      map[string]lock.Mod
	listed    map[string]bool
	packs     map[string]lock.Pack
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
	FromSide     string `json:"fromSide,omitempty"`
	ToSide       string `json:"toSide,omitempty"`
	FromChannel  string `json:"fromChannel,omitempty"`
	ToChannel    string `json:"toChannel,omitempty"`
}

type RemovedMod struct {
	ID            string   `json:"id"`
	VersionNumber string   `json:"versionNumber"`
	RequiredBy    []string `json:"requiredBy"`
	StillLocked   bool     `json:"stillLocked,omitempty"`
}

type Changes struct {
	Platform []Change     `json:"platform"`
	Added    []AddedMod   `json:"added"`
	Updated  []Change     `json:"updated"`
	Removed  []RemovedMod `json:"removed"`
	Packs    []PackChange `json:"packs"`
}

func (c *Changes) Empty() bool {
	return len(c.Platform)+len(c.Added)+len(c.Updated)+len(c.Removed)+len(c.Packs) == 0
}

// Snapshot copies each RequiredBy because the resolver filters those slices in place.
func (r *Resolver) Snapshot() Snapshot {
	s := Snapshot{minecraft: r.Lock.Minecraft, loader: r.Lock.Loader, mods: map[string]lock.Mod{}, listed: map[string]bool{}, packs: map[string]lock.Pack{}}
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
	c := &Changes{Platform: []Change{}, Added: []AddedMod{}, Updated: []Change{}, Removed: []RemovedMod{}, Packs: PackChanges(before.packs, r.Lock.Packs)}
	if before.minecraft != r.Lock.Minecraft {
		c.Platform = append(c.Platform, Change{ID: "minecraft", From: before.minecraft, To: r.Lock.Minecraft})
	}
	if from, to := loaderLabel(before.loader), loaderLabel(r.Lock.Loader); from != to {
		c.Platform = append(c.Platform, Change{ID: "loader", From: from, To: to})
	}
	for _, id := range r.lockIDs() {
		now := r.Lock.Mods[id]
		old, existed := before.mods[id]
		_, listed := r.Manifest.Mods[id]
		switch {
		case !existed || (listed && !before.listed[id]):
			c.Added = append(c.Added, AddedMod{ID: id, VersionNumber: now.VersionNumber, Side: now.Side, Provider: now.Provider, RequiredBy: nonNil(now.RequiredBy), AlreadyLocked: existed})
		case old.Sha512 != now.Sha512 || old.Provider != now.Provider || old.Side != now.Side || old.Channel != now.Channel:
			ch := Change{ID: id, From: old.VersionNumber, To: now.VersionNumber}
			if old.Provider != now.Provider {
				ch.FromProvider, ch.ToProvider = old.Provider, now.Provider
			}
			if old.Side != now.Side {
				ch.FromSide, ch.ToSide = old.Side, now.Side
			}
			if old.Channel != now.Channel {
				ch.FromChannel, ch.ToChannel = old.Channel, now.Channel
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

func loaderLabel(l lock.Loader) string {
	return strings.TrimSpace(l.Type + " " + l.Version)
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return append([]string{}, s...)
}
