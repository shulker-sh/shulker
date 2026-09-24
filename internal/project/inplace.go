package project

import (
	"errors"
	"os"
	"path/filepath"

	"shulker.sh/shulker/internal/manifest"
)

// InPlace is the manifest at dir when a side of it builds there, which is what makes the directory
// an instance rather than a project that builds elsewhere. It takes no lock: nothing a reader
// deciding what a directory is does builds.
func InPlace(dir string) (m *manifest.Manifest, side string, ok bool, err error) {
	m, err = manifest.Load(filepath.Join(dir, manifest.FileName))
	if errors.Is(err, os.ErrNotExist) {
		return nil, "", false, nil
	}
	if err != nil {
		return nil, "", false, err
	}
	side, ok = m.InPlaceSide()
	return m, side, ok, nil
}

// InPlaceIntent is what an instance that is a project reads from its manifest rather than from its
// instance file: the modpack entry it follows, and the side that builds where it stands. With
// several modpacks required, none of them is the one it was linked from, so the entry comes back
// empty.
func InPlaceIntent(dir string) (pack manifest.Require, side string, inPlace bool) {
	m, side, inPlace, err := InPlace(dir)
	if err != nil || !inPlace {
		return manifest.Require{}, "", false
	}
	if key := ModpackKey(m, ""); key != "" {
		pack = m.Requires[key]
	}
	return pack, side, true
}

// InPlaceID is the id an instance that is a project was linked under: link makes the manifest's
// name and the id one value, and the folder the launcher named after it is a different one.
func InPlaceID(dir string) string {
	m, _, inPlace, err := InPlace(dir)
	if err != nil || !inPlace {
		return ""
	}
	return m.Name
}
