package packarchive

import (
	"encoding/json"
	"net/url"
	"slices"
	"sort"

	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/security"
)

// mrpack is the Modrinth modpack format: an index naming each file by path, hashes, download
// URLs and the sides it is for, and three override folders, one per side and one shared.
type mrpack struct{}

const (
	mrpackIndexName = "modrinth.index.json"
	// mrpackIconName is the root entry the Modrinth App takes as the instance icon; the format
	// itself has no icon.
	mrpackIconName     = "icon.png"
	mrpackFormat       = 1
	mrpackGame         = "minecraft"
	mrpackMinecraftKey = "minecraft"
)

// MrpackHosts are the hosts a Modrinth launcher downloads a pack's files from. A file hosted
// anywhere else has to be bundled.
var MrpackHosts = []string{"cdn.modrinth.com", "github.com", "raw.githubusercontent.com", "gitlab.com"}

type mrpackIndex struct {
	FormatVersion int               `json:"formatVersion"`
	Game          string            `json:"game"`
	VersionID     string            `json:"versionId"`
	Name          string            `json:"name"`
	Summary       string            `json:"summary,omitempty"`
	Files         []mrpackFile      `json:"files"`
	Dependencies  map[string]string `json:"dependencies"`
}

type mrpackFile struct {
	Path      string            `json:"path"`
	Hashes    map[string]string `json:"hashes"`
	Env       map[string]string `json:"env"`
	Downloads []string          `json:"downloads"`
	FileSize  int64             `json:"fileSize"`
}

func (mrpack) Name() string           { return "mrpack" }
func (mrpack) Title() string          { return "Modrinth" }
func (mrpack) Extension() string      { return ".mrpack" }
func (mrpack) NamedByExtension() bool { return true }
func (mrpack) Provider() string       { return "" }
func (mrpack) Sided() bool            { return true }
func (mrpack) Renames() bool          { return false }

func (mrpack) Usage() Usage {
	return Usage{
		Short:   "Export a Modrinth modpack (.mrpack) for the Modrinth app and other launchers",
		Archive: "a .mrpack",
		Listed:  "by download",
		Bundle:  "put files that Modrinth launchers cannot download inside the archive.",
		Bundled: "recipients receive the file itself, not a download link",
	}
}

func (mrpack) Places(kind, path string) bool { return true }

func (mrpack) Lists(f File) bool {
	return len(f.Downloads) > 0 && mrpackHosted(f.Downloads[0]) == ""
}

// mrpackHosted is where a download comes from when it isn't https on one of MrpackHosts, or empty
// when a Modrinth launcher would download it.
func mrpackHosted(download string) string {
	parsed, err := url.Parse(download)
	if err != nil {
		return download
	}
	if parsed.Scheme == "https" && slices.Contains(MrpackHosts, parsed.Hostname()) {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}

func (mrpack) CantPlace(what string) *out.Error {
	return out.Errorf("mrpack-invalid", "%s can't be placed by a Modrinth launcher", what)
}

func (mrpack) NotListed(what string, n int) *out.Error {
	return out.Errorf("mrpack-host-not-allowed", "%s can't be downloaded by Modrinth launchers", what)
}

func (mrpack) detect(z *zipEntries) bool { return z.has(mrpackIndexName) }

func (mrpack) decode(file string, z *zipEntries) (*Archive, error) {
	if raw := z.unsafe(); raw != "" {
		return nil, security.Refusal(security.Paths, out.Errorf("mrpack-invalid", "%s contains an unsafe entry %q", file, raw))
	}
	data, err := z.read(mrpackIndexName)
	if err != nil {
		return nil, err
	}
	var index mrpackIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, out.Errorf("mrpack-invalid", "shulker can't parse %s in %s", mrpackIndexName, file).WithCause("json", err)
	}
	if index.FormatVersion != mrpackFormat || index.Game != mrpackGame {
		return nil, out.Errorf("mrpack-unsupported", "%s is format %d for %q; shulker reads format %d for %q", file, index.FormatVersion, index.Game, mrpackFormat, mrpackGame)
	}
	if index.Dependencies[mrpackMinecraftKey] == "" {
		return nil, out.Errorf("mrpack-invalid", "%s: the index has no minecraft dependency", file)
	}
	a := &Archive{Name: index.Name, Version: index.VersionID, Summary: index.Summary, Minecraft: index.Dependencies[mrpackMinecraftKey], Files: []File{}}
	for _, l := range loader.All {
		if v, ok := index.Dependencies[l.MrpackKey]; ok {
			a.Loader = Loader{Type: l.Name, Version: v}
			break
		}
	}
	for _, f := range index.Files {
		if f.Hashes["sha512"] == "" || f.Hashes["sha1"] == "" || len(f.Downloads) == 0 {
			return nil, out.Errorf("mrpack-invalid", "index file %s lacks sha1, sha512, or a download url", f.Path)
		}
		if !fsutil.IsPortableLocal(f.Path) {
			return nil, security.Refusal(security.Paths, out.Errorf("mrpack-invalid", "index file %s is outside the pack's folder", f.Path))
		}
		for _, d := range f.Downloads {
			if from := mrpackHosted(d); from != "" {
				return nil, security.Refusal(security.MrpackHosts, out.Errorf("mrpack-invalid", "index file %s downloads from %s, which Modrinth launchers don't", f.Path, from))
			}
		}
		a.Files = append(a.Files, File{Path: f.Path, Hashes: f.Hashes, Side: mrpackSide(f.Env), Downloads: f.Downloads, Size: f.FileSize})
	}
	if a.Icon, err = z.read(mrpackIconName); err != nil {
		return nil, err
	}
	var jar *Marker
	for _, layer := range Layers {
		overrides, m, err := z.overridesUnder(layer, layer)
		if err != nil {
			return nil, err
		}
		if m != nil && jar != nil {
			return nil, out.Errorf("mrpack-invalid", "two shulker marker jars: %s/%s and %s/%s", jar.Layer, jar.Path, m.Layer, m.Path)
		}
		if m != nil {
			jar = m
		}
		a.Overrides = append(a.Overrides, overrides...)
	}
	sort.Slice(a.Overrides, func(i, j int) bool {
		if a.Overrides[i].Layer != a.Overrides[j].Layer {
			return a.Overrides[i].Layer < a.Overrides[j].Layer
		}
		return a.Overrides[i].Path < a.Overrides[j].Path
	})
	a.Marker = jar
	root, err := z.rootIdentity(file)
	if err != nil {
		return nil, err
	}
	if root != nil {
		a.Marker = root
	}
	return a, nil
}

// mrpackSide is the side an index file's env needs it on: client, server, or both when the env
// names both, and empty when the index gives no env.
func mrpackSide(env map[string]string) string {
	switch {
	case len(env) == 0:
		return ""
	case env["server"] == "unsupported":
		return "client"
	case env["client"] == "unsupported":
		return "server"
	}
	return "both"
}

// mrpackEnv is the index env for a file needed on side: "client", "server", or anything else for
// both.
func mrpackEnv(side string) map[string]string {
	env := map[string]string{"client": "required", "server": "required"}
	switch side {
	case "client":
		env["server"] = "unsupported"
	case "server":
		env["client"] = "unsupported"
	}
	return env
}

func (mrpack) encode(x *Export) (map[string][]byte, string, error) {
	dependencies := map[string]string{mrpackMinecraftKey: x.Minecraft}
	if l, ok := loader.Lookup(x.Loader.Type); ok {
		dependencies[l.MrpackKey] = x.Loader.Version
	}
	files := make([]mrpackFile, 0, len(x.Files))
	for _, f := range x.Files {
		files = append(files, mrpackFile{Path: f.Path, Hashes: f.Hashes, Env: mrpackEnv(f.Side), Downloads: f.Downloads, FileSize: f.Size})
	}
	index := mrpackIndex{FormatVersion: mrpackFormat, Game: mrpackGame, VersionID: x.Version, Name: x.Name, Summary: x.Summary, Files: files, Dependencies: dependencies}
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return nil, "", err
	}
	entries := map[string][]byte{mrpackIndexName: append(data, '\n')}
	for _, o := range x.Overrides {
		entries[o.Layer+"/"+o.Path] = o.Data
	}
	if x.Icon != nil {
		entries[mrpackIconName] = x.Icon
	}
	if err := x.addIdentity(entries); err != nil {
		return nil, "", err
	}
	return entries, mrpackIndexName, nil
}
