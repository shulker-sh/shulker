package resolve

import (
	"fmt"

	"shulker.sh/shulker/internal/jarmeta"
	"shulker.sh/shulker/internal/lock"
)

// Where a locked mod's side came from, the lock's sideFrom. The default both has none.
const (
	sideFromRequires     = "requires"
	sideFromProvider     = "provider"
	sideFromJar          = "jar"
	sideFromDependencies = "dependencies"
)

// jarSide is the side info's metadata gives the mod, and where it came from.
func jarSide(info *jarmeta.Info) (side, from string) {
	switch {
	case info.SideFromDependencies:
		return info.Side, sideFromDependencies
	case info.Side != "" && info.Side != "both":
		return info.Side, sideFromJar
	}
	return "both", ""
}

// DependencySides is a note for each mod added or moved to a side that was read from its mods.toml's
// dependency sides. That rule can take a mod off the server, so it says so when it does.
func (c *Changes) DependencySides(mods map[string]lock.Mod) []string {
	var notes []string
	note := func(id string) {
		if m := mods[id]; m.SideFrom == sideFromDependencies && m.Side == "client" {
			notes = append(notes, fmt.Sprintf("%s is client-only: every dependency in its mods.toml is CLIENT; shulker set requires.%s.side both places it on the server too", id, id))
		}
	}
	for _, m := range c.Added {
		note(m.ID)
	}
	for _, ch := range c.Updated {
		if ch.ToSide != "" {
			note(ch.ID)
		}
	}
	return notes
}
