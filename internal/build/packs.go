package build

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
)

const resourcePacksKey = "resourcePacks"

// collectPacks places the resource packs and shaders. Both are client-only, and
// both are placed under their requires key rather than the provider's file name,
// so a pack enabled in game stays enabled when it updates.
func (b *Builder) collectPacks(cond conditions, desired map[string]source, report *Report) error {
	for _, ref := range b.packRefs() {
		if entry, ok := ref.listed(b.Manifest); ok {
			if admitted, why := cond.admits(entry); !admitted {
				report.Excluded = append(report.Excluded, ref.key+" ("+why+")")
				continue
			}
		}
		if !b.Cache.Has(ref.pack.Sha512) {
			return notInstalled(ref.key)
		}
		desired[ref.path] = source{sha512: ref.pack.Sha512}
	}
	return nil
}

// packRef is a locked pack with the kind it was locked as and where it lands, so
// builds and exports agree on both. A hybrid datapack's resource pack copy is a
// resource pack ref of its own.
type packRef struct {
	key    string
	kind   string
	path   string
	pack   lock.Pack
	hybrid bool
}

// listed is the manifest entry the pack was locked from, if any.
func (ref packRef) listed(m *manifest.Manifest) (manifest.Require, bool) {
	listed := m.ResourcePacks()
	switch {
	case ref.hybrid:
		listed = m.Datapacks()
	case ref.kind == manifest.TypeShader:
		listed = m.Shaders()
	}
	entry, ok := listed[ref.key]
	return entry, ok
}

func (b *Builder) packRefs() []packRef {
	var refs []packRef
	for _, kind := range []string{manifest.TypeResourcePack, manifest.TypeShader} {
		locked := b.Lock.ResourcePacks
		if kind == manifest.TypeShader {
			locked = b.Lock.Shaders
		}
		for _, key := range sortedPacks(locked) {
			refs = append(refs, packRef{key: key, kind: kind, path: locked[key].Path(kind), pack: locked[key]})
		}
	}
	for _, key := range sortedPacks(b.Lock.Datapacks) {
		if p := b.Lock.Datapacks[key]; p.ResourcePack {
			refs = append(refs, packRef{key: key, kind: manifest.TypeResourcePack, path: p.Path(manifest.TypeResourcePack), pack: p, hybrid: true})
		}
	}
	return refs
}

// shaderConfigs are the config files each shader mod enables its pack in, in the
// order they are tried. Canvas has none, and a vanilla shader is a resource pack,
// so neither appears.
var shaderConfigs = []struct{ mod, file string }{{"iris", "config/iris.properties"}, {"oculus", "config/oculus.properties"}}

// enableShader points a shader mod this build placed at the first placed pack it
// can load, and returns that pack's key, empty when none. placed holds the placed
// mods' jar ids, so a renamed mod or one from another provider is still found.
// Only the two keys shulker owns are written, through the per-key merge, so the
// rest of the player's shader settings survive a rebuild.
func (b *Builder) enableShader(desired map[string]source, placed map[string]bool) string {
	for _, key := range b.placedShaders(desired) {
		p := b.Lock.Shaders[key]
		if config := shaderConfig(p, placed); config != "" {
			props := properties{"shaderPack": p.Filename, "enableShaders": "true"}
			desired[config] = source{owned: propsFile{props: props, sep: "="}}
			return key
		}
	}
	return ""
}

// reportUnenabledShaders warns about every placed shader but the enabled one,
// with what it takes to turn it on.
func (b *Builder) reportUnenabledShaders(desired map[string]source, placed map[string]bool, enabled string, report *Report) {
	for _, key := range b.placedShaders(desired) {
		p := b.Lock.Shaders[key]
		var hint string
		switch {
		case key == enabled:
			continue
		case shaderConfig(p, placed) != "":
			hint = " is placed but not enabled; turn it on in game under Options, Video Settings, Shader Packs"
		case loadsShader(p, "canvas", placed):
			hint = " is placed but not enabled; turn it on in Canvas's own menu"
		default:
			hint = " is placed, but nothing in this build can load it; shulker add iris"
		}
		report.Warnings = append(report.Warnings, key+hint)
	}
}

// placedShaders are the keys of the locked shaders this build places, in the
// order enabling tries them. A vanilla shader is a resource pack, so it isn't one.
func (b *Builder) placedShaders(desired map[string]source) []string {
	var keys []string
	for _, key := range sortedPacks(b.Lock.Shaders) {
		p := b.Lock.Shaders[key]
		if _, ok := desired[p.Path(manifest.TypeShader)]; ok && !p.IsVanillaShader() {
			keys = append(keys, key)
		}
	}
	return keys
}

// shaderConfig is the config file of the first placed mod that can enable p, empty
// when none can.
func shaderConfig(p lock.Pack, placed map[string]bool) string {
	for _, c := range shaderConfigs {
		if loadsShader(p, c.mod, placed) {
			return c.file
		}
	}
	return ""
}

// loadsShader reports whether mod is placed and can load p: p names it among its
// loaders, or names none.
func loadsShader(p lock.Pack, mod string, placed map[string]bool) bool {
	return placed[mod] && (len(p.Loaders) == 0 || slices.Contains(p.Loaders, mod))
}

// seedResourcePacks fills in options.txt's enabled list once, and holds it from
// then on. Minecraft reads the list in priority order, so once it is a player's
// own shulker stops touching it: a later add or remove leaves the line alone and
// the pack is enabled in game by hand. --force seeds it again. A list the
// override folders ship is the pack's own, so it is never seeded over.
func (b *Builder) seedResourcePacks(side string, opts Options, desired map[string]source, options properties, shipped string) {
	if _, own := options[resourcePacksKey]; own {
		return
	}
	if shipped != "" && !untouchedPackList(shipped) {
		return
	}
	dir := b.Target(side, opts.Dir)
	file := filepath.Join(dir, filepath.FromSlash(b.Manifest.OptionsPath()))
	state := LoadState(dir)
	was, recorded := state.Values[b.Manifest.OptionsPath()][resourcePacksKey]
	switch {
	case opts.Force:
	case recorded:
		// Keep desiring what was written, so the merge neither drops the key nor
		// overrules a list the player has since changed in game.
		options[resourcePacksKey] = was
		renamed := map[string]string{}
		for key, name := range b.placedPackNames(desired) {
			if old := state.Packs[key]; old != "" && old != name {
				renamed[old] = name
			}
		}
		if len(renamed) == 0 {
			return
		}
		data, _ := os.ReadFile(file)
		live, present := parseProperties(data)[resourcePacksKey]
		if !present {
			live = was
		}
		if swapped := renamePacks(live, renamed); swapped != live {
			options[resourcePacksKey] = swapped
		}
		return
	default:
		data, _ := os.ReadFile(file)
		if current, present := parseProperties(data)[resourcePacksKey]; present && !untouchedPackList(current) {
			return
		}
	}
	if packs := placedPacks(desired); len(packs) > 0 {
		options[resourcePacksKey] = packList(packs)
	}
}

// shippedPackList is the enabled list the override folders put in options.txt,
// the last folder's winning as it does when the build lays them, or empty when
// none sets one.
func (b *Builder) shippedPackList(side string, cond conditions, vars map[string]string) (string, error) {
	rel := b.Manifest.OptionsPath()
	var list string
	for _, l := range b.overrideLayers(side, cond, vars) {
		for _, name := range []string{rel, rel + TemplateSuffix} {
			if l.skips(name) {
				continue
			}
			data, ok := overrideData(l, name)
			if !ok {
				continue
			}
			if name != rel {
				var err error
				if data, err = render(l.label+"/"+name, l.pack, data, l.vars); err != nil {
					return "", err
				}
			}
			if value, set := parseProperties(data)[resourcePacksKey]; set {
				list = value
			}
		}
	}
	return list, nil
}

func overrideData(l overrideLayer, rel string) ([]byte, bool) {
	if l.archived {
		for _, o := range l.files {
			if o.Path == rel {
				return o.Data, true
			}
		}
		return nil, false
	}
	data, err := os.ReadFile(filepath.Join(l.root, filepath.FromSlash(rel)))
	return data, err == nil
}

// enabledPackList is the enabled list the game reads once this build is
// written: the one the build writes, unless the build keeps the file already
// there, or the merge keeps a list the player changed in game.
func (b *Builder) enabledPackList(side string, opts Options, desired map[string]source) (string, error) {
	rel := b.Manifest.OptionsPath()
	dir := b.Target(side, opts.Dir)
	abs := filepath.Join(dir, filepath.FromSlash(rel))
	data, _ := os.ReadFile(abs)
	live, present := parseProperties(data)[resourcePacksKey]
	src, written := desired[rel]
	if !written {
		return live, nil
	}
	state := LoadState(dir)
	if src.owned == nil {
		// A whole file the player changed, or one no build recorded, is kept
		// rather than written; one that also changed in the source fails the build.
		current, exists, err := fileSha256(abs)
		if err != nil {
			return "", err
		}
		untouched := false
		if exists && state.Files[rel] != "" {
			if untouched, err = state.isUntouched(rel, abs, current); err != nil {
				return "", err
			}
		}
		if exists && !opts.Force && !untouched {
			return live, nil
		}
		return parseProperties(src.data)[resourcePacksKey], nil
	}
	want, set := src.owned.values()[resourcePacksKey]
	if !set {
		return live, nil
	}
	last, known := state.Values[rel][resourcePacksKey]
	if present && known && !opts.Force && live != last && want == last {
		return live, nil
	}
	return want, nil
}

// reportPackList names the packs this build placed that the enabled list
// doesn't carry, because shulker seeds that list once and then leaves it to the
// player. A list the override folders shipped is the project's own, so a pack
// it enables that nothing places is named too.
func (b *Builder) reportPackList(side string, opts Options, desired map[string]source, shipped string, report *Report) error {
	if report == nil {
		return nil
	}
	list, err := b.enabledPackList(side, opts, desired)
	if err != nil {
		return err
	}
	var entries []string
	_ = json.Unmarshal([]byte(list), &entries)
	placed := placedPacks(desired)
	for _, name := range placed {
		if slices.Contains(entries, "file/"+name) {
			continue
		}
		report.Warnings = append(report.Warnings, fmt.Sprintf("%s is placed but not enabled; turn it on in game under Options, Resource Packs", strings.TrimSuffix(name, ".zip")))
	}
	if shipped == "" || list != shipped {
		return nil
	}
	for _, entry := range entries {
		if name, ok := strings.CutPrefix(entry, "file/"); ok && !slices.Contains(placed, name) {
			report.Warnings = append(report.Warnings, fmt.Sprintf("%s enables %s, but no pack is placed under that name", b.Manifest.OptionsPath(), name))
		}
	}
	return nil
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

// placedPackNames maps each resource pack this build places to its file name.
func (b *Builder) placedPackNames(desired map[string]source) map[string]string {
	names := map[string]string{}
	for _, ref := range b.packRefs() {
		if _, placed := desired[ref.path]; !placed {
			continue
		}
		if name, ok := strings.CutPrefix(ref.path, "resourcepacks/"); ok {
			names[ref.key] = name
		}
	}
	return names
}

// renamePacks swaps each old file name in an enabled list for its new one, in
// place so its priority holds. It is one pass, so a new name that is another
// pack's old one isn't renamed twice.
func renamePacks(list string, names map[string]string) string {
	var pairs []string
	for from, to := range names {
		pairs = append(pairs, `"file/`+from+`"`, `"file/`+to+`"`)
	}
	return strings.NewReplacer(pairs...).Replace(list)
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
	for _, kind := range append([]string{manifest.TypeMod}, manifest.PackKinds...) {
		n := counts[kind]
		if n == 0 {
			continue
		}
		one, word := manifest.TypeNouns(kind)
		if n == 1 {
			word = one
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

// collectDatapacks places the datapacks locked for side in the folder DatapackFolder picks, and
// warns when that is a client's datapacks/, which only some global datapack mods read.
func (b *Builder) collectDatapacks(side, levelName string, cond conditions, desired map[string]source, report *Report) error {
	folder, loaded := b.Lock.DatapackFolder(side, levelName)
	listed := b.Manifest.Datapacks()
	var placed []string
	for _, key := range sortedPacks(b.Lock.Datapacks) {
		p := b.Lock.Datapacks[key]
		if p.Side != "both" && p.Side != side {
			continue
		}
		if entry, ok := listed[key]; ok {
			if admitted, why := cond.admits(entry); !admitted {
				report.Excluded = append(report.Excluded, key+" ("+why+")")
				continue
			}
		}
		if !b.Cache.Has(p.Sha512) {
			return notInstalled(key)
		}
		desired[folder+"/"+p.Filename] = source{sha512: p.Sha512}
		placed = append(placed, key)
	}
	if !loaded && len(placed) > 0 {
		report.Warnings = append(report.Warnings, fmt.Sprintf("%s: placed in %s/, which only some global datapack mods read; add one, such as paxi, to load it in every world", strings.Join(placed, ", "), folder))
	}
	return nil
}
