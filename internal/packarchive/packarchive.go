// Package packarchive reads and writes pack archives, the modpack files launchers exchange, one
// adapter per format. It owns the bytes only: telling a format from what an archive holds,
// decoding one into a description every format shares, and laying an export out in a format.
package packarchive

import (
	"archive/zip"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/zipfile"
)

// Format is one pack archive format. Its facts say what an export can carry and how a launcher
// treats it; reading and writing go through Read and Write.
type Format interface {
	// Name is the format's id, as --type and the export subcommand name it.
	Name() string
	// Title is the format as the user reads it: Modrinth for mrpack.
	Title() string
	// Extension is the file extension an export gets.
	Extension() string
	// NamedByExtension reports whether the extension alone says a file is this format's archive:
	// a .mrpack is only ever a Modrinth modpack, while a .zip may as well be a resource pack.
	NamedByExtension() bool
	Usage() Usage
	// Provider is the provider whose ids the archive lists files by, or empty for a format that
	// lists files by download.
	Provider() string
	// Sided reports whether the archive says which side each file and override is for. A
	// format that doesn't carries one side, with its overrides in one folder.
	Sided() bool
	// Renames reports whether the launcher saves a listed file under the provider's own file
	// name rather than the path the listing gives.
	Renames() bool
	// Places reports whether the launcher lays a listed file of kind at path.
	Places(kind, path string) bool
	// Lists reports whether the listing can carry f: by a download from a host the format's
	// launchers fetch from, or by the ids of the provider it names.
	Lists(f File) bool
	// CantPlace refuses the files, named by what, that Places turned down.
	CantPlace(what string) *out.Error
	// NotListed refuses the n files, named by what, that Lists turned down.
	NotListed(what string, n int) *out.Error

	detect(z *zipEntries) bool
	decode(file string, z *zipEntries) (*Archive, error)
	encode(x *Export) (entries map[string][]byte, first string, err error)
}

// Usage is the wording a format's commands and messages use.
type Usage struct {
	// Short describes the export subcommand.
	Short string
	// Archive describes what an archive of the format is, for a file that isn't one.
	Archive string
	// Listed says how a listed file reaches the launcher: by download, by file ID.
	Listed string
	// Bundle is the --bundle flag's help.
	Bundle string
	// Bundled is what bundling a file means for the recipient.
	Bundled string
}

// Formats are every format, in the order they are tried when reading an archive.
var Formats = []Format{mrpack{}, cfpack{}}

// Lookup is the format named name.
func Lookup(name string) (Format, bool) {
	for _, f := range Formats {
		if f.Name() == name {
			return f, true
		}
	}
	return nil, false
}

// Names are the formats' names, in Formats' order.
func Names() []string {
	names := make([]string, len(Formats))
	for i, f := range Formats {
		names[i] = f.Name()
	}
	return names
}

// HasArchiveExtension reports whether name ends the way some format's archive does, so the name
// alone says to expect a pack archive rather than a jar or a folder.
func HasArchiveExtension(name string) bool {
	ext := strings.ToLower(path.Ext(name))
	return slices.ContainsFunc(Formats, func(f Format) bool { return f.Extension() == ext })
}

// NamedFormat is the format name alone identifies, for a format whose extension nothing else
// uses; ok is false for a name that could be another kind of file.
func NamedFormat(name string) (Format, bool) {
	ext := strings.ToLower(path.Ext(name))
	for _, f := range Formats {
		if f.NamedByExtension() && f.Extension() == ext {
			return f, true
		}
	}
	return nil, false
}

// Archives are the formats' archive descriptions joined for a sentence: a .mrpack, or a
// CurseForge zip with a manifest.json of type minecraftModpack.
func Archives() string {
	kinds := make([]string, len(Formats))
	for i, f := range Formats {
		kinds[i] = f.Usage().Archive
	}
	return strings.Join(kinds, ", or ")
}

// Titles are the formats' titles joined for a sentence: Modrinth or CurseForge.
func Titles() string {
	titles := make([]string, len(Formats))
	for i, f := range Formats {
		titles[i] = f.Title()
	}
	if len(titles) < 2 {
		return strings.Join(titles, "")
	}
	return strings.Join(titles[:len(titles)-1], ", ") + " or " + titles[len(titles)-1]
}

// Archive is a pack archive as every format describes one.
type Archive struct {
	Format    Format
	Name      string
	Version   string
	Summary   string
	Authors   []string
	Minecraft string
	Loader    Loader
	// Memory is the heap the pack recommends for the client, as client.memory takes it.
	Memory string
	// Files are the files the archive lists rather than carries.
	Files []File
	// Overrides are the files the archive carries, less the marker jar of the project that
	// exported it.
	Overrides []Override
	// Marker is the shulker project that exported the archive, when it kept one.
	Marker *Marker
	Icon   []byte
}

// Loader is the mod loader an archive names; empty for vanilla Minecraft.
type Loader struct {
	Type    string
	Version string
}

// File is a listed file: one the archive names by hash and download, or by its provider's ids.
type File struct {
	// Path is where the file goes, relative to the game folder; empty when the listing leaves
	// the name to the provider.
	Path   string
	Hashes map[string]string
	// Side is client, server or both, or empty when the listing doesn't say.
	Side      string
	Downloads []string
	Size      int64
	// Provider, Project and Version name the file on a provider, for a listing by id.
	Provider string
	Project  string
	Version  string
	// Optional marks a file the pack lists but doesn't require.
	Optional bool
	// Filename is the provider's own name for the file, which a launcher that Renames saves it
	// under. Title, Author and Page describe its project, for a listing that names projects.
	Filename string
	Title    string
	Author   string
	Page     string
}

// Layers are the override folders an archive and a project share: files for both sides, then
// client-only and server-only ones.
var Layers = []string{"overrides", "client-overrides", "server-overrides"}

// LayerFor is the override folder for files needed on side; both, or no side, is the shared one.
func LayerFor(side string) string {
	switch side {
	case "client", "server":
		return side + "-overrides"
	}
	return "overrides"
}

// LayerSide is the side an override folder's files are for.
func LayerSide(layer string) string {
	switch layer {
	case "client-overrides":
		return "client"
	case "server-overrides":
		return "server"
	}
	return "both"
}

// Override is a file an archive carries, by the folder it sits in and its path within it.
type Override struct {
	Layer string
	Path  string
	Data  []byte
}

// IsModJar reports whether an override path is a jar in mods/.
func IsModJar(p string) bool {
	return path.Dir(p) == "mods" && strings.EqualFold(path.Ext(p), ".jar")
}

// IsPackZip reports whether an override path is a zip in resourcepacks/, shaderpacks/ or one of
// lock.DatapackFolders.
func IsPackZip(p string) bool {
	dir := path.Dir(p)
	return (dir == "resourcepacks" || dir == "shaderpacks" || IsDatapackZip(p)) && strings.EqualFold(path.Ext(p), ".zip")
}

// IsDatapackZip reports whether an override path is a zip in one of lock.DatapackFolders.
func IsDatapackZip(p string) bool {
	return slices.Contains(lock.DatapackFolders, path.Dir(p)) && strings.EqualFold(path.Ext(p), ".zip")
}

// IsLoadedDatapackZip reports whether an override path is a zip in a global datapack mod's own
// folder, which the mod loads whatever the Minecraft version.
func IsLoadedDatapackZip(p string) bool {
	return IsDatapackZip(p) && path.Dir(p) != "datapacks"
}

// Export is what an export ships, for a format to lay out.
type Export struct {
	Name      string
	Version   string
	Summary   string
	Authors   []string
	Minecraft string
	Loader    Loader
	// Files are listed, in this order; each passed the format's Lists.
	Files     []File
	Overrides []Override
	Icon      []byte
	IconName  string
	// Manifest and Lock are the exporting project's own, kept at the archive root so an export
	// imports back as the project it came from; nil leaves them out.
	Manifest []byte
	Lock     []byte
}

// Read opens a pack archive in whichever format its content is.
func Read(file string) (*Archive, error) {
	zr, err := zip.OpenReader(file)
	if err != nil {
		return nil, NotArchive(file)
	}
	defer zr.Close()
	z := newZipEntries(&zr.Reader)
	for _, f := range Formats {
		if f.detect(z) {
			a, err := f.decode(file, z)
			if err != nil {
				return nil, err
			}
			a.Format = f
			return a, nil
		}
	}
	return nil, NotArchive(file)
}

// IsArchive reports whether the file is a pack archive of some format, by its content.
func IsArchive(file string) bool {
	zr, err := zip.OpenReader(file)
	if err != nil {
		return false
	}
	defer zr.Close()
	z := newZipEntries(&zr.Reader)
	return slices.ContainsFunc(Formats, func(f Format) bool { return f.detect(z) })
}

// NotArchive refuses a file that is no format's archive.
func NotArchive(file string) *out.Error {
	e := out.Errorf("archive-not-modpack", "%s is not a %s modpack", file, Titles())
	e.Help = "a modpack archive is " + Archives()
	return e
}

// Write lays x out in f's format at output, with the entry a streaming reader should meet first
// at the front of the zip.
func Write(f Format, output string, x *Export) error {
	entries, first, err := f.encode(x)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return err
	}
	data, err := zipfile.Build(entries, first)
	if err != nil {
		return err
	}
	return fsutil.Write(output, data)
}

// zipEntries are a zip's files by their cleaned names, folders left out.
type zipEntries struct {
	files map[string]*zip.File
	// raw is the name each entry was stored under.
	raw map[string]string
}

func newZipEntries(zr *zip.Reader) *zipEntries {
	z := &zipEntries{files: map[string]*zip.File{}, raw: map[string]string{}}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := path.Clean(strings.ReplaceAll(f.Name, "\\", "/"))
		z.files[name] = f
		z.raw[name] = f.Name
	}
	return z
}

func (z *zipEntries) has(name string) bool {
	_, ok := z.files[name]
	return ok
}

// unsafe is the stored name of an entry that would escape the folder an archive is laid out
// in, or empty when every entry is safe.
func (z *zipEntries) unsafe() string {
	for name, raw := range z.raw {
		if strings.HasPrefix(name, "../") || name == ".." || path.IsAbs(name) {
			return raw
		}
	}
	return ""
}

// read is the entry's bytes, or nil when the archive has no such entry.
func (z *zipEntries) read(name string) ([]byte, error) {
	f, ok := z.files[name]
	if !ok {
		return nil, nil
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

// names are the entries' cleaned names, sorted.
func (z *zipEntries) names() []string {
	names := make([]string, 0, len(z.files))
	for name := range z.files {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// rootIdentity reads the shulker.json and shulker.lock an export writes at the archive root as
// its marker, or nil when the archive lacks either. They win over a marker jar: every export of a
// project that keeps the marker carries them, while the jar reaches only a client-side export of
// a project with a loader.
func (z *zipEntries) rootIdentity(file string) (*Marker, error) {
	manifestData, err := z.read(manifest.FileName)
	if err != nil {
		return nil, err
	}
	lockData, err := z.read(lock.FileName)
	if err != nil {
		return nil, err
	}
	return readRootIdentity(file, manifestData, lockData)
}

// overridesUnder reads the files under folder in the archive into layer, leaving out the
// marker jar of the project that exported it, which the reading project replaces with its own.
func (z *zipEntries) overridesUnder(folder, layer string) ([]Override, *Marker, error) {
	var overrides []Override
	var marker *Marker
	for _, name := range z.names() {
		rel, ok := strings.CutPrefix(name, folder+"/")
		if !ok {
			continue
		}
		data, err := z.read(name)
		if err != nil {
			return nil, nil, err
		}
		o := Override{Layer: layer, Path: rel, Data: data}
		m, err := readMarker(o)
		if err != nil {
			return nil, nil, err
		}
		if m == nil {
			overrides = append(overrides, o)
			continue
		}
		if marker != nil {
			return nil, nil, out.Errorf("mrpack-invalid", "two shulker marker jars: %s/%s and %s/%s", marker.Layer, marker.Path, o.Layer, o.Path)
		}
		marker = m
	}
	return overrides, marker, nil
}
