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
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/provider"
)

// ImportCurseForge locks every file a CurseForge pack names, by its project and file id. A file
// CurseForge won't serve is taken from DownloadsDir, and every one missing there is named in one
// missing-files error, so a single pass says everything to download.
func (r *Resolver) ImportCurseForge(ctx context.Context, a *cfpack.Archive) (*Imported, error) {
	p, ok := r.Providers["curseforge"]
	if !ok {
		return nil, Unavailable("curseforge")
	}
	rep := &Imported{Locked: []string{}, Reused: []string{}, Dropped: []string{}, Unmanaged: []string{}, Warnings: []string{}}
	var missing []string
	for _, f := range a.Manifest.Files {
		if !f.Required {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("skipped CurseForge project %d file %d: the pack marks it optional", f.ProjectID, f.FileID))
			continue
		}
		proj, v, err := r.importCurseForgeFile(ctx, p, f, rep)
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
		e.Help = "download them, then run the import again"
		return nil, e
	}
	for _, o := range a.Overrides {
		rep.Overrides = append(rep.Overrides, o)
		if isModJar(o.Path) || isPackZip(o.Path) {
			rep.Unmanaged = append(rep.Unmanaged, o.Layer+"/"+o.Path)
		}
	}
	sort.Strings(rep.Locked)
	sort.Strings(rep.Unmanaged)
	return rep, nil
}

func (r *Resolver) importCurseForgeFile(ctx context.Context, p provider.Provider, f cfpack.File, rep *Imported) (*provider.Project, *provider.Version, error) {
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
		r.Manifest.Requires[id] = listed
		rep.Locked = append(rep.Locked, id)
	case manifest.TypeResourcePack, manifest.TypeShader:
		key := proj.Slug
		if held, taken := r.Manifest.Requires[key]; taken && held.Kind() == kind {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("%s appears twice in the pack; kept the first", key))
			return proj, v, nil
		}
		if !manifest.IsValidKey(key) {
			return nil, nil, out.Errorf("requires-unsupported", "%s can't be a requires key", key)
		}
		if err := r.packKeyFree(key, kind); err != nil {
			return nil, nil, err
		}
		if err := r.lockPackVersion(ctx, p, proj, v, key, kind, ""); err != nil {
			return proj, v, err
		}
		listed.Type = kind
		r.Manifest.Requires[key] = listed
		rep.Locked = append(rep.Locked, key)
	default:
		return nil, nil, out.Errorf("requires-unsupported", "%s is a %s, which a pack can't carry", proj.Slug, kind)
	}
	return proj, v, nil
}
