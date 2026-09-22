package resolve

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"

	"shulker.sh/shulker/internal/cfpack"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/mrpack"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/provider"
)

// ImportCurseForge locks every file a CurseForge pack names, by its project and file id. A file
// CurseForge won't serve is taken from DownloadsDir, and every one missing there is named in one
// missing-files error, so a single pass says everything to download. A file the exporting shulker
// project locked, matched by sha512, comes back as that project locked it.
func (r *Resolver) ImportCurseForge(ctx context.Context, a *cfpack.Archive) (*Imported, error) {
	p, ok := r.Providers["curseforge"]
	if !ok {
		return nil, Unavailable("curseforge")
	}
	im := newImporter(r, &mrpack.Archive{Marker: a.Marker}, true)
	im.keepSides = true
	rep := im.rep
	var missing []string
	for _, f := range a.Manifest.Files {
		if !f.Required {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("skipped CurseForge project %d file %d: the pack marks it optional", f.ProjectID, f.FileID))
			continue
		}
		proj, v, err := im.curseForgeFile(ctx, p, f)
		if out.CodeOf(err) == "manual-download" {
			missing = append(missing, fmt.Sprintf("%s: download %s from %s and place it in %s/", proj.Slug, v.File.Filename, v.Page, filepath.Join(r.Dir, DownloadsDir)))
			continue
		}
		if err != nil {
			return nil, err
		}
	}
	if len(missing) > 0 {
		e := out.Errorf("missing-files", "%d file(s) need a manual download", len(missing))
		e.Items = missing
		e.Help = "download them, then run the command again"
		return nil, e
	}
	for _, o := range a.Overrides {
		if err := im.override(o); err != nil {
			return nil, err
		}
	}
	im.dropUnmatched()
	sort.Strings(rep.Locked)
	sort.Strings(rep.Reused)
	sort.Strings(rep.Dropped)
	sort.Strings(rep.Unmanaged)
	return rep, nil
}

// lockedFromCurseForge keeps what curseForgeFile locked under key, unless the exporting project
// locked the same bytes, which then come back as it locked them.
func (im *importer) lockedFromCurseForge(key, kind string, listed manifest.Require) {
	packs := im.r.Lock.ResourcePacks
	if kind == manifest.TypeShader {
		packs = im.r.Lock.Shaders
	}
	if id, ok := im.bySha[im.r.Lock.Mods[key].Sha512]; ok && kind == manifest.TypeMod {
		delete(im.r.Lock.Mods, key)
		im.reuse(id, "")
		return
	}
	if p, ok := im.packBySha[packs[key].Sha512]; ok && kind != manifest.TypeMod && p.kind == kind {
		delete(packs, key)
		im.reusePack(p)
		return
	}
	im.r.Manifest.Requires[key] = listed
	im.rep.Locked = append(im.rep.Locked, key)
}

func (im *importer) curseForgeFile(ctx context.Context, p provider.Provider, f cfpack.File) (*provider.Project, *provider.Version, error) {
	r, rep := im.r, im.rep
	projectID, fileID := strconv.Itoa(f.ProjectID), strconv.Itoa(f.FileID)
	proj, err := p.Project(ctx, projectID, "")
	if errors.Is(err, provider.ErrNotFound) {
		return nil, nil, out.Errorf("mod-not-found", "curseforge has no project %s", projectID)
	}
	if err != nil {
		return nil, nil, err
	}
	v, err := p.Version(ctx, fileID)
	if errors.Is(err, provider.ErrNotFound) {
		return nil, nil, out.Errorf("version-not-found", "curseforge has no file %s for %s", fileID, proj.Slug)
	}
	if err != nil {
		return nil, nil, err
	}
	if v.ProjectID != proj.ID {
		return nil, nil, out.Errorf("pin-mismatch", "file %s belongs to project %s, not %s", fileID, v.ProjectID, proj.Slug)
	}
	listed := manifest.Require{Project: lockID(p.Name(), proj.ID)}
	if p.Name() != r.Manifest.ProviderOrder()[0] {
		listed.Provider = p.Name()
	}
	kind := proj.Type
	if kind == "" {
		kind = manifest.TypeMod
	}
	switch kind {
	case manifest.TypeMod:
		if r.Lock.Loader.Type == "" {
			return nil, nil, out.Errorf("loader-required", "%s is a mod, and the pack names no mod loader", proj.Slug)
		}
		id, prior, err := r.place(ctx, p, proj, v, "", "", "", "", false)
		if err != nil {
			return proj, v, err
		}
		if prior != nil {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("%s appears twice in the pack; kept %s", id, r.Lock.Mods[id].Filename))
			return proj, v, nil
		}
		im.lockedFromCurseForge(id, kind, listed)
	case manifest.TypeResourcePack, manifest.TypeShader:
		key := proj.Slug
		if ok, err := im.canListPack(key, kind); !ok {
			return proj, v, err
		}
		if err := r.lockPackVersion(ctx, p, proj, v, key, kind, ""); err != nil {
			return proj, v, err
		}
		listed.Type = kind
		im.lockedFromCurseForge(key, kind, listed)
	default:
		return nil, nil, out.Errorf("requires-unsupported", "%s is a %s, which a pack can't carry", proj.Slug, kind)
	}
	return proj, v, nil
}
