package packarchive

import (
	"encoding/json"
	"fmt"
	"html"
	"path"
	"slices"
	"strconv"
	"strings"

	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/security"
)

// cfpack is the CurseForge modpack format: a manifest naming each file by CurseForge project and
// file id, a modlist beside it, and one folder of overrides for the one side the app builds.
type cfpack struct{}

const (
	cfProvider        = "curseforge"
	cfManifestName    = "manifest.json"
	cfManifestType    = "minecraftModpack"
	cfManifestVersion = 1
	cfModlistName     = "modlist.html"
	cfOverridesFolder = "overrides"
	cfImageFolder     = "profileImage"
)

type cfManifest struct {
	Minecraft       cfMinecraft `json:"minecraft"`
	ManifestType    string      `json:"manifestType"`
	ManifestVersion int         `json:"manifestVersion"`
	Name            string      `json:"name"`
	Version         string      `json:"version"`
	Author          string      `json:"author,omitempty"`
	Files           []cfFile    `json:"files"`
	Overrides       string      `json:"overrides"`
	Image           string      `json:"image,omitempty"`
}

type cfMinecraft struct {
	Version    string        `json:"version"`
	ModLoaders []cfModLoader `json:"modLoaders"`
	// RecommendedRAM is the heap in MB the CurseForge app offers for the pack.
	RecommendedRAM int `json:"recommendedRam,omitempty"`
}

type cfModLoader struct {
	ID      string `json:"id"`
	Primary bool   `json:"primary"`
}

// cfFile lists a file by CurseForge's integer ids. IsLocked is the app's per-mod "lock from
// updates" flag, which it writes as false on export.
type cfFile struct {
	ProjectID int  `json:"projectID"`
	FileID    int  `json:"fileID"`
	Required  bool `json:"required"`
	IsLocked  bool `json:"isLocked"`
}

func (cfpack) Name() string           { return cfProvider }
func (cfpack) Title() string          { return "CurseForge" }
func (cfpack) Extension() string      { return ".zip" }
func (cfpack) NamedByExtension() bool { return false }
func (cfpack) Provider() string       { return cfProvider }
func (cfpack) Sided() bool            { return false }
func (cfpack) Renames() bool          { return true }

func (cfpack) Usage() Usage {
	return Usage{
		Short:   "Export a CurseForge modpack (.zip) for the CurseForge app",
		Archive: "a CurseForge zip with a manifest.json of type minecraftModpack",
		Listed:  "by file ID",
		Bundle:  "put files that aren't on CurseForge inside the archive.",
		Bundled: "the CurseForge app will warn that it isn't on CurseForge",
	}
}

// Places turns down a datapack outside datapacks/, the one folder the app installs datapacks in.
func (cfpack) Places(kind, p string) bool {
	return kind != manifest.TypeDatapack || path.Dir(p) == "datapacks"
}

func (cfpack) Lists(f File) bool {
	_, ok := cfIDs(f)
	return ok
}

// cfIDs is the listing for a file CurseForge hosts, which the format writes by integer ids; ok is
// false for a file from elsewhere or with ids that aren't integers.
func cfIDs(f File) (cfFile, bool) {
	if f.Provider != cfProvider {
		return cfFile{}, false
	}
	p, err := strconv.Atoi(f.Project)
	v, err2 := strconv.Atoi(f.Version)
	if err != nil || err2 != nil {
		return cfFile{}, false
	}
	return cfFile{ProjectID: p, FileID: v, Required: true}, true
}

func (cfpack) CantPlace(what string) *out.Error {
	return out.Errorf("curseforge-cant-place", "%s can't go in by file ID: the CurseForge app installs datapacks in datapacks/, and this build places them elsewhere", what)
}

func (cfpack) NotListed(what string, n int) *out.Error {
	verb := "aren't"
	if n == 1 {
		verb = "isn't"
	}
	return out.Errorf("curseforge-not-found", "%s %s on CurseForge", what, verb)
}

// cfModLoaderID is the loader id a pack manifest names for a loader version, carrying the game
// version where the row says CurseForge needs it to tell builds apart.
func cfModLoaderID(loaderName, minecraft, version string) string {
	l, _ := loader.Lookup(loaderName)
	if slices.Contains(l.CurseForgeGameInID, minecraft) {
		return loaderName + "-" + minecraft + "-" + version
	}
	return loaderName + "-" + version
}

// cfParseModLoaderID reads a loader name and version from a pack manifest's loader id, the
// inverse of cfModLoaderID.
func cfParseModLoaderID(id string) (string, string, bool) {
	name, version, ok := strings.Cut(id, "-")
	if !ok || version == "" {
		return "", "", false
	}
	l, known := loader.Lookup(name)
	if !known {
		return "", "", false
	}
	for _, game := range l.CurseForgeGameInID {
		version = strings.TrimPrefix(version, game+"-")
	}
	return name, version, true
}

// detect takes a manifest.json of the modpack type as the format's own, so a zip that merely
// carries some other manifest.json is passed over by what it holds, not by its name. One that
// names the type but doesn't parse is taken too, for decode to refuse as malformed.
func (cfpack) detect(z *zipEntries) bool {
	data, err := z.read(cfManifestName)
	if err != nil || data == nil {
		return false
	}
	var probe struct {
		ManifestType string `json:"manifestType"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return strings.Contains(string(data), `"manifestType"`)
	}
	return probe.ManifestType == cfManifestType
}

func (cfpack) decode(file string, z *zipEntries) (*Archive, error) {
	if raw := z.unsafe(); raw != "" {
		return nil, security.Refusal(security.Paths, out.Errorf("curseforge-invalid", "%s contains an unsafe entry %q", file, raw))
	}
	data, err := z.read(cfManifestName)
	if err != nil {
		return nil, err
	}
	var m cfManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, out.Errorf("curseforge-invalid", "shulker can't parse %s in %s", cfManifestName, file).WithCause("json", err)
	}
	if m.ManifestVersion != cfManifestVersion {
		return nil, out.Errorf("curseforge-invalid", "%s is manifest version %d; shulker reads version %d", file, m.ManifestVersion, cfManifestVersion)
	}
	if m.Minecraft.Version == "" {
		return nil, out.Errorf("curseforge-invalid", "%s names no minecraft version", file)
	}
	a := &Archive{Name: m.Name, Version: m.Version, Minecraft: m.Minecraft.Version, Memory: cfHeap(m.Minecraft.RecommendedRAM), Files: []File{}}
	if m.Author != "" {
		a.Authors = []string{m.Author}
	}
	if a.Loader, err = cfLoader(m.Minecraft.ModLoaders); err != nil {
		return nil, err
	}
	for _, f := range m.Files {
		a.Files = append(a.Files, File{Provider: cfProvider, Project: strconv.Itoa(f.ProjectID), Version: strconv.Itoa(f.FileID), Optional: !f.Required})
	}
	if a.Marker, err = z.rootIdentity(file); err != nil {
		return nil, err
	}
	if m.Image != "" {
		if a.Icon, err = z.read(path.Clean(strings.ReplaceAll(m.Image, "\\", "/"))); err != nil {
			return nil, err
		}
	}
	folder := cfOverridesFolder
	if m.Overrides != "" {
		folder = strings.Trim(path.Clean(strings.ReplaceAll(m.Overrides, "\\", "/")), "/")
	}
	if a.Overrides, _, err = z.overridesUnder(folder, LayerFor("both")); err != nil {
		return nil, err
	}
	return a, nil
}

// cfHeap is a recommendedRam in MB as a heap size: whole gigabytes where it is one, else megabytes.
func cfHeap(mb int) string {
	switch {
	case mb <= 0:
		return ""
	case mb%1024 == 0:
		return strconv.Itoa(mb/1024) + "G"
	}
	return strconv.Itoa(mb) + "M"
}

// cfLoader is the pack's primary mod loader, or its first when none is marked primary. A pack for
// vanilla Minecraft names none.
func cfLoader(loaders []cfModLoader) (Loader, error) {
	if len(loaders) == 0 {
		return Loader{}, nil
	}
	id := loaders[0].ID
	for _, l := range loaders {
		if l.Primary {
			id = l.ID
			break
		}
	}
	name, version, ok := cfParseModLoaderID(id)
	if !ok {
		return Loader{}, out.Errorf("unsupported-loader", "shulker doesn't support the pack's loader %s", id)
	}
	return Loader{Type: name, Version: version}, nil
}

func (cfpack) encode(x *Export) (map[string][]byte, string, error) {
	files := make([]cfFile, 0, len(x.Files))
	for _, f := range x.Files {
		listed, ok := cfIDs(f)
		if !ok {
			return nil, "", fmt.Errorf("%s is not a CurseForge file", f.Path)
		}
		files = append(files, listed)
	}
	modLoaders := []cfModLoader{}
	if x.Loader.Type != "" {
		modLoaders = append(modLoaders, cfModLoader{ID: cfModLoaderID(x.Loader.Type, x.Minecraft, x.Loader.Version), Primary: true})
	}
	m := cfManifest{
		Minecraft:       cfMinecraft{Version: x.Minecraft, ModLoaders: modLoaders},
		ManifestType:    cfManifestType,
		ManifestVersion: cfManifestVersion,
		Name:            x.Name,
		Version:         x.Version,
		Author:          strings.Join(x.Authors, ", "),
		Files:           files,
		Overrides:       cfOverridesFolder,
	}
	entries := map[string][]byte{}
	for _, o := range x.Overrides {
		entries[cfOverridesFolder+"/"+o.Path] = o.Data
	}
	if x.Icon != nil {
		m.Image = cfImageFolder + "/" + x.IconName
		entries[m.Image] = x.Icon
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, "", err
	}
	entries[cfManifestName] = append(data, '\n')
	entries[cfModlistName] = cfModlist(x.Files)
	if err := x.addIdentity(entries); err != nil {
		return nil, "", err
	}
	return entries, cfManifestName, nil
}

// cfModlist writes each line as the CurseForge app does, less its byte-order mark: the project's
// title and first author, linking its page.
func cfModlist(files []File) []byte {
	var sb strings.Builder
	sb.WriteString("<ul>\n")
	for _, f := range files {
		text := f.Title
		if f.Author != "" {
			text += " (by " + f.Author + ")"
		}
		fmt.Fprintf(&sb, "<li><a href=\"%s\">%s</a></li>\n", html.EscapeString(f.Page), html.EscapeString(text))
	}
	sb.WriteString("</ul>\n")
	return []byte(sb.String())
}
