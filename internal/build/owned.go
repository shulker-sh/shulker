package build

import (
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/packarchive"
)

// DropManifestOwned leaves out the override files the manifest renders itself: the client options
// when the manifest sets them, and a server's properties and player lists.
func DropManifestOwned(m *manifest.Manifest, overrides []packarchive.Override) []packarchive.Override {
	owned := map[string]bool{}
	if m.Client != nil && m.Client.Options != nil {
		owned[m.OptionsPath()] = true
	}
	if m.Server != nil {
		owned[PropertiesFile] = true
		if m.Server.Players != nil {
			owned[WhitelistFile] = true
			owned[OpsFile] = true
			owned[BansFile] = true
		}
	}
	kept := overrides[:0]
	for _, o := range overrides {
		if !owned[o.Path] {
			kept = append(kept, o)
		}
	}
	return kept
}
