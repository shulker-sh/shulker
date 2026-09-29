package resolve

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/manual"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/packarchive"
	"shulker.sh/shulker/internal/provider"
)

// listedByID locks every file the archive lists by provider id, the files of each provider found
// in one request each, whatever the pack's size. A file the provider won't serve is taken from
// DownloadsDir, and every one missing there is named in one missing-files error, so a single pass
// says everything to download.
func (im *importer) listedByID(ctx context.Context) error {
	byProvider := map[string][]packarchive.File{}
	var names []string
	for _, f := range im.a.Files {
		if f.Provider == "" {
			continue
		}
		if _, seen := byProvider[f.Provider]; !seen {
			names = append(names, f.Provider)
		}
		byProvider[f.Provider] = append(byProvider[f.Provider], f)
	}
	r, rep := im.r, im.rep
	for _, name := range names {
		p, err := r.Providers.Get(name)
		if err != nil {
			return err
		}
		found, err := findListed(ctx, p, byProvider[name])
		if err != nil {
			return err
		}
		var missing []string
		var rows []out.Detail
		var byHand []manual.File
		var fetches []groupFetch
		for _, f := range byProvider[name] {
			if !f.Optional {
				fetches = append(fetches, groupFetch{name: found.versions[f.Version].File.Filename, kind: found.projects[f.Project].Type})
			}
		}
		end := r.startGroup(fetches)
		for _, f := range byProvider[name] {
			if f.Optional {
				rep.Warnings = append(rep.Warnings, fmt.Sprintf("skipped %s project %s file %s: the pack marks it optional", p.Title(), f.Project, f.Version))
				continue
			}
			proj, v, err := im.listedFile(ctx, p, found, f)
			if out.CodeOf(err) == "manual-download" && r.SkipPending && im.lockPending(p, proj, v, f) {
				continue
			}
			if out.CodeOf(err) == "manual-download" {
				missing = append(missing, fmt.Sprintf("%s: download %s from %s and place it in %s/", proj.Slug, v.File.Filename, v.Page, r.downloads()))
				rows = append(rows, ManualRow(v.File.Filename, v.Page))
				byHand = append(byHand, manual.File{Name: v.File.Filename, Page: v.Page, Sha1: v.File.Sha1, Sha512: v.File.Sha512})
				continue
			}
			if err != nil {
				end(true)
				return err
			}
		}
		end(false)
		if len(missing) > 0 {
			e := out.Errorf("missing-files", "%s a manual download", out.Count(len(missing), "file needs", "files need"))
			e.Items, e.Rows = missing, rows
			manual.Attach(e, byHand)
			e.Help = "download them, then run the command again"
			return e
		}
	}
	return nil
}

// lockedListed keeps what listedFile locked under key, unless the exporting project locked the
// same bytes, which then come back as it locked them.
func (im *importer) lockedListed(p provider.Provider, key, kind string, listed manifest.Require) {
	packs := im.r.Lock.Packs(kind)
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
	im.rep.locked(key, kind, p.Name())
}

// listed is what a provider has of the files a pack lists by its ids: their projects and
// versions, by id, and why a version it has can't be used.
type listed struct {
	projects map[string]provider.Project
	versions map[string]provider.Version
	unusable map[string]error
}

// findListed fetches the projects and versions of every required file in one request each.
func findListed(ctx context.Context, p provider.Provider, files []packarchive.File) (listed, error) {
	var projectIDs, versionIDs []string
	for _, f := range files {
		if !f.Optional {
			projectIDs = append(projectIDs, f.Project)
			versionIDs = append(versionIDs, f.Version)
		}
	}
	if len(versionIDs) == 0 {
		return listed{}, nil
	}
	slices.Sort(projectIDs)
	slices.Sort(versionIDs)
	projects, err := p.Projects(ctx, slices.Compact(projectIDs))
	if err != nil {
		return listed{}, err
	}
	versions, unusable, err := p.VersionsByID(ctx, slices.Compact(versionIDs))
	if err != nil {
		return listed{}, err
	}
	return listed{projects: projects, versions: versions, unusable: unusable}, nil
}

func (im *importer) listedFile(ctx context.Context, p provider.Provider, found listed, f packarchive.File) (*provider.Project, *provider.Version, error) {
	r, rep := im.r, im.rep
	project, ok := found.projects[f.Project]
	if !ok {
		return nil, nil, out.Errorf("mod-not-found", "%s has no project %s", p.Title(), f.Project)
	}
	proj := &project
	if err := found.unusable[f.Version]; err != nil {
		return nil, nil, err
	}
	version, ok := found.versions[f.Version]
	if !ok {
		return nil, nil, out.Errorf("version-not-found", "%s has no file %s for %s", p.Title(), f.Version, proj.Slug)
	}
	v := &version
	if v.ProjectID != proj.ID {
		return nil, nil, out.Errorf("pin-mismatch", "file %s belongs to project %s, not %s", f.Version, v.ProjectID, proj.Slug)
	}
	channel := shippedChannel(v)
	listedFrom := func(p provider.Provider, proj *provider.Project) manifest.Require {
		listed := manifest.Require{Project: proj.ID, Channel: channel}
		if p.Name() != r.Manifest.ProviderOrder()[0] {
			listed.Provider = p.Name()
		}
		return listed
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
		from, id, prior, err := r.placeAnywhere(ctx, hosted{p, proj, v}, "", "", "", channel, false, func(w string) { rep.Warnings = append(rep.Warnings, w) })
		p, proj, v = from.p, from.proj, from.v
		if err != nil {
			return proj, v, err
		}
		if prior != nil && !im.duplicate(id, prior, proj.ID, v.File.Filename) {
			return proj, v, nil
		}
		im.lockedListed(p, id, kind, listedFrom(p, proj))
	case manifest.TypeResourcePack, manifest.TypeShader, manifest.TypeDatapack:
		key := proj.Slug
		if ok, err := im.canListPack(key, kind); !ok {
			return proj, v, err
		}
		if err := r.lockPackVersion(ctx, p, proj, v, key, kind, channel); err != nil {
			return proj, v, err
		}
		listed := listedFrom(p, proj)
		listed.Type = kind
		listed.Filename = providerPackName(key, kind, v.File.Filename)
		r.placePack(key, kind, listed)
		im.lockedListed(p, key, kind, listed)
	default:
		return nil, nil, out.Errorf("requires-unsupported", "%s is a %s, which a pack can't carry", proj.Slug, kind)
	}
	return proj, v, nil
}

// lockPending locks a mod whose manual download was skipped without its bytes, by the sha1 the
// provider gives, so a later install asks for it again. Only a mod with a sha1 can wait that way.
func (im *importer) lockPending(p provider.Provider, proj *provider.Project, v *provider.Version, f packarchive.File) bool {
	if (proj.Type != "" && proj.Type != manifest.TypeMod) || v.File.Sha1 == "" {
		return false
	}
	key := proj.Slug
	if !manifest.IsValidKey(key) {
		key = nameKey(key)
	}
	side := cmp.Or(f.Side, proj.Side, "both")
	channel := shippedChannel(v)
	im.r.Lock.Mods[key] = lock.Mod{Provider: p.Name(), Project: proj.ID, Version: v.ID, VersionNumber: v.Number, Filename: v.File.Filename, Page: v.Page, Sha1: v.File.Sha1, Size: v.File.Size, Side: side, SideFrom: sideFromProvider, Channel: channelLabel(channel), RequiredBy: []string{}}
	entry := manifest.Require{Channel: channel}
	im.r.setSource(&entry, key, p, proj)
	im.r.Manifest.Requires[key] = entry
	im.rep.Pending = append(im.rep.Pending, key)
	return true
}
