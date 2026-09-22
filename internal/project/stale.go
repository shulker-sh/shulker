package project

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"

	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/loaderver"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/mcver"
)

func (p *Project) IsLockStale() bool {
	return p.Lock == nil || len(p.LockDifferences()) > 0
}

func (p *Project) LockDifferences() []string {
	if p.Lock == nil {
		return []string{"no shulker.lock"}
	}
	m, l := p.Manifest, p.Lock
	diffs := append(PlatformDifferences(m, l), ProviderDifferences(m, l)...)
	diffs = append(diffs, PackDifferences(p.Dir, m, l)...)
	diffs = append(diffs, ZipDifferences(p.Dir, m, l)...)
	mods := m.Mods()
	for _, id := range slices.Sorted(maps.Keys(mods)) {
		lm, ok := l.Mods[id]
		if !ok {
			diffs = append(diffs, id+": in shulker.json, not in shulker.lock")
			continue
		}
		diffs = append(diffs, ModDifferences(p.Dir, id, mods[id], lm)...)
	}
	for _, id := range slices.Sorted(maps.Keys(l.Mods)) {
		if _, listed := mods[id]; !listed && len(l.Mods[id].RequiredBy) == 0 && l.Mods[id].Modpack == "" {
			diffs = append(diffs, id+": in shulker.lock, not in shulker.json")
		}
	}
	return diffs
}

func PlatformDifferences(m *manifest.Manifest, l *lock.Lock) []string {
	var diffs []string
	switch {
	case l.Minecraft == "":
		diffs = append(diffs, "minecraft: not in shulker.lock")
	case !minecraftMatches(m.Minecraft, l.Minecraft):
		diffs = append(diffs, fmt.Sprintf("minecraft: locked %s is outside %s", l.Minecraft, m.Minecraft))
	}
	switch {
	case inheritsLoader(m, l):
		// The loader came from a locked modpack, so shulker.json not naming one is
		// inheritance rather than drift; a modpack that moves is caught when the
		// resolver compares what the modpacks now pin.
	case l.Loader.Type != m.Loader.Type:
		diffs = append(diffs, fmt.Sprintf("loader: locked %s, shulker.json asks for %s", loader.Describe(l.Loader.Type, ""), loader.Describe(m.Loader.Type, "")))
	case l.Loader.Type != "" && !loaderMatches(m.Loader.Version, l.Loader.Version):
		diffs = append(diffs, fmt.Sprintf("loader: locked %s %s is outside %s", l.Loader.Type, l.Loader.Version, m.Loader.Version))
	}
	return diffs
}

func inheritsLoader(m *manifest.Manifest, l *lock.Lock) bool {
	if m.Loader.Type != "" {
		return false
	}
	for _, mp := range l.Modpacks {
		if mp.UsesLock {
			return true
		}
	}
	return false
}

func ProviderDifferences(m *manifest.Manifest, l *lock.Lock) []string {
	order := m.ProviderOrder()
	var diffs []string
	for _, id := range slices.Sorted(maps.Keys(l.Mods)) {
		if l.Mods[id].File != "" {
			continue
		}
		if name := l.Mods[id].Provider; !slices.Contains(order, name) {
			diffs = append(diffs, fmt.Sprintf("providers: %s is locked from %s, which shulker.json does not list", id, name))
		}
	}
	return diffs
}

// PackDifferences compares the modpacks shulker.json requires with what the lock records for them.
// An archive's changed bytes are a difference only when it follows them; one that doesn't stays at
// the bytes it locked until `shulker update`.
func PackDifferences(dir string, m *manifest.Manifest, l *lock.Lock) []string {
	var diffs []string
	modpacks := m.Modpacks()
	for _, name := range slices.Sorted(maps.Keys(modpacks)) {
		mp := modpacks[name]
		lp, ok := l.Modpacks[name]
		switch {
		case !ok:
			diffs = append(diffs, fmt.Sprintf("pack %s: in shulker.json, not in shulker.lock", name))
		case (mp.File == "") != (lp.File == ""):
			diffs = append(diffs, fmt.Sprintf("pack %s: %s -> %s", name, lp.Source+lp.File, mp.Source+mp.File))
		case mp.File != "":
			if mp.File != lp.File || mp.AutoUpdates() {
				diffs = append(diffs, FileDifferences(dir, name, mp.File, lp.File, lp.Size, lp.Sha512)...)
			}
			if mp.Locked != nil && *mp.Locked != lp.UsesLock {
				diffs = append(diffs, fmt.Sprintf("pack %s: locked %v -> %v", name, lp.UsesLock, *mp.Locked))
			}
		case lp.Source != mp.Source:
			diffs = append(diffs, fmt.Sprintf("pack %s: source %s -> %s", name, lp.Source, mp.Source))
		case lp.Ref != mp.Ref:
			diffs = append(diffs, fmt.Sprintf("pack %s: ref %q -> %q", name, lp.Ref, mp.Ref))
		case mp.Locked != nil && *mp.Locked != lp.UsesLock:
			diffs = append(diffs, fmt.Sprintf("pack %s: locked %v -> %v", name, lp.UsesLock, *mp.Locked))
		}
	}
	for _, name := range slices.Sorted(maps.Keys(l.Modpacks)) {
		if _, listed := modpacks[name]; !listed {
			diffs = append(diffs, fmt.Sprintf("pack %s: in shulker.lock, not in shulker.json", name))
		}
	}
	return diffs
}

// ZipDifferences compares the resource packs and shaders shulker.json lists with
// what the lock records. An entry a locked modpack supplied is not listed here,
// so it never reads as drift.
func ZipDifferences(dir string, m *manifest.Manifest, l *lock.Lock) []string {
	var diffs []string
	for _, kind := range []string{manifest.TypeResourcePack, manifest.TypeShader} {
		listed, locked := m.ResourcePacks(), l.ResourcePacks
		if kind == manifest.TypeShader {
			listed, locked = m.Shaders(), l.Shaders
		}
		for _, key := range slices.Sorted(maps.Keys(listed)) {
			lp, ok := locked[key]
			if !ok {
				diffs = append(diffs, key+": in shulker.json, not in shulker.lock")
				continue
			}
			diffs = append(diffs, ZipEntryDifferences(dir, key, listed[key], lp)...)
		}
		for _, key := range slices.Sorted(maps.Keys(locked)) {
			if _, ok := listed[key]; !ok && locked[key].Modpack == "" {
				diffs = append(diffs, key+": in shulker.lock, not in shulker.json")
			}
		}
	}
	return diffs
}

// ZipEntryDifferences compares one listed resource pack or shader with what the
// lock records for it.
func ZipEntryDifferences(dir, key string, e manifest.Require, lp lock.Pack) []string {
	if e.File != "" || lp.File != "" {
		return FileDifferences(dir, key, e.File, lp.File, lp.Size, lp.Sha512)
	}
	var diffs []string
	channel := e.Channel
	if channel == "" {
		channel = "release"
	}
	if channel != lp.Channel {
		diffs = append(diffs, fmt.Sprintf("%s: channel %s -> %s", key, lp.Channel, channel))
	}
	if e.Pin != nil && fmt.Sprint(e.Pin) != fmt.Sprint(lp.Version) {
		diffs = append(diffs, fmt.Sprintf("%s: pinned to %v, locked %v", key, e.Pin, lp.Version))
	}
	if name := e.Provider; name != "" && name != lp.Provider {
		diffs = append(diffs, fmt.Sprintf("%s: provider %s -> %s", key, lp.Provider, name))
	}
	return diffs
}

func ModDifferences(dir, id string, e manifest.Require, lm lock.Mod) []string {
	if e.File != "" || lm.File != "" {
		diffs := FileDifferences(dir, id, e.File, lm.File, lm.Size, lm.Sha512)
		if e.Side != "" && e.Side != lm.Side {
			diffs = append(diffs, fmt.Sprintf("%s: side %s -> %s", id, lm.Side, e.Side))
		}
		return diffs
	}
	var diffs []string
	channel := e.Channel
	if channel == "" {
		channel = "release"
	}
	if channel != lm.Channel {
		diffs = append(diffs, fmt.Sprintf("%s: channel %s -> %s", id, lm.Channel, channel))
	}
	if e.Pin != nil && fmt.Sprint(e.Pin) != fmt.Sprint(lm.Version) {
		diffs = append(diffs, fmt.Sprintf("%s: pinned to %v, locked %v", id, e.Pin, lm.Version))
	}
	if e.Side != "" && e.Side != lm.Side {
		diffs = append(diffs, fmt.Sprintf("%s: side %s -> %s", id, lm.Side, e.Side))
	}
	provider := e.Provider
	if provider == "" {
		provider = lm.Provider
	}
	project, ok := lockedProject(lm, provider)
	switch {
	case !ok:
		diffs = append(diffs, fmt.Sprintf("%s: provider %s -> %s", id, lm.Provider, provider))
	case e.Project != nil && fmt.Sprint(e.Project) != project:
		diffs = append(diffs, fmt.Sprintf("%s: project %s -> %v", id, project, e.Project))
	}
	return diffs
}

// FileDifferences compares a local file entry with the file on disk: its path, then its size, and
// its sha512 only when the size still matches. A file that is gone is no difference, since the cache
// still serves the bytes the lock names.
func FileDifferences(dir, key, listed, locked string, size int64, sha512 string) []string {
	switch {
	case listed == "":
		return []string{fmt.Sprintf("%s: locked as a local file, shulker.json names a provider", key)}
	case locked == "":
		return []string{fmt.Sprintf("%s: a local file in shulker.json, locked from a provider", key)}
	case listed != locked:
		return []string{fmt.Sprintf("%s: file %s -> %s", key, locked, listed)}
	}
	path := filepath.Join(dir, filepath.FromSlash(listed))
	st, err := os.Stat(path)
	if err != nil {
		return nil
	}
	if st.Size() == size {
		if got, err := fsutil.SHA512(path); err != nil || got == sha512 {
			return nil
		}
	}
	return []string{key + ": the file's bytes changed"}
}

// GoneFiles warns about each local file entry whose file is gone but whose locked bytes cached
// has, which the build places instead. One the cache lacks too is install's missing-files. An entry
// a modpack supplies names a file in the modpack's directory, not this one, so it is left out.
func (p *Project) GoneFiles(cached func(sha512 string) bool) []string {
	if p.Lock == nil {
		return nil
	}
	var gone []string
	check := func(key, rel, sha512 string) {
		if rel == "" || p.Manifest.Requires[key].File != rel || !cached(sha512) {
			return
		}
		if _, err := os.Stat(filepath.Join(p.Dir, filepath.FromSlash(rel))); errors.Is(err, os.ErrNotExist) {
			gone = append(gone, FileGone(key, rel))
		}
	}
	for _, id := range slices.Sorted(maps.Keys(p.Lock.Mods)) {
		check(id, p.Lock.Mods[id].File, p.Lock.Mods[id].Sha512)
	}
	for _, section := range []map[string]lock.Pack{p.Lock.ResourcePacks, p.Lock.Shaders} {
		for _, key := range slices.Sorted(maps.Keys(section)) {
			check(key, section[key].File, section[key].Sha512)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(p.Lock.Modpacks)) {
		check(name, p.Lock.Modpacks[name].File, p.Lock.Modpacks[name].Sha512)
	}
	return gone
}

// FileGone is the warning for a local file entry whose file has been deleted.
func FileGone(key, rel string) string {
	return fmt.Sprintf("%s: %s is gone; using the copy in the cache", key, rel)
}

func lockedProject(lm lock.Mod, provider string) (string, bool) {
	switch {
	case provider == lm.Provider:
		return fmt.Sprint(lm.Project), true
	case provider == "modrinth" && lm.Aliases.Modrinth != "":
		return lm.Aliases.Modrinth, true
	case provider == "curseforge" && lm.Aliases.CurseForge != 0:
		return strconv.Itoa(lm.Aliases.CurseForge), true
	}
	return "", false
}

func minecraftMatches(raw, id string) bool {
	rng, err := mcver.ParseRange(raw)
	if err != nil {
		return false
	}
	v, err := mcver.Parse(id)
	return err == nil && rng.Matches(v)
}

func loaderMatches(raw, id string) bool {
	rng, err := loaderver.ParseRange(raw)
	if err != nil {
		return false
	}
	v, err := loaderver.Parse(id)
	return err == nil && rng.Matches(v)
}
