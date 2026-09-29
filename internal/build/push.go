package build

import (
	"fmt"
	"maps"
	"slices"

	"shulker.sh/shulker/internal/out"
)

// maxPushSize is the largest resource pack the wiki says a server may push.
const maxPushSize = 250 << 20

// pushResourcePack fills resource-pack and resource-pack-sha1 from the locked pack key names. A
// feature conditions the push; an OS doesn't, since the pack is bound for clients on any OS.
func (b *Builder) pushResourcePack(key string, cond conditions, props properties, report *Report) error {
	p, ok := b.Lock.ResourcePacks[key]
	if !ok {
		e := out.Errorf("resourcepack-not-found", "server.resourcePack %s is not a locked resource pack", key)
		e.Candidates = slices.Sorted(maps.Keys(b.Lock.ResourcePacks))
		return e
	}
	if entry, listed := b.Manifest.ResourcePacks()[key]; listed {
		cond.admitsAnyOS = true
		if admitted, why := cond.admits(entry); !admitted {
			report.Excluded = append(report.Excluded, fmt.Sprintf("%s (not pushed: %s)", key, why))
			return nil
		}
	}
	for _, k := range []string{"resource-pack", "resource-pack-sha1"} {
		if _, set := props[k]; set {
			e := out.Errorf("resourcepack-conflict", "server.properties sets %s, which server.resourcePack %s fills", k, key)
			e.Help = "remove " + k + " from server.properties, or drop server.resourcePack"
			return e
		}
	}
	if p.File != "" {
		e := out.Errorf("resourcepack-local-file", "%s is a local file, so there's no URL for clients to download it from", key)
		e.Help = "push a pack added from Modrinth or CurseForge, or set resource-pack by hand to a URL you host"
		return e
	}
	if p.URL == nil {
		return out.Errorf("resourcepack-not-distributed", "%s is not distributed by %s, so there's no URL for clients to download it from", key, b.Providers.Title(p.Provider))
	}
	if p.Size > maxPushSize {
		report.Warnings = append(report.Warnings, fmt.Sprintf("resource pack %s is %.1f MiB, over the 250 MiB a server may push; it is pushed anyway.", key, float64(p.Size)/(1<<20)))
	}
	props["resource-pack"] = *p.URL
	props["resource-pack-sha1"] = p.Sha1
	return nil
}
