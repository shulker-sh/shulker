package build

import (
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"

	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/modpack"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/provider"
	"shulker.sh/shulker/internal/security"
)

type mismatch struct {
	key, provider, host, modpack string
}

// CheckProvenance refuses a lock with an entry that names a provider but downloads from a host
// that isn't one of the provider's own, since the lock's author picked that file and its hash.
// source names the remote source the lock came from, whose author alone can fix it; empty for the
// player's own project, whose packs say where each entry they bring can be locked again.
func CheckProvenance(ps provider.Providers, m *manifest.Manifest, l *lock.Lock, packs []*modpack.Loaded, source string) error {
	var found []mismatch
	check := func(key, name, modpack string, u *string) {
		if name == "" || u == nil {
			return
		}
		host := *u
		if parsed, err := url.Parse(*u); err == nil && parsed.Hostname() != "" {
			host = parsed.Hostname()
			if p, ok := ps[name]; ok && provider.Serves(p, host) {
				return
			}
		}
		found = append(found, mismatch{key: key, provider: name, host: host, modpack: modpack})
	}
	for _, key := range slices.Sorted(maps.Keys(l.Mods)) {
		if e := l.Mods[key]; e.File == "" {
			check(key, e.Provider, e.Modpack, e.URL)
		}
	}
	for _, kind := range manifest.PackKinds {
		section := l.Packs(kind)
		for _, key := range slices.Sorted(maps.Keys(section)) {
			if e := section[key]; e.File == "" {
				check(key, e.Provider, e.Modpack, e.URL)
			}
		}
	}
	for _, key := range slices.Sorted(maps.Keys(l.Modpacks)) {
		if e := l.Modpacks[key]; e.File == "" && e.Source == "" {
			check(key, e.Provider, "", e.URL)
		}
	}
	if len(found) == 0 {
		return nil
	}
	slices.SortFunc(found, func(a, b mismatch) int { return strings.Compare(a.key, b.key) })
	var e *out.Error
	if len(found) == 1 {
		f := found[0]
		e = out.Errorf("provenance-mismatch", "%s is locked from %s but downloads from %s", f.key, ps.Title(f.provider), f.host)
	} else {
		e = out.Errorf("provenance-mismatch", "%d lock entries download from outside their provider", len(found))
		for _, f := range found {
			e.Rows = append(e.Rows, out.Detail{Text: fmt.Sprintf("%s: locked from %s, downloads from %s", f.key, ps.Title(f.provider), f.host)})
		}
	}
	for _, f := range found {
		e.Items = append(e.Items, f.key)
	}
	e.Help = provenanceHelp(found, m, packs, source)
	return security.Refusal(security.Provenance, e)
}

// provenanceHelp names who can fix each mismatch: `lock` looks an entry up again, in the project
// or in the local folder a modpack brings it from, or locks again the hosted modpack that brings
// it, while a git, URL or file modpack's own lock is its author's to fix.
func provenanceHelp(found []mismatch, m *manifest.Manifest, packs []*modpack.Loaded, source string) string {
	if source != "" {
		return fmt.Sprintf("this lock is %s's, so its author has to fix it", source)
	}
	local := map[string]string{}
	for _, p := range packs {
		if p.Kind == modpack.Local {
			local[p.Name] = p.Dir
		}
	}
	byDir := map[string][]string{}
	var authors []string
	for _, f := range found {
		dir, isLocal := local[f.modpack]
		switch {
		case f.modpack == "":
			byDir[""] = append(byDir[""], f.key)
		case m.Requires[f.modpack].IsHosted():
			byDir[""] = append(byDir[""], f.modpack)
		case isLocal:
			byDir[dir] = append(byDir[dir], f.key)
		default:
			authors = append(authors, f.modpack)
		}
	}
	var parts []string
	for _, dir := range slices.Sorted(maps.Keys(byDir)) {
		keys := slices.Compact(slices.Sorted(slices.Values(byDir[dir])))
		flag, them, their := "", "them", "their"
		if dir != "" {
			flag = " -C " + dir
		}
		if len(keys) == 1 {
			them, their = "it", "its"
		}
		parts = append(parts, fmt.Sprintf("run `shulker lock %s%s` to look %s up again from %s provider", strings.Join(keys, " "), flag, them, their))
	}
	if len(authors) > 0 {
		which := "them"
		if len(parts) > 0 {
			which = "the rest"
		}
		parts = append(parts, fmt.Sprintf("modpack %s's own lock names %s, so its author has to fix it", strings.Join(slices.Compact(slices.Sorted(slices.Values(authors))), ", "), which))
	}
	return strings.Join(parts, "; ")
}
