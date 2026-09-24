package project

import (
	"errors"
	"os"
	"path/filepath"
	"slices"

	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/local"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
)

// SyncedFrom lists what is synced from p: the registry rows that name it as their source, then
// the sync directories its local file records that have no row, as detached builds under ids no
// row holds. The project's own directory is never among them, however it was synced.
func SyncedFrom(p *Project, registry []config.Instance, lf *local.File) ([]InstanceEntry, error) {
	dir, err := filepath.Abs(p.Dir)
	if err != nil {
		return nil, err
	}
	var all []InstanceEntry
	for _, in := range registry {
		if config.SameDir(in.Source, dir) && !config.SameDir(in.Dir, dir) {
			all = append(all, Inspect(in))
		}
	}
	taken := slices.Clone(registry)
	for _, side := range p.Manifest.Sides() {
		for _, d := range lf.ExistingSyncDirs(side) {
			if config.SameDir(d, dir) || slices.ContainsFunc(all, func(e InstanceEntry) bool { return config.SameDir(e.Dir, d) }) {
				continue
			}
			e := Inspect(config.Instance{Name: p.Manifest.DisplayName(side), Dir: d, Source: dir})
			e.ID = config.InstanceID(taken, "", filepath.Base(d), d)
			taken = append(taken, config.Instance{ID: e.ID, Dir: d})
			e.Detached = true
			if e.Side == "" {
				e.Side = side
			}
			all = append(all, e)
		}
	}
	return all, nil
}

// BuildDirs is the build directory for side plus every directory that side was synced into and
// that still exists: registered instances first, then any the local file recorded. It is what a
// diff or a pull compares against.
func BuildDirs(p *Project, registry []config.Instance, lf *local.File, side string) (buildDir string, dirs []string, err error) {
	buildDir, err = filepath.Abs(filepath.Join(p.Dir, p.Manifest.BuildDir(side)))
	if err != nil {
		return "", nil, err
	}
	synced, err := SyncedFrom(p, registry, lf)
	if err != nil {
		return "", nil, err
	}
	seen := map[string]bool{}
	add := func(dir string) {
		if dir == "" || seen[dir] {
			return
		}
		if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
			return
		}
		seen[dir] = true
		dirs = append(dirs, dir)
	}
	add(buildDir)
	for _, e := range synced {
		if e.Side == side {
			add(e.Dir)
		}
	}
	for _, d := range lf.ExistingSyncDirs(side) {
		add(d)
	}
	return buildDir, dirs, nil
}

// SyncTarget is where a sync of the project at srcDir builds its side. A remote source, named by
// remoteSource, needs --into; the build dir is the manifest's for the side, and into equal to it
// is the project's own build; anything else is a synced directory.
func SyncTarget(m *manifest.Manifest, srcDir, side, into, remoteSource string) (dir string, ownBuild, syncedDir bool, err error) {
	if into == "" && remoteSource != "" {
		return "", false, false, out.Errorf("into-required", "--into is required when syncing from %s", remoteSource)
	}
	buildDir, err := filepath.Abs(filepath.Join(srcDir, m.BuildDir(side)))
	if err != nil {
		return "", false, false, err
	}
	dir = into
	if dir == "" {
		dir = buildDir
	}
	if dir, err = filepath.Abs(dir); err != nil {
		return "", false, false, err
	}
	ownBuild = config.SameDir(dir, buildDir)
	return dir, ownBuild, into != "" && !ownBuild, nil
}
