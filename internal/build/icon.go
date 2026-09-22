package build

import (
	"bytes"
	"errors"
	"image/png"
	"io/fs"
	"os"
	"path"
	"path/filepath"

	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
)

// ReadIcon reads the icon a manifest in dir names, or nothing when it names none.
func ReadIcon(dir string, m *manifest.Manifest) ([]byte, error) {
	if m == nil || m.Icon == "" {
		return nil, nil
	}
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(m.Icon)))
	if errors.Is(err, fs.ErrNotExist) {
		e := out.Errorf("manifest-invalid", "icon %q doesn't exist in %s", m.Icon, dir)
		e.Rows = []out.Detail{{Label: "Fix", Text: "add the PNG at that path, or remove the icon key"}}
		return nil, e
	} else if err != nil {
		return nil, err
	}
	if _, err := png.DecodeConfig(bytes.NewReader(data)); err != nil {
		e := out.Errorf("manifest-invalid", "icon %q is not a PNG", m.Icon)
		e.Rows = []out.Detail{{Label: "Fix", Text: "save the icon as a PNG"}}
		return nil, e
	}
	return data, nil
}

// exportIcon is the image an export carries and the file name it goes by: the pack's own icon,
// else the shulker icon for a pack that uses the marker, else none.
func (b *Builder) exportIcon() ([]byte, string, error) {
	data, err := ReadIcon(b.Dir, b.Manifest)
	if err != nil || data != nil {
		return data, path.Base(b.Manifest.Icon), err
	}
	if b.Manifest.UsesMarker() {
		return markerIcon, markerLogo, nil
	}
	return nil, "", nil
}

// InstanceIcon is the icon a launcher shows for an instance: its own manifest's, else the first
// followed pack's that names one. An archive pack's icon isn't read.
func (b *Builder) InstanceIcon() ([]byte, error) {
	if data, err := ReadIcon(b.Dir, b.Manifest); err != nil || data != nil {
		return data, err
	}
	for _, p := range b.Packs {
		if p.Archive != nil {
			continue
		}
		if data, err := ReadIcon(p.Dir, p.Manifest); err != nil || data != nil {
			return data, err
		}
	}
	return nil, nil
}
