// Package mrpack reads Modrinth modpack archives, and the shulker project an export from shulker
// carries inside one.
package mrpack

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"path"
	"sort"
	"strings"

	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
)

const (
	IndexName = "modrinth.index.json"
	// IconName is the root entry the Modrinth App takes as the instance icon; the format itself has no icon.
	IconName      = "icon.png"
	FormatVersion = 1
	Game          = "minecraft"
)

// Layers are the archive's override folders: files for both sides, then client-only and
// server-only ones.
var Layers = []string{"overrides", "client-overrides", "server-overrides"}

type Index struct {
	FormatVersion int               `json:"formatVersion"`
	Game          string            `json:"game"`
	VersionID     string            `json:"versionId"`
	Name          string            `json:"name"`
	Summary       string            `json:"summary,omitempty"`
	Files         []File            `json:"files"`
	Dependencies  map[string]string `json:"dependencies"`
}

type File struct {
	Path      string            `json:"path"`
	Hashes    map[string]string `json:"hashes"`
	Env       map[string]string `json:"env"`
	Downloads []string          `json:"downloads"`
	FileSize  int64             `json:"fileSize"`
}

// Env is the index env for a file needed on side: "client", "server", or anything else for both.
func Env(side string) map[string]string {
	env := map[string]string{"client": "required", "server": "required"}
	switch side {
	case "client":
		env["server"] = "unsupported"
	case "server":
		env["client"] = "unsupported"
	}
	return env
}

func (f File) Side() string {
	switch {
	case f.Env["server"] == "unsupported":
		return "client"
	case f.Env["client"] == "unsupported":
		return "server"
	}
	return "both"
}

// IsModJar reports whether an archive path is a jar in mods/.
func IsModJar(p string) bool {
	return path.Dir(p) == "mods" && strings.EqualFold(path.Ext(p), ".jar")
}

// IsPackZip reports whether an archive path is a zip in resourcepacks/ or shaderpacks/.
func IsPackZip(p string) bool {
	dir := path.Dir(p)
	return (dir == "resourcepacks" || dir == "shaderpacks") && strings.EqualFold(path.Ext(p), ".zip")
}

func (f File) Layer() string {
	switch f.Side() {
	case "client":
		return "client-overrides"
	case "server":
		return "server-overrides"
	}
	return "overrides"
}

type Override struct {
	Layer string
	Path  string
	Data  []byte
}

// Marker is the shulker project an export carries, from the archive root or a marker jar. Layer
// is empty when it came from the root.
type Marker struct {
	Layer    string
	Path     string
	Manifest *manifest.Manifest
	Lock     *lock.Lock
}

type Archive struct {
	Index     Index
	Overrides []Override
	Marker    *Marker
	Icon      []byte
}

func (a *Archive) Loader() (string, string, bool) {
	for _, l := range loader.All {
		if v, ok := a.Index.Dependencies[l.MrpackKey]; ok {
			return l.Name, v, true
		}
	}
	return "", "", false
}

func Read(file string) (*Archive, error) {
	zr, err := zip.OpenReader(file)
	if err != nil {
		return nil, out.Errorf("mrpack-invalid", "%s is not a readable mrpack", file).WithCause("zip", err)
	}
	defer zr.Close()
	a := &Archive{}
	root := map[string][]byte{}
	found := false
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := path.Clean(strings.ReplaceAll(f.Name, "\\", "/"))
		if strings.HasPrefix(name, "../") || path.IsAbs(name) {
			return nil, out.Errorf("mrpack-invalid", "%s contains an unsafe entry %q", file, f.Name)
		}
		data, err := readEntry(f)
		if err != nil {
			return nil, err
		}
		if name == IndexName {
			found = true
			if err := json.Unmarshal(data, &a.Index); err != nil {
				return nil, out.Errorf("mrpack-invalid", "shulker can't parse %s in %s", IndexName, file).WithCause("json", err)
			}
			continue
		}
		if name == IconName {
			a.Icon = data
			continue
		}
		if name == manifest.FileName || name == lock.FileName {
			root[name] = data
			continue
		}
		layer, rel, ok := splitLayer(name)
		if !ok {
			continue
		}
		a.Overrides = append(a.Overrides, Override{Layer: layer, Path: rel, Data: data})
	}
	if !found {
		return nil, out.Errorf("mrpack-invalid", "%s has no %s", file, IndexName)
	}
	if a.Index.FormatVersion != FormatVersion || a.Index.Game != Game {
		return nil, out.Errorf("mrpack-unsupported", "%s is format %d for %q; shulker reads format %d for %q", file, a.Index.FormatVersion, a.Index.Game, FormatVersion, Game)
	}
	sort.Slice(a.Overrides, func(i, j int) bool {
		if a.Overrides[i].Layer != a.Overrides[j].Layer {
			return a.Overrides[i].Layer < a.Overrides[j].Layer
		}
		return a.Overrides[i].Path < a.Overrides[j].Path
	})
	if err := a.findMarker(); err != nil {
		return nil, err
	}
	if err := a.readRootIdentity(file, root); err != nil {
		return nil, err
	}
	return a, nil
}

// readRootIdentity takes the manifest and lock a shulker export writes at the
// archive root. They win over a marker jar: every export of a project that keeps
// the marker carries them, while the jar reaches only a client-side export of a
// project with a loader.
func (a *Archive) readRootIdentity(file string, root map[string][]byte) error {
	manifestData, lockData := root[manifest.FileName], root[lock.FileName]
	if manifestData == nil || lockData == nil {
		return nil
	}
	m, err := manifest.Parse(manifestData)
	if err != nil {
		return markerInvalid(file, manifest.FileName, err)
	}
	l, err := lock.Parse(lockData)
	if err != nil {
		return markerInvalid(file, lock.FileName, err)
	}
	a.Marker = &Marker{Path: manifest.FileName, Manifest: m, Lock: l}
	return nil
}

func readEntry(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

func splitLayer(name string) (string, string, bool) {
	for _, layer := range Layers {
		if strings.HasPrefix(name, layer+"/") {
			return layer, strings.TrimPrefix(name, layer+"/"), true
		}
	}
	return "", "", false
}

func (a *Archive) findMarker() error {
	kept := a.Overrides[:0]
	for _, o := range a.Overrides {
		m, err := ReadMarker(o)
		if err != nil {
			return err
		}
		if m == nil {
			kept = append(kept, o)
			continue
		}
		if a.Marker != nil {
			return out.Errorf("mrpack-invalid", "two shulker marker jars: %s/%s and %s/%s", a.Marker.Layer, a.Marker.Path, o.Layer, o.Path)
		}
		a.Marker = m
	}
	a.Overrides = kept
	return nil
}

// ReadMarker reads the shulker marker jar o is, or gives nil when o is some other file.
func ReadMarker(o Override) (*Marker, error) {
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

func markerInvalid(where, name string, err error) *out.Error {
	parsed := out.AsError(err)
	e := out.Errorf("mrpack-marker", "%s has a %s shulker can't read", where, name)
	e.Rows = []out.Detail{{Label: name, Text: parsed.Message, Children: parsed.Rows}}
	return e
}
