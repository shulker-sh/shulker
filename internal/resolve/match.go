package resolve

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"sort"

	"shulker.sh/shulker/internal/jarmeta"
	"shulker.sh/shulker/internal/mrpack"
	"shulker.sh/shulker/internal/provider"
)

// Matched is what MatchOverrides made of a project's override files: the requires keys it locked,
// the files those were, and the files it left as overrides.
type Matched struct {
	Locked   []string `json:"locked"`
	Moved    []string `json:"moved"`
	Kept     []string `json:"kept"`
	Warnings []string `json:"-"`
}

// MatchOverrides looks the project's override jars and pack zips up on Modrinth by sha1, then what
// Modrinth lacks on CurseForge by fingerprint, and locks each match into requires, its side taken
// from the file's layer. Each file's Layer and Path name it under r.Dir. A file that doesn't match,
// can't be locked, or whose key requires already holds stays an override.
func (r *Resolver) MatchOverrides(ctx context.Context, files []mrpack.Override) (*Matched, error) {
	im := newImporter(r, &mrpack.Archive{}, false)
	im.inProject = true
	if p, ok := r.Providers["modrinth"].(hashLookup); ok {
		im.modrinth = p
	}
	sha1s := make([]string, len(files))
	for i, o := range files {
		sum := sha1.Sum(o.Data)
		sha1s[i] = hex.EncodeToString(sum[:])
	}
	if err := im.lookUpOnModrinth(ctx, sha1s); err != nil {
		return nil, err
	}
	for i, o := range files {
		found, ok := im.onModrinth[sha1s[i]]
		if !ok {
			im.unmatched = append(im.unmatched, o)
			continue
		}
		if im.alreadyRequired(o, found.proj) {
			im.unmanaged(o)
			continue
		}
		side := layerSide(o.Layer)
		if side == "both" {
			side = ""
		}
		locked, err := im.lockFile(ctx, im.modrinth, o.Layer, o.Path, side, found.proj, found.v)
		if err != nil {
			return nil, err
		}
		if !locked {
			im.unmanaged(o)
		}
	}
	if err := im.matchCurseForge(ctx); err != nil {
		return nil, err
	}
	res := &Matched{Locked: im.rep.LockedIDs(), Moved: []string{}, Kept: im.rep.Unmanaged, Warnings: im.rep.Warnings}
	for _, o := range files {
		if file := o.Layer + "/" + o.Path; !slices.Contains(res.Kept, file) {
			res.Moved = append(res.Moved, file)
		}
	}
	sort.Strings(res.Locked)
	sort.Strings(res.Kept)
	return res, nil
}

// alreadyRequired reports, with a warning, whether a project's override file matched as proj is
// one requires already holds the key of, for it to stay an override. A pack's own files are never
// checked: a duplicate there is dropped.
func (im *importer) alreadyRequired(o mrpack.Override, proj *provider.Project) bool {
	if !im.inProject {
		return false
	}
	key := proj.Slug
	if mrpack.IsModJar(o.Path) {
		info, err := jarmeta.Read(filepath.Join(im.r.Dir, o.Layer, filepath.FromSlash(o.Path)), path.Base(o.Path), im.r.Lock.Loader.Type)
		if err != nil {
			im.rep.Warnings = append(im.rep.Warnings, fmt.Sprintf("%s: %v; kept as an override", o.Layer+"/"+o.Path, err))
			return true
		}
		key = info.ID
	}
	_, listed := im.r.Manifest.Requires[key]
	_, locked := im.r.Lock.Mods[key]
	if !listed && !locked {
		return false
	}
	im.rep.Warnings = append(im.rep.Warnings, fmt.Sprintf("requires already has %s, so %s stays an override", key, o.Layer+"/"+o.Path))
	return true
}
