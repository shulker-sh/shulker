package mrpack

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"path"
	"sort"
	"strings"

	"github.com/shulker-sh/shulker/internal/lock"
	"github.com/shulker-sh/shulker/internal/manifest"
	"github.com/shulker-sh/shulker/internal/out"
)

const (
	IndexName     = "modrinth.index.json"
	FormatVersion = 1
	Game          = "minecraft"
)

var Layers = []string{"overrides", "client-overrides", "server-overrides"}

var LoaderKeys = map[string]string{"fabric": "fabric-loader", "quilt": "quilt-loader", "neoforge": "neoforge", "forge": "forge"}

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
}

func (a *Archive) Loader() (string, string, bool) {
	for loaderType, key := range LoaderKeys {
		if v, ok := a.Index.Dependencies[key]; ok {
			return loaderType, v, true
		}
	}
	return "", "", false
}

func Read(file string) (*Archive, error) {
	zr, err := zip.OpenReader(file)
	if err != nil {
		return nil, out.Errorf("mrpack-invalid", "%s is not a readable mrpack: %v", file, err)
	}
	defer zr.Close()
	a := &Archive{}
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
				return nil, out.Errorf("mrpack-invalid", "%s: %s: %v", file, IndexName, err)
			}
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
	return a, nil
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
		m, err := readMarker(o)
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
		return nil, out.Errorf("mrpack-marker", "marker jar %s/%s: %s: %v", o.Layer, o.Path, manifest.FileName, err)
	}
	l, err := lock.Parse(lockData)
	if err != nil {
		return nil, out.Errorf("mrpack-marker", "marker jar %s/%s: %s: %v", o.Layer, o.Path, lock.FileName, err)
	}
	return &Marker{Layer: o.Layer, Path: o.Path, Manifest: m, Lock: l}, nil
}
