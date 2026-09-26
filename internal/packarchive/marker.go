package packarchive

import (
	"archive/zip"
	"bytes"
	"io"
	"path"

	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/managed"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/schema"
)

// Marker is the shulker project an export carries, from the archive root or a marker jar. Layer
// is empty when it came from the root.
type Marker struct {
	Layer    string
	Path     string
	Manifest *manifest.Manifest
	Lock     *lock.Lock
	// Folders is the project folder each override came from, by its layer and path, where the
	// export recorded one.
	Folders map[string]string
}

// FoldersFile is where an export records the project folder each override came from, beside the
// root manifest and lock: a feature's files ship in overrides/ like any other.
const FoldersFile = "shulker.overrides.json"

type folders struct {
	Schema  string            `json:"$schema"`
	Folders map[string]string `json:"folders"`
}

func (x *Export) addIdentity(entries map[string][]byte) error {
	if x.Manifest == nil || x.Lock == nil {
		return nil
	}
	entries[manifest.FileName] = x.Manifest
	entries[lock.FileName] = x.Lock
	if len(x.Folders) == 0 {
		return nil
	}
	data, err := fsutil.MarshalJSON(folders{Schema: schema.Base + string(schema.Overrides), Folders: x.Folders})
	if err != nil {
		return err
	}
	entries[FoldersFile] = data
	return nil
}

func readRootIdentity(file string, manifestData, lockData, foldersData []byte) (*Marker, error) {
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
	marker := &Marker{Path: manifest.FileName, Manifest: m, Lock: l}
	if foldersData != nil {
		var f folders
		if err := managed.Decode(schema.Overrides, FoldersFile, foldersData, &f); err != nil {
			return nil, markerInvalid(file, FoldersFile, err)
		}
		marker.Folders = f.Folders
	}
	return marker, nil
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
