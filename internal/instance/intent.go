package instance

import (
	"errors"

	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/modpack"
)

// SaveIntent writes what a directory syncs from, keeping the settings block a person may have
// edited: a sync never touches it. Every directory shulker syncs into gets one, launcher instance
// or not; a link goes through Intent instead, so it can seed the settings first.
func SaveIntent(dir string, inPlace bool, source string, at modpack.At, side string, assumeClient bool) error {
	f, _, err := Intent(dir, inPlace, source, at, side, assumeClient)
	if err != nil {
		return err
	}
	return f.Save(dir)
}

// Intent is the instance file for a directory with this sync recorded in it, and whether it had
// to be created, which is what tells a link that the settings are still shulker's to seed.
func Intent(dir string, inPlace bool, source string, at modpack.At, side string, assumeClient bool) (*File, bool, error) {
	f, err := Load(dir)
	fresh := false
	switch {
	case errors.Is(err, ErrNotFound):
		f, fresh = New(), true
	case err != nil:
		return nil, false, err
	default:
		f.IsUnlinked = false
	}
	// One writer per fact: an instance that is a project holds the modpack it follows and the side
	// that builds in place in its manifest, so its file keeps no copy of either to go stale.
	if inPlace {
		f.Source, f.Ref, f.Path, f.Side, f.AssumesClient = "", "", "", "", false
	} else {
		f.Source, f.Ref, f.Path, f.Side, f.AssumesClient = source, at.Ref, at.Path, side, assumeClient
	}
	return f, fresh, nil
}

// SeedHooks gives a fresh instance file the manifest's hook defaults. A file that was already there
// keeps its settings block, because it belongs to whoever edited it once it exists.
func SeedHooks(f *File, fresh bool, hooks manifest.Hooks) {
	if !fresh {
		return
	}
	if hooks.PreLaunch != nil {
		f.Settings.Hooks.PreLaunch = hooks.PreLaunch
	}
	if hooks.PostExit != nil {
		f.Settings.Hooks.PostExit = hooks.PostExit
	}
}
