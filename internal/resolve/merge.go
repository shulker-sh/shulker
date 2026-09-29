package resolve

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/modpack"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/packarchive"
	"shulker.sh/shulker/internal/project"
)

// Incoming is a modpack read as a project of its own, for a merge: its manifest and lock, the
// override files it lays, and the folder its local files lie in. HasBlocks is set when the
// manifest is the pack's own, a marker's or a source's, whose blocks merge too.
type Incoming struct {
	Manifest  *manifest.Manifest
	Lock      *lock.Lock
	Overrides []packarchive.Override
	Dir       string
	HasBlocks bool
	// Earlier is what an earlier import of the same pack wrote, which the merge replaces where
	// the project still holds it; nil keeps everything the project has.
	Earlier *Earlier
}

// Merged is what a merge brought in and what it left alone.
type Merged struct {
	Entries   []string
	KeptYours []string
	LeftOut   []string
	Copied    int
	Kept      int
	// Created are the files and folders the merge wrote, which a failed relock takes away again.
	Created []string
	// replaced are the files the merge wrote over, with what they held, for Undo to put back.
	replaced []replacedFile
}

type replacedFile struct {
	path string
	data []byte
}

// Undo removes what the merge wrote, newest first, and puts back the files it wrote over.
func (m *Merged) Undo() {
	for _, path := range slices.Backward(m.Created) {
		os.RemoveAll(path)
	}
	for _, f := range slices.Backward(m.replaced) {
		fsutil.Write(f.path, f.data)
	}
}

// replace writes data over the file at path, keeping what it held for Undo.
func (m *Merged) replace(path string, data []byte) error {
	was, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	m.replaced = append(m.replaced, replacedFile{path, was})
	return fsutil.Write(path, data)
}

// IncomingFromSource reads a shulker source for a merge: its manifest, its lock, and the files in
// its override folders.
func IncomingFromSource(c *modpack.Checkout) (*Incoming, error) {
	src, err := project.OpenReplacingLock(c.Dir)
	if err != nil {
		return nil, err
	}
	if err := src.RequireLock(); err != nil {
		return nil, err
	}
	overrides, err := project.ReadOverrideFolders(c.Dir, src.Manifest)
	if err != nil {
		return nil, err
	}
	return &Incoming{Manifest: src.Manifest, Lock: src.Lock, Overrides: overrides, Dir: c.Dir, HasBlocks: true}, nil
}

// Inlined is a required modpack as a pack to merge: the project's lock entries it provides, taken
// out of the project's lock and listed under the pack's own requires entry where it has one, its
// manifest's blocks, and its override files.
func Inlined(p *project.Project, loaded *modpack.Loaded) (*Incoming, error) {
	key := loaded.Name
	pm := *loaded.Manifest
	pm.Requires = map[string]manifest.Require{}
	pl := lock.New()
	provides := func(modpack string, requiredBy []string, id string) bool {
		_, own := p.Manifest.Requires[id]
		return modpack == key || (!own && slices.Contains(requiredBy, key))
	}
	entry := func(id, kind string, projectID, providerName string) manifest.Require {
		req, ok := loaded.Manifest.Requires[id]
		if !ok {
			req = manifest.Require{}
			if kind != manifest.TypeMod {
				req.Type = kind
			}
		}
		req.Pin = ""
		if projectID != "" {
			req.Project = projectID
			req.Provider = ""
			if providerName != "" && providerName != p.Manifest.ProviderOrder()[0] {
				req.Provider = providerName
			}
		}
		return req
	}
	for id, m := range p.Lock.Mods {
		if !provides(m.Modpack, m.RequiredBy, id) {
			for i, by := range m.RequiredBy {
				if by == key {
					m.RequiredBy = slices.Delete(slices.Clone(m.RequiredBy), i, i+1)
					p.Lock.Mods[id] = m
					break
				}
			}
			continue
		}
		delete(p.Lock.Mods, id)
		m.Modpack = ""
		m.RequiredBy = slices.DeleteFunc(slices.Clone(m.RequiredBy), func(by string) bool { return by == key })
		pl.Mods[id] = m
		pm.Requires[id] = entry(id, manifest.TypeMod, m.Project, m.Provider)
	}
	for _, kind := range manifest.PackKinds {
		section := p.Lock.Packs(kind)
		for id, lp := range section {
			if lp.Modpack != key {
				continue
			}
			delete(section, id)
			lp.Modpack = ""
			pl.Packs(kind)[id] = lp
			pm.Requires[id] = entry(id, kind, lp.Project, lp.Provider)
		}
	}
	inc := &Incoming{Manifest: &pm, Lock: pl, Dir: loaded.Dir, HasBlocks: true, Overrides: loaded.Overrides}
	if loaded.Dir != "" && loaded.Archive == nil {
		overrides, err := project.ReadOverrideFolders(loaded.Dir, loaded.Manifest)
		if err != nil {
			return nil, err
		}
		inc.Overrides = overrides
	}
	return inc, nil
}

// Merge merges inc into p within sides: the pack's entries, lock entries, local files and
// overrides, and a marker's or source's blocks, the project keeping its own on every clash.
func Merge(p *project.Project, inc *Incoming, sides []string) (*Merged, error) {
	rep := &Merged{LeftOut: []string{}, KeptYours: []string{}}
	pm, pl := inc.Manifest, inc.Lock
	overrides := inc.Overrides
	if len(sides) == 1 {
		overrides, rep.LeftOut = KeepSide(pm, pl, overrides, sides[0])
	}
	takePlatform(p, pm, pl)
	for _, pattern := range pm.SeedFiles {
		if !slices.Contains(p.Manifest.SeedFiles, pattern) {
			p.Manifest.SeedFiles = append(p.Manifest.SeedFiles, pattern)
		}
	}
	projectJars := map[string]string{}
	for key := range p.Lock.Mods {
		projectJars[p.Lock.JarID(key)] = key
	}
	kept := map[string]bool{}
	requiredBy := map[string][]string{}
	replacedFiles := map[string]bool{}
	for _, key := range slices.Sorted(maps.Keys(pm.Requires)) {
		_, isMod := pl.Mods[key]
		mine := ""
		if _, listed := p.Manifest.Requires[key]; listed {
			mine = key
		} else if isMod {
			mine = projectJars[pl.JarID(key)]
		}
		if mine != "" && inc.Earlier.wroteEntry(p.Lock, mine) {
			if m, ok := p.Lock.Mods[mine]; ok {
				requiredBy[key] = m.RequiredBy
				delete(projectJars, p.Lock.JarID(mine))
			}
			if file := dropEntry(p, mine); file != "" {
				replacedFiles[file] = true
			}
			if mine != key {
				renameRequiredBy(p.Lock, mine, key)
			}
			mine = ""
		}
		if mine != "" {
			rep.KeptYours = append(rep.KeptYours, key)
			kept[key] = true
			continue
		}
		req := pm.Requires[key]
		req.Pin = ""
		p.Manifest.Requires[key] = req
		rep.Entries = append(rep.Entries, key)
	}
	var files []string
	for _, key := range slices.Sorted(maps.Keys(pl.Mods)) {
		mod := pl.Mods[key]
		if _, held := p.Lock.Mods[key]; held || kept[key] || kept[mod.Modpack] || projectJars[pl.JarID(key)] != "" {
			continue
		}
		for _, by := range requiredBy[key] {
			if !slices.Contains(mod.RequiredBy, by) {
				mod.RequiredBy = append(slices.Clone(mod.RequiredBy), by)
			}
		}
		p.Lock.Mods[key] = mod
		files = append(files, mod.File)
	}
	for _, kind := range manifest.PackKinds {
		mine := p.Lock.Packs(kind)
		for key, lp := range pl.Packs(kind) {
			if _, held := mine[key]; held || kept[key] || kept[lp.Modpack] {
				continue
			}
			mine[key] = lp
			files = append(files, lp.File)
		}
	}
	for key, mp := range pl.Modpacks {
		if _, held := p.Lock.Modpacks[key]; held || kept[key] {
			continue
		}
		p.Lock.Modpacks[key] = mp
	}
	for _, rel := range files {
		if rel == "" {
			continue
		}
		from, to := filepath.Join(inc.Dir, filepath.FromSlash(rel)), filepath.Join(p.Dir, filepath.FromSlash(rel))
		_, err := os.Lstat(to)
		if err == nil && !replacedFiles[rel] {
			continue
		}
		if _, err := os.Stat(from); inc.Dir == "" || err != nil {
			return rep, modpack.FileMissing(inc.Manifest.Name, rel)
		}
		if err == nil {
			data, err := os.ReadFile(from)
			if err != nil {
				return rep, err
			}
			if err := rep.replace(to, data); err != nil {
				return rep, err
			}
			continue
		}
		rep.Created = append(rep.Created, to)
		if err := fsutil.CopyPath(from, to); err != nil {
			return rep, err
		}
	}
	for _, o := range overrides {
		if !filepath.IsLocal(filepath.FromSlash(o.Path)) {
			return rep, out.Errorf("path-outside", "%s/%s is outside its folder", o.Layer, o.Path)
		}
		to := filepath.Join(p.Dir, filepath.FromSlash(o.Layer), filepath.FromSlash(o.Path))
		if _, err := os.Lstat(to); err == nil {
			if data, err := os.ReadFile(to); err == nil && inc.Earlier.wroteOverride(o.Layer+"/"+o.Path, data) {
				if err := rep.replace(to, o.Data); err != nil {
					return rep, err
				}
				rep.Copied++
				continue
			}
			rep.KeptYours = append(rep.KeptYours, o.Layer+"/"+o.Path)
			rep.Kept++
			continue
		}
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return rep, err
		}
		rep.Created = append(rep.Created, to)
		if err := fsutil.Write(to, o.Data); err != nil {
			return rep, err
		}
		rep.Copied++
	}
	if inc.HasBlocks {
		if err := mergeBlocks(p.Manifest, pm, sides); err != nil {
			return rep, err
		}
		if p.Manifest.Server != nil && p.Manifest.Server.Players != nil && len(p.Lock.Players) == 0 {
			p.Lock.Players = pl.Players
		}
	}
	slices.Sort(rep.KeptYours)
	slices.Sort(rep.LeftOut)
	return rep, nil
}

// takePlatform gives a project that sets and inherits no Minecraft version or loader the pack's.
func takePlatform(p *project.Project, pm *manifest.Manifest, pl *lock.Lock) {
	if p.Manifest.Minecraft == "" && p.Lock.Minecraft == "" {
		p.Manifest.Minecraft = pm.Minecraft
		p.Lock.Minecraft, p.Lock.DataVersion, p.Lock.Java = pl.Minecraft, pl.DataVersion, pl.Java
	}
	if p.Manifest.Loader.Type == "" && p.Lock.Loader.Type == "" && pl.Loader.Type != "" {
		p.Manifest.Loader = pm.Loader
		p.Lock.Loader = pl.Loader
	}
}

// mergeBlocks merges the pack manifest's blocks into the project's, key by key, the project's value
// winning: features, variables, providers, java and note, and the side blocks the project declares
// within sides, less the folder each side builds into.
func mergeBlocks(m, pm *manifest.Manifest, sides []string) error {
	mine, err := asObject(m)
	if err != nil {
		return err
	}
	theirs, err := asObject(pm)
	if err != nil {
		return err
	}
	keys := []string{"features", "variables", "providers", "java", "note"}
	for _, side := range sides {
		if m.HasSide(side) {
			keys = append(keys, side)
			if block, ok := theirs[side].(map[string]any); ok {
				delete(block, "build")
			}
		}
	}
	for _, key := range keys {
		if v, ok := theirs[key]; ok {
			mine[key] = mergeValue(mine[key], v)
		}
	}
	data, err := json.Marshal(mine)
	if err != nil {
		return err
	}
	var merged manifest.Manifest
	if err := json.Unmarshal(data, &merged); err != nil {
		return err
	}
	*m = merged
	return nil
}

func asObject(m *manifest.Manifest) (map[string]any, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	var obj map[string]any
	return obj, json.Unmarshal(data, &obj)
}

// mergeValue merges theirs into mine: objects key by key, and anything else stays mine when mine
// is set.
func mergeValue(mine, theirs any) any {
	if mine == nil {
		return theirs
	}
	mineObj, ok := mine.(map[string]any)
	theirObj, ok2 := theirs.(map[string]any)
	if !ok || !ok2 {
		return mine
	}
	for k, v := range theirObj {
		mineObj[k] = mergeValue(mineObj[k], v)
	}
	return mineObj
}
