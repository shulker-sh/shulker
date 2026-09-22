// Package cfpack reads and describes CurseForge modpack zips: a manifest.json naming each file by
// project and file id, and one folder of overrides.
package cfpack

import (
	"archive/zip"
	"encoding/json"
	"io"
	"path"
	"sort"
	"strings"

	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/mrpack"
	"shulker.sh/shulker/internal/out"
)

const (
	ManifestName    = "manifest.json"
	ManifestType    = "minecraftModpack"
	ManifestVersion = 1
	// OverridesLayer is the project folder a pack's overrides land in: CurseForge has one folder
	// for both sides.
	OverridesLayer = "overrides"
)

type Manifest struct {
	Minecraft       Minecraft `json:"minecraft"`
	ManifestType    string    `json:"manifestType"`
	ManifestVersion int       `json:"manifestVersion"`
	Name            string    `json:"name"`
	Version         string    `json:"version"`
	Author          string    `json:"author,omitempty"`
	Files           []File    `json:"files"`
	Overrides       string    `json:"overrides"`
	Image           string    `json:"image,omitempty"`
}

type Minecraft struct {
	Version    string      `json:"version"`
	ModLoaders []ModLoader `json:"modLoaders"`
}

type ModLoader struct {
	ID      string `json:"id"`
	Primary bool   `json:"primary"`
}

type File struct {
	ProjectID int  `json:"projectID"`
	FileID    int  `json:"fileID"`
	Required  bool `json:"required"`
	IsLocked  bool `json:"isLocked"`
}

type Archive struct {
	Manifest Manifest
	// Overrides are the files under the folder the manifest names, all in OverridesLayer, less the
	// marker jar of the project that exported it.
	Overrides []mrpack.Override
	// Marker is the shulker project that exported the pack, from the shulker.json and shulker.lock
	// at its root.
	Marker *mrpack.Marker
	Icon   []byte
}

// Loader is the pack's primary mod loader, or its first when none is marked primary. A pack for
// vanilla Minecraft names none, and gives two empty strings.
func (a *Archive) Loader() (string, string, error) {
	loaders := a.Manifest.Minecraft.ModLoaders
	if len(loaders) == 0 {
		return "", "", nil
	}
	id := loaders[0].ID
	for _, l := range loaders {
		if l.Primary {
			id = l.ID
			break
		}
	}
	name, version, ok := loader.ParseCurseForgeModLoader(id)
	if !ok {
		return "", "", out.Errorf("unsupported-loader", "shulker doesn't support the pack's loader %s", id)
	}
	return name, version, nil
}

// Mrpack is the archive in the shape a Modrinth pack is read in: its Minecraft version and loader
// as index dependencies, its overrides, and no index files, since it names its files by id.
func (a *Archive) Mrpack() (*mrpack.Archive, error) {
	name, version, err := a.Loader()
	if err != nil {
		return nil, err
	}
	deps := map[string]string{mrpack.Game: a.Manifest.Minecraft.Version}
	if l, ok := loader.Lookup(name); ok {
		deps[l.MrpackKey] = version
	}
	index := mrpack.Index{FormatVersion: 1, Game: mrpack.Game, VersionID: a.Manifest.Version, Name: a.Manifest.Name, Dependencies: deps}
	return &mrpack.Archive{Index: index, Overrides: a.Overrides}, nil
}

// Read opens a CurseForge modpack zip. Anything that isn't one is refused by what it holds, not
// by its name, as archive-not-modpack.
func Read(file string) (*Archive, error) {
	zr, err := zip.OpenReader(file)
	if err != nil {
		return nil, NotModpack(file, "a zip")
	}
	defer zr.Close()
	entries := map[string]*zip.File{}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := path.Clean(strings.ReplaceAll(f.Name, "\\", "/"))
		if strings.HasPrefix(name, "../") || name == ".." || path.IsAbs(name) {
			return nil, out.Errorf("curseforge-invalid", "%s contains an unsafe entry %q", file, f.Name)
		}
		entries[name] = f
	}
	mf, ok := entries[ManifestName]
	if !ok {
		if _, isMrpack := entries[mrpack.IndexName]; isMrpack {
			e := out.Errorf("archive-not-modpack", "%s is a Modrinth modpack, not a CurseForge one", file)
			e.Help = "import a Modrinth modpack with `shulker import mrpack`"
			return nil, e
		}
		return nil, NotModpack(file, "a CurseForge modpack")
	}
	data, err := readEntry(mf)
	if err != nil {
		return nil, err
	}
	var probe struct {
		ManifestType string `json:"manifestType"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, out.Errorf("curseforge-invalid", "shulker can't parse %s in %s", ManifestName, file).WithCause("json", err)
	}
	if probe.ManifestType != ManifestType {
		return nil, NotModpack(file, "a CurseForge modpack")
	}
	a := &Archive{}
	if err := json.Unmarshal(data, &a.Manifest); err != nil {
		return nil, out.Errorf("curseforge-invalid", "shulker can't parse %s in %s", ManifestName, file).WithCause("json", err)
	}
	if a.Manifest.ManifestVersion != ManifestVersion {
		return nil, out.Errorf("curseforge-invalid", "%s is manifest version %d; shulker reads version %d", file, a.Manifest.ManifestVersion, ManifestVersion)
	}
	if a.Manifest.Minecraft.Version == "" {
		return nil, out.Errorf("curseforge-invalid", "%s names no minecraft version", file)
	}
	image := ""
	if a.Manifest.Image != "" {
		image = path.Clean(strings.ReplaceAll(a.Manifest.Image, "\\", "/"))
	}
	root := map[string][]byte{}
	names := []string{manifest.FileName, lock.FileName, image}
	for _, name := range names {
		if f, ok := entries[name]; ok {
			if root[name], err = readEntry(f); err != nil {
				return nil, err
			}
		}
	}
	if a.Marker, err = mrpack.ReadRootIdentity(file, root[manifest.FileName], root[lock.FileName]); err != nil {
		return nil, err
	}
	a.Icon = root[image]
	folder := "overrides"
	if a.Manifest.Overrides != "" {
		folder = strings.Trim(path.Clean(strings.ReplaceAll(a.Manifest.Overrides, "\\", "/")), "/")
	}
	for name, f := range entries {
		rel, ok := strings.CutPrefix(name, folder+"/")
		if !ok {
			continue
		}
		data, err := readEntry(f)
		if err != nil {
			return nil, err
		}
		o := mrpack.Override{Layer: OverridesLayer, Path: rel, Data: data}
		marker, err := mrpack.ReadMarker(o)
		if err != nil {
			return nil, err
		}
		if marker == nil {
			a.Overrides = append(a.Overrides, o)
		}
	}
	sort.Slice(a.Overrides, func(i, j int) bool { return a.Overrides[i].Path < a.Overrides[j].Path })
	return a, nil
}

// NotModpack refuses a file whose content is not the kind of archive asked for.
func NotModpack(file, want string) *out.Error {
	return out.Errorf("archive-not-modpack", "%s is not %s", file, want)
}

func readEntry(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}
