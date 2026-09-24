package project

import (
	"maps"
	"slices"

	"shulker.sh/shulker/internal/manifest"
)

// ModpackKey is the key an instance follows a link's source under: the source manifest's name
// when the link wrote the entry, and whatever an earlier link or a hand edit chose when it didn't.
// Empty where no single entry is the link's: with several packs required, none of them is the one.
func ModpackKey(m *manifest.Manifest, source string) string {
	keys := slices.Sorted(maps.Keys(m.Modpacks()))
	for _, key := range keys {
		if m.Requires[key].Source == source {
			return key
		}
	}
	if len(keys) == 1 {
		return keys[0]
	}
	return ""
}
