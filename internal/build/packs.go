package build

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
)

const resourcePacksKey = "resourcePacks"

// collectPacks places the resource packs and shaders. Both are client-only, and
// both are placed under their requires key rather than the provider's file name,
// so a pack enabled in game stays enabled when it updates.
func (b *Builder) collectPacks(cond conditions, desired map[string]source, report *Report) error {
	for _, kind := range []string{manifest.TypeResourcePack, manifest.TypeShader} {
		listed, locked := b.Manifest.ResourcePacks(), b.Lock.ResourcePacks
		if kind == manifest.TypeShader {
			listed, locked = b.Manifest.Shaders(), b.Lock.Shaders
		}
		for _, key := range sortedPacks(locked) {
			p := locked[key]
			if entry, ok := listed[key]; ok {
				if admitted, why := cond.admits(entry); !admitted {
					report.Excluded = append(report.Excluded, key+" ("+why+")")
					continue
				}
			}
			if !b.Cache.Has(p.Sha512) {
				return out.Errorf("not-installed", "%s is not in the cache; run `shulker install`", key)
			}
			desired[packPath(kind, key, p)] = source{sha512: p.Sha512}
		}
	}
	return nil
}

// packRef is a locked pack with the kind it was locked as and where it lands, so
// builds and exports agree on both.
type packRef struct {
	key  string
	kind string
	path string
	pack lock.Pack
}

func (b *Builder) packRefs() []packRef {
	var refs []packRef
	for _, kind := range []string{manifest.TypeResourcePack, manifest.TypeShader} {
		locked := b.Lock.ResourcePacks
		if kind == manifest.TypeShader {
			locked = b.Lock.Shaders
		}
		for _, key := range sortedPacks(locked) {
			refs = append(refs, packRef{key: key, kind: kind, path: packPath(kind, key, locked[key]), pack: locked[key]})
		}
	}
	return refs
}

// packPath is where a pack lands. Shaders go to shaderpacks/, except the vanilla
// ones: those are resource packs carrying core shaders, and no shader mod loads
// them.
func packPath(kind, key string, p lock.Pack) string {
	if kind == manifest.TypeShader && p.Loader != "vanilla" {
		return "shaderpacks/" + key + ".zip"
	}
	return "resourcepacks/" + key + ".zip"
}

// shaderConfigs are the config files each shader loader enables its pack in.
// Canvas has none, and a vanilla shader is a resource pack, so neither appears.
var shaderConfigs = map[string]string{"iris": "config/iris.properties", "oculus": "config/oculus.properties"}

// enableShader points the shader mod at the pack this build placed. Only the two
// keys shulker owns are written, through the per-key merge, so the rest of the
// player's shader settings survive a rebuild.
func (b *Builder) enableShader(desired map[string]source) {
	for _, key := range sortedPacks(b.Lock.Shaders) {
		p := b.Lock.Shaders[key]
		file, ok := shaderConfigs[p.Loader]
		if !ok {
			continue
		}
		if _, placed := desired[packPath(manifest.TypeShader, key, p)]; !placed {
			continue
		}
		props := properties{"shaderPack": key + ".zip", "enableShaders": "true"}
		desired[file] = source{owned: propsFile{props: props, sep: "="}}
		return
	}
}

// seedResourcePacks fills in options.txt's enabled list once, and holds it from
// then on. Minecraft reads the list in priority order, so once it is a player's
// own shulker stops touching it: a later add or remove leaves the line alone and
// the pack is enabled in game by hand. --force seeds it again.
func (b *Builder) seedResourcePacks(name string, opts Options, desired map[string]source, options properties, report *Report) {
	defer func() { reportUnenabled(desired, options[resourcePacksKey], report) }()
	if _, own := options[resourcePacksKey]; own {
		return
	}
	dir := opts.Dir
	if dir == "" {
		dir = filepath.Join(b.Dir, b.Manifest.TargetBuildDir(name))
	}
	was, recorded := LoadState(dir).Values[OptionsFile][resourcePacksKey]
	switch {
	case opts.Force:
	case recorded:
		// Keep desiring what was written, so the merge neither drops the key nor
		// overrules a list the player has since changed in game.
		options[resourcePacksKey] = was
		return
	default:
		data, _ := os.ReadFile(filepath.Join(dir, OptionsFile))
		if current, present := parseProperties(data)[resourcePacksKey]; present && !untouchedPackList(current) {
			return
		}
	}
	if packs := placedPacks(desired); len(packs) > 0 {
		options[resourcePacksKey] = packList(packs)
	}
}

// reportUnenabled names the packs this build placed that the enabled list
// doesn't carry, because shulker seeds that list once and then leaves it to the
// player.
func reportUnenabled(desired map[string]source, enabled string, report *Report) {
	if report == nil {
		return
	}
	for _, name := range placedPacks(desired) {
		if strings.Contains(enabled, `"file/`+name+`"`) {
			continue
		}
		report.Warnings = append(report.Warnings, fmt.Sprintf("%s is placed but not enabled; turn it on in game under Options, Resource Packs", strings.TrimSuffix(name, ".zip")))
	}
}

// placedPacks are the file names this build puts in resourcepacks/, which are
// the ones options.txt can enable.
func placedPacks(desired map[string]source) []string {
	var names []string
	for path := range desired {
		if name, ok := strings.CutPrefix(path, "resourcepacks/"); ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// packList is options.txt's own syntax: a json array, vanilla first, each pack
// as file/<name>.
func packList(names []string) string {
	quoted := make([]string, 0, len(names)+1)
	quoted = append(quoted, `"vanilla"`)
	for _, name := range names {
		quoted = append(quoted, `"file/`+name+`"`)
	}
	return "[" + strings.Join(quoted, ",") + "]"
}

// untouchedPackList reports whether the list is still what Minecraft writes for
// itself, which shulker may replace.
func untouchedPackList(value string) bool {
	return value == `["vanilla"]` || value == "[]"
}

var kindLabels = map[string][2]string{
	manifest.TypeMod:          {"mod", "mods"},
	manifest.TypeResourcePack: {"resource pack", "resource packs"},
	manifest.TypeShader:       {"shader", "shaders"},
}

// kindTally collects what an export can't point at, keeping each kind's own
// count so the error names them separately.
type kindTally struct {
	items  []string
	counts map[string]int
}

func (t *kindTally) add(kind, item string) {
	if t.counts == nil {
		t.counts = map[string]int{}
	}
	t.counts[kind]++
	t.items = append(t.items, item)
}

func (t *kindTally) total() int { return len(t.items) }

// kindCount names each kind it counts, so an export never calls a resource pack
// a mod.
func kindCount(counts map[string]int) string {
	var parts []string
	for _, kind := range []string{manifest.TypeMod, manifest.TypeResourcePack, manifest.TypeShader} {
		n := counts[kind]
		if n == 0 {
			continue
		}
		word := kindLabels[kind][1]
		if n == 1 {
			word = kindLabels[kind][0]
		}
		parts = append(parts, fmt.Sprintf("%d %s", n, word))
	}
	if len(parts) < 2 {
		return strings.Join(parts, "")
	}
	return strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
}

func sortedPacks(m map[string]lock.Pack) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
