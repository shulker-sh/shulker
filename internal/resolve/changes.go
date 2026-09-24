package resolve

import (
	"fmt"
	"slices"
	"strings"

	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/lock"
)

// Snapshot is the lock as it stood before a change, for Changes to compare against.
type Snapshot struct {
	minecraft string
	loader    lock.Loader
	mods      map[string]lock.Mod
	listed    map[string]bool
	packs     map[string]lock.Modpack
	zips      map[string]lock.Pack
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
	Platform []Change        `json:"platform"`
	Added    []AddedMod      `json:"added"`
	Updated  []Change        `json:"updated"`
	Removed  []RemovedMod    `json:"removed"`
	Modpacks []ModpackChange `json:"modpacks"`
}

func (c *Changes) IsEmpty() bool {
	return len(c.Platform)+len(c.Added)+len(c.Updated)+len(c.Removed)+len(c.Modpacks) == 0
}

// Snapshot copies each RequiredBy because the resolver filters those slices in place.
func (r *Resolver) Snapshot() Snapshot {
	s := Snapshot{minecraft: r.Lock.Minecraft, loader: r.Lock.Loader, mods: map[string]lock.Mod{}, listed: map[string]bool{}, packs: map[string]lock.Modpack{}}
	for id, m := range r.Lock.Mods {
		m.RequiredBy = append([]string{}, m.RequiredBy...)
		s.mods[id] = m
	}
	for id := range r.Manifest.Mods() {
		s.listed[id] = true
	}
	for source, pin := range r.Lock.Modpacks {
		s.packs[source] = pin
	}
	s.zips = r.lockedPacks()
	return s
}

// Changes is what the lock gained, lost and moved since before.
func (r *Resolver) Changes(before Snapshot) *Changes {
	c := &Changes{Platform: []Change{}, Added: []AddedMod{}, Updated: []Change{}, Removed: []RemovedMod{}, Modpacks: ModpackChanges(before.packs, r.Lock.Modpacks)}
	if before.minecraft != r.Lock.Minecraft {
		c.Platform = append(c.Platform, Change{ID: "minecraft", From: before.minecraft, To: r.Lock.Minecraft})
	}
	if from, to := loaderLabel(before.loader), loaderLabel(r.Lock.Loader); from != to {
		c.Platform = append(c.Platform, Change{ID: "loader", From: from, To: to})
	}
	mods := r.Manifest.Mods()
	for _, id := range r.lockIDs() {
		now := r.Lock.Mods[id]
		old, existed := before.mods[id]
		_, listed := mods[id]
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
		_, listed := mods[id]
		switch {
		case !locked:
			c.Removed = append(c.Removed, RemovedMod{ID: id, VersionNumber: old.VersionNumber, RequiredBy: nonNil(old.RequiredBy)})
		case before.listed[id] && !listed:
			c.Removed = append(c.Removed, RemovedMod{ID: id, VersionNumber: now.VersionNumber, RequiredBy: nonNil(now.RequiredBy), StillLocked: true})
		}
	}
	zips := r.lockedPacks()
	for _, key := range sortedKeys(zips) {
		p := zips[key]
		old, existed := before.zips[key]
		switch {
		case !existed:
			c.Added = append(c.Added, AddedMod{ID: key, VersionNumber: p.VersionNumber, Side: "client", Provider: p.Provider, RequiredBy: []string{}})
		case old.Sha512 != p.Sha512 || old.Provider != p.Provider || old.Channel != p.Channel:
			ch := Change{ID: key, From: old.VersionNumber, To: p.VersionNumber}
			if old.Provider != p.Provider {
				ch.FromProvider, ch.ToProvider = old.Provider, p.Provider
			}
			if old.Channel != p.Channel {
				ch.FromChannel, ch.ToChannel = old.Channel, p.Channel
			}
			c.Updated = append(c.Updated, ch)
		}
	}
	for _, key := range sortedKeys(before.zips) {
		if _, still := zips[key]; !still {
			c.Removed = append(c.Removed, RemovedMod{ID: key, VersionNumber: before.zips[key].VersionNumber, RequiredBy: []string{}})
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

// Unshipped is the warning for each mod added on a side the project does not declare, and
// unconditioned, so no side of the project ships it. A required mod, a pack or one that a
// condition already keeps off a side gets none.
func (c *Changes) Unshipped(sides []string, mods map[string]lock.Mod, placements map[string]build.Placement) []string {
	var warnings []string
	for _, m := range c.Added {
		if _, isMod := mods[m.ID]; !isMod || len(m.RequiredBy) > 0 || len(sides) == 0 || IsSideDeclared(sides, m.Side) {
			continue
		}
		if place := placements[m.ID]; len(place.OS) > 0 || len(place.Feature) > 0 {
			continue
		}
		warnings = append(warnings, fmt.Sprintf("%s is %s only, so no side of this project ships it; shulker set requires.%s.side both ships it anyway", m.ID, m.Side, m.ID))
	}
	return warnings
}

// IsSideDeclared reports whether a mod's side is one the project declares; a mod on both is
// declared whenever the project has any side.
func IsSideDeclared(sides []string, side string) bool {
	if side == "" || side == "both" {
		return len(sides) > 0
	}
	return slices.Contains(sides, side)
}
