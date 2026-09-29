package resolve

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"slices"

	"shulker.sh/shulker/internal/packarchive"
	"shulker.sh/shulker/internal/provider"
)

// Matched is what MatchOverrides made of a project's override files: the entries it locked, the
// files those were, and the files it left as overrides.
type Matched struct {
	Locked   []LockedFile `json:"locked"`
	Moved    []string     `json:"moved"`
	Kept     []string     `json:"kept"`
	Warnings []string     `json:"-"`
}

// MatchOverrides looks the project's override jars and pack zips up on Modrinth by sha1, then what
// Modrinth lacks on CurseForge by fingerprint, and locks each match into requires, its side taken
// from the file's layer. Each file's Layer and Path name it under r.Dir. A file that doesn't match,
// can't be locked, or whose key requires already holds stays an override.
func (r *Resolver) MatchOverrides(ctx context.Context, files []packarchive.Override) (*Matched, error) {
	im := newImporter(r, &packarchive.Archive{}, false)
	im.inProject = true
	for _, o := range files {
		side := packarchive.LayerSide(o.Layer)
		if side == "both" {
			side = ""
		}
		im.toIdentify(o, side)
	}
	if err := im.identify(ctx); err != nil {
		return nil, err
	}
	if err := im.lockIdentified(ctx); err != nil {
		return nil, err
	}
	im.rep.sort()
	im.rep.warnSides()
	res := &Matched{Locked: im.rep.Locked, Moved: []string{}, Kept: im.rep.Unmanaged, Warnings: im.rep.Warnings}
	for _, o := range files {
		if file := o.Layer + "/" + o.Path; !slices.Contains(res.Kept, file) {
			res.Moved = append(res.Moved, file)
		}
	}
	return res, nil
}

// alreadyRequired reports, with a warning, whether a project's override file matched as proj is
// one requires already holds the key of, for it to stay an override. A pack's own files are never
// checked: a duplicate there is dropped.
func (im *importer) alreadyRequired(o packarchive.Override, proj *provider.Project) bool {
	if !im.inProject {
		return false
	}
	key := proj.Slug
	if packarchive.IsModJar(o.Path) {
		info, err := im.r.readJar(filepath.Join(im.r.Dir, o.Layer, filepath.FromSlash(o.Path)), path.Base(o.Path))
		if err != nil {
			im.rep.Warnings = append(im.rep.Warnings, fmt.Sprintf("%s: %v; kept as an override", o.Layer+"/"+o.Path, err))
			return true
		}
		key = jarKey(info.ID, proj.Slug)
	}
	_, listed := im.r.Manifest.Requires[key]
	_, locked := im.r.Lock.Mods[key]
	if !listed && !locked {
		return false
	}
	im.rep.Warnings = append(im.rep.Warnings, fmt.Sprintf("requires already has %s, so %s stays an override.", key, o.Layer+"/"+o.Path))
	return true
}
