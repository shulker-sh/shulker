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
	"shulker.sh/shulker/internal/pack"
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
}

// Undo removes what the merge wrote, newest first.
func (m *Merged) Undo() {
	for _, path := range slices.Backward(m.Created) {
		os.RemoveAll(path)
	}
}

// IncomingFromSource reads a shulker source for a merge: its manifest, its lock, and the files in
// its override folders.
func IncomingFromSource(c *pack.Checkout) (*Incoming, error) {
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
	projectJars := map[string]bool{}
	for key := range p.Lock.Mods {
		projectJars[p.Lock.JarID(key)] = true
	}
	kept := map[string]bool{}
	for _, key := range slices.Sorted(maps.Keys(pm.Requires)) {
		_, isMod := pl.Mods[key]
		if _, listed := p.Manifest.Requires[key]; listed || (isMod && projectJars[pl.JarID(key)]) {
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
		if _, held := p.Lock.Mods[key]; held || kept[key] || kept[mod.Modpack] || projectJars[pl.JarID(key)] {
			continue
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
		if _, err := os.Lstat(to); err == nil {
			continue
		}
		if _, err := os.Stat(from); inc.Dir == "" || err != nil {
			return rep, pack.FileMissing(inc.Manifest.Name, rel)
		}
		rep.Created = append(rep.Created, to)
		if err := fsutil.CopyPath(from, to); err != nil {
			return rep, err
		}
	}
	for _, o := range overrides {
		to := filepath.Join(p.Dir, filepath.FromSlash(o.Layer), filepath.FromSlash(o.Path))
		if _, err := os.Lstat(to); err == nil {
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
