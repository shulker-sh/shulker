package link

import (
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/modpack"
	"shulker.sh/shulker/internal/project"
)

// Settings are what a link seeds an instance with over the manifest's hook defaults: the switches
// it turns off, the marker choice when the link makes one (nil defers to the manifest), and the
// Java and wrapper this machine runs the instance with. On a relink only these land, because the
// settings block belongs to whoever edited it once it exists, and no sync rewrites it.
type Settings struct {
	NoPreLaunch bool
	NoPostExit  bool
	Marker      *bool
	Java        string
	Wrapper     []string
}

// save writes what a directory syncs from, and the settings this link decided.
func (s Settings) save(dir, source string, at modpack.At, m *manifest.Manifest) error {
	_, _, inPlace, err := project.InPlace(dir)
	if err != nil {
		return err
	}
	f, fresh, err := instance.Intent(dir, inPlace, source, at, "client", false)
	if err != nil {
		return err
	}
	instance.SeedHooks(f, fresh, m.ClientHooks())
	if s.NoPreLaunch {
		f.Settings.Hooks.PreLaunch = instance.Off()
	}
	if s.NoPostExit {
		f.Settings.Hooks.PostExit = instance.Off()
	}
	if s.Marker != nil {
		f.Settings.Marker = s.Marker
	}
	if s.Java != "" {
		f.Settings.Java = s.Java
	}
	if len(s.Wrapper) > 0 {
		f.Settings.Wrapper = s.Wrapper
	}
	return f.Save(dir)
}
