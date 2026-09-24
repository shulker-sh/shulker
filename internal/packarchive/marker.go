package packarchive

import (
	"archive/zip"
	"bytes"
	"io"
	"path"

	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
)

// Marker is the shulker project an export carries, from the archive root or a marker jar. Layer
// is empty when it came from the root.
type Marker struct {
	Layer    string
	Path     string
	Manifest *manifest.Manifest
	Lock     *lock.Lock
}

func readRootIdentity(file string, manifestData, lockData []byte) (*Marker, error) {
	if manifestData == nil || lockData == nil {
		return nil, nil
	}
	m, err := manifest.Parse(manifestData)
	if err != nil {
		return nil, markerInvalid(file, manifest.FileName, err)
	}
	l, err := lock.Parse(lockData)
	if err != nil {
		return nil, markerInvalid(file, lock.FileName, err)
	}
	return &Marker{Path: manifest.FileName, Manifest: m, Lock: l}, nil
}

// readMarker reads the shulker marker jar o is, or gives nil when o is some other file.
func readMarker(o Override) (*Marker, error) {
	if path.Dir(o.Path) != "mods" || path.Ext(o.Path) != ".jar" {
		return nil, nil
	}
	zr, err := zip.NewReader(bytes.NewReader(o.Data), int64(len(o.Data)))
	if err != nil {
		return nil, nil
	}
	entries := map[string]*zip.File{}
	for _, f := range zr.File {
		entries[f.Name] = f
	}
	mf, lf := entries[manifest.FileName], entries[lock.FileName]
	if mf == nil || lf == nil {
		return nil, nil
	}
	manifestData, err := readEntry(mf)
	if err != nil {
		return nil, err
	}
	lockData, err := readEntry(lf)
	if err != nil {
		return nil, err
	}
	m, err := manifest.Parse(manifestData)
	if err != nil {
		return nil, markerInvalid("marker jar "+o.Layer+"/"+o.Path, manifest.FileName, err)
	}
	l, err := lock.Parse(lockData)
	if err != nil {
		return nil, markerInvalid("marker jar "+o.Layer+"/"+o.Path, lock.FileName, err)
	}
	return &Marker{Layer: o.Layer, Path: o.Path, Manifest: m, Lock: l}, nil
}

func readEntry(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

func markerInvalid(where, name string, err error) *out.Error {
	parsed := out.AsError(err)
	e := out.Errorf("mrpack-marker", "%s has a %s shulker can't read", where, name)
	e.Rows = []out.Detail{{Label: name, Text: parsed.Message, Children: parsed.Rows}}
	return e
}
