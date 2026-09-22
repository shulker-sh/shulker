package game

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/out"
)

// Store is the shared pile a direct launch assembles from, laid out the way the Mojang launcher
// lays out its own directory:
//
//	versions/<id>/<id>.json        a version JSON, vanilla or a loader's
//	versions/<id>/<id>.jar         the client jar
//	libraries/<maven path>         every library, classpath and native alike
//	assets/indexes/<id>.json       an asset index
//	assets/objects/<aa>/<sha1>     the assets it names
//	launcher_profiles.json         a stub, so a loader's own installer will run against the store
//	loaders.json                   which version id each loader's installer wrote
//
// Sharing the layout is what lets the Forge and NeoForge installers, which only know how to
// install into a Mojang launcher directory, install into the store unchanged.
type Store struct {
	Root string
	// Resources is where asset objects are fetched from; empty means Mojang's own host.
	Resources string
}

// File is one file the store holds: where it sits under the root, and where it came from. A file
// with no URL is one a loader installer wrote, so it can only be found, never fetched.
type File struct {
	Path string
	URL  string
	Sha1 string
	Size int64
}

func (s Store) path(parts ...string) string {
	return filepath.Join(append([]string{s.Root}, parts...)...)
}

func (s Store) VersionJSON(id string) string { return s.path("versions", id, id+".json") }
func (s Store) Libraries() string            { return s.path("libraries") }
func (s Store) Assets() string               { return s.path("assets") }
func (s Store) AssetIndex(id string) string  { return s.path("assets", "indexes", id+".json") }

// HasVersion reports whether a version JSON is already in the store.
func (s Store) HasVersion(id string) bool { return s.Has(File{Path: versionPath(id)}) }

// Local is where a file the store holds sits on this machine.
func (s Store) Local(f File) string {
	return filepath.Join(s.Root, filepath.FromSlash(f.Path))
}

// EnsureProfiles writes the stub launcher_profiles.json the Forge and NeoForge installers refuse
// to run without.
func (s Store) EnsureProfiles() error {
	if err := os.MkdirAll(s.Root, 0o755); err != nil {
		return err
	}
	return (&launcher.Mojang{Dir: s.Root}).EnsureProfilesFile()
}

// Version reads one version JSON out of the store, without following inheritsFrom.
func (s Store) Version(id string) (Version, error) {
	data, err := os.ReadFile(s.VersionJSON(id))
	if os.IsNotExist(err) {
		return Version{}, out.Errorf("store-incomplete", "the game store has no version %s at %s", id, s.VersionJSON(id))
	}
	if err != nil {
		return Version{}, err
	}
	v, err := ParseVersion(data)
	if err != nil {
		return Version{}, out.Errorf("store-incomplete", "shulker can't parse %s", s.VersionJSON(id)).WithCause("json", err)
	}
	return v, nil
}

// Resolve reads a version and everything it inherits from, merged into the one version a launch
// runs. The chain is walked rather than recursed on so a version JSON that inherits from itself
// fails instead of hanging.
func (s Store) Resolve(id string) (Version, error) {
	v, err := s.Version(id)
	if err != nil {
		return Version{}, err
	}
	seen := map[string]bool{id: true}
	for v.InheritsFrom != "" {
		parentID := v.InheritsFrom
		if seen[parentID] {
			return Version{}, out.Errorf("store-incomplete", "version %s inherits from itself", id)
		}
		seen[parentID] = true
		parent, err := s.Version(parentID)
		if err != nil {
			return Version{}, err
		}
		v = Merge(parent, v)
	}
	return v, nil
}

// Assembly is one launch's share of the store: what to fetch, what goes on the classpath in what
// order, and which jars are unpacked into the instance's natives directory.
type Assembly struct {
	Version    Version
	Client     File
	AssetIndex File
	Libraries  []File
	Natives    []File
	// Excludes holds, per native file path, the archive entries that jar says not to unpack.
	Excludes map[string][]string
}

// Assemble works out what a merged version needs on this platform. Libraries keep the order the
// merged version gives them, since a classpath resolves a class from the first jar that has it
// and that is how a loader shadows what vanilla ships; a path already on the classpath is left
// where it is.
func Assemble(v Version, p Platform, features map[string]bool) (Assembly, error) {
	client, err := ClientJar(v)
	if err != nil {
		return Assembly{}, err
	}
	a := Assembly{Version: v, Client: client, Excludes: map[string][]string{}}
	if v.AssetIndex != nil {
		a.AssetIndex = File{Path: assetIndexPath(v.AssetIndex.ID), URL: v.AssetIndex.URL, Sha1: v.AssetIndex.Sha1, Size: v.AssetIndex.Size}
	}
	seen := map[string]bool{}
	for _, l := range v.Libraries {
		if !l.Applies(p, features) {
			continue
		}
		f, err := l.File(p)
		if err != nil {
			return Assembly{}, out.Errorf("store-incomplete", "minecraft %s names a library shulker can't place", v.ID).WithCause("library", err)
		}
		if seen[f.Path] {
			continue
		}
		seen[f.Path] = true
		f.Path = libraryPath(f.Path)
		if !l.IsNative() {
			a.Libraries = append(a.Libraries, f)
			continue
		}
		a.Natives = append(a.Natives, f)
		if l.Extract != nil {
			a.Excludes[f.Path] = l.Extract.Exclude
		}
	}
	return a, nil
}

// ClientJar is the client jar a version runs, filed under the version that declares it.
func ClientJar(v Version) (File, error) {
	client, ok := v.Downloads["client"]
	if !ok {
		return File{}, out.Errorf("store-incomplete", "minecraft %s has no client download", v.ID)
	}
	return File{Path: clientPath(v.ClientID), URL: client.URL, Sha1: client.Sha1, Size: client.Size}, nil
}

// LibrariesFrom is the part of the classpath one version in the chain declares itself: for a
// loader's version, the jars the loader brings on top of vanilla.
func (a Assembly) LibrariesFrom(v Version, p Platform) []File {
	own := map[string]bool{}
	for _, l := range v.Libraries {
		if f, err := l.File(p); err == nil && !l.IsNative() {
			own[libraryPath(f.Path)] = true
		}
	}
	var files []File
	for _, f := range a.Libraries {
		if own[f.Path] {
			files = append(files, f)
		}
	}
	return files
}

// Classpath is what the launch runs with: every library jar in order, then the client jar last,
// which is where the Mojang launcher puts it.
func (a Assembly) Classpath(s Store) []string {
	paths := make([]string, 0, len(a.Libraries)+1)
	for _, f := range a.Libraries {
		paths = append(paths, s.Local(f))
	}
	return append(paths, s.Local(a.Client))
}

// ClasspathSize is what the classpath weighs, for the summary a dry run prints.
func (a Assembly) ClasspathSize(s Store) int64 {
	return s.Size(append(a.Libraries[:len(a.Libraries):len(a.Libraries)], a.Client))
}

// Size is what a set of files weighs. A file the version JSON gave no size for counts as what is on
// disk.
func (s Store) Size(files []File) int64 {
	total := int64(0)
	for _, f := range files {
		if f.Size > 0 {
			total += f.Size
			continue
		}
		if info, err := os.Stat(s.Local(f)); err == nil {
			total += info.Size()
		}
	}
	return total
}

func clientPath(id string) string { return "versions/" + id + "/" + id + ".jar" }

func libraryPath(rel string) string { return "libraries/" + rel }

func versionPath(id string) string { return "versions/" + id + "/" + id + ".json" }

func assetIndexPath(id string) string { return "assets/indexes/" + id + ".json" }

// SaveVersion writes a version JSON into the store under the id it names.
func (s Store) SaveVersion(data []byte) (string, error) {
	return (&launcher.Mojang{Dir: s.Root}).InstallVersion(data)
}

// Has reports whether the store already holds a file. The size settles it where the version JSON
// gives one: every file lands by rename with its hash checked first, so a file of the right size is
// one shulker wrote itself, and re-hashing the assets would cost most of a second launch. Without a
// size, the hash decides.
func (s Store) Has(f File) bool {
	path := s.Local(f)
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	switch {
	case f.Size > 0:
		return info.Size() == f.Size
	case f.Sha1 == "":
		return true
	}
	sum, err := fsutil.SHA1(path)
	return err == nil && strings.EqualFold(sum, f.Sha1)
}

// InstalledLoader is the version id a loader wrote into the store, by its own installer or from its
// launcher profile, remembered so a launch needs neither again. The note is the store's own
// bookkeeping: losing it costs one install or one download and nothing else.
func (s Store) InstalledLoader(key string) (string, bool) {
	data, err := os.ReadFile(s.loadersPath())
	if err != nil {
		return "", false
	}
	var installed map[string]string
	if json.Unmarshal(data, &installed) != nil {
		return "", false
	}
	id, ok := installed[key]
	return id, ok
}

// RecordLoader remembers the version id a loader wrote, keeping the notes already there.
func (s Store) RecordLoader(key, versionID string) error {
	installed := map[string]string{}
	if data, err := os.ReadFile(s.loadersPath()); err == nil {
		if json.Unmarshal(data, &installed) != nil {
			installed = map[string]string{}
		}
	}
	installed[key] = versionID
	return fsutil.WriteJSON(s.loadersPath(), installed)
}

func (s Store) loadersPath() string { return s.path("loaders.json") }
