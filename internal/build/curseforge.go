package build

import (
	"encoding/json"
	"fmt"
	"html"
	"os"
	"sort"
	"strconv"
	"strings"

	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/provider/curseforge"
)

const curseForgeManifestName = "manifest.json"

type CurseForgeOptions struct {
	Side     string
	Version  string
	Output   string
	Bundle   bool
	OS       string
	Features map[string]bool
	// Match looks fingerprints up on CurseForge. It only runs when a mod isn't locked from CurseForge.
	Match func(fingerprints []uint32) (map[uint32]curseforge.Match, error)
}

type CurseForgeReport struct {
	Path                 string   `json:"path"`
	Version              string   `json:"version"`
	Name                 string   `json:"name"`
	Side                 string   `json:"side"`
	Mods                 []string `json:"mods"`
	ResourcePacks        []string `json:"resourcepacks"`
	Shaders              []string `json:"shaders"`
	Matched              []string `json:"matched"`
	BundledMods          []string `json:"bundledMods"`
	BundledResourcePacks []string `json:"bundledResourcepacks"`
	BundledShaders       []string `json:"bundledShaders"`
	Overrides            []string `json:"overrides"`
	Warnings             []string `json:"-"`
}

// curseForgeEntry is one file the export ships, whatever kind it is, so mods and
// packs go through the same lookup and bundling.
type curseForgeEntry struct {
	key      string
	kind     string
	path     string
	provider string
	sha512   string
	url      *string
	project  any
	version  any
}

type curseForgeManifest struct {
	Minecraft       curseForgeMinecraft `json:"minecraft"`
	ManifestType    string              `json:"manifestType"`
	ManifestVersion int                 `json:"manifestVersion"`
	Name            string              `json:"name"`
	Version         string              `json:"version"`
	Author          string              `json:"author,omitempty"`
	Files           []curseForgeFile    `json:"files"`
	Overrides       string              `json:"overrides"`
	Image           string              `json:"image"`
}

type curseForgeMinecraft struct {
	Version    string                `json:"version"`
	ModLoaders []curseForgeModLoader `json:"modLoaders"`
}

type curseForgeModLoader struct {
	ID      string `json:"id"`
	Primary bool   `json:"primary"`
}

type curseForgeFile struct {
	ProjectID int  `json:"projectID"`
	FileID    int  `json:"fileID"`
	Required  bool `json:"required"`
}

// ExportCurseForge writes the project as a CurseForge modpack.
func (b *Builder) ExportCurseForge(opts CurseForgeOptions) (*CurseForgeReport, error) {
	side := opts.Side
	if side == "" {
		side = "client"
	}
	sides, err := b.mrpackSides([]string{side})
	if err != nil {
		return nil, err
	}
	t := sides[0]
	report := &CurseForgeReport{Path: opts.Output, Version: opts.Version, Name: b.mrpackName(sides), Side: t.side, Mods: []string{}, ResourcePacks: []string{}, Shaders: []string{}, Matched: []string{}, BundledMods: []string{}, BundledResourcePacks: []string{}, BundledShaders: []string{}, Overrides: []string{}}
	if report.Warnings, err = b.mrpackCollect(t, opts.OS, opts.Features); err != nil {
		return nil, err
	}
	files, names, err := b.curseForgeMods(t, opts, report)
	if err != nil {
		return nil, err
	}
	entries := map[string][]byte{}
	for path, data := range t.files {
		entries["overrides/"+path] = data
		report.Overrides = append(report.Overrides, path)
	}
	sort.Strings(report.Overrides)
	modLoaders := []curseForgeModLoader{}
	if l, ok := loader.Lookup(b.Lock.Loader.Type); ok {
		modLoaders = append(modLoaders, curseForgeModLoader{ID: l.CurseForgeModLoader(b.Lock.Minecraft, b.Lock.Loader.Version), Primary: true})
	}
	profile := curseForgeManifest{
		Minecraft:       curseForgeMinecraft{Version: b.Lock.Minecraft, ModLoaders: modLoaders},
		ManifestType:    "minecraftModpack",
		ManifestVersion: 1,
		Name:            report.Name,
		Version:         opts.Version,
		Author:          strings.Join(b.Manifest.Authors, ", "),
		Files:           files,
		Overrides:       "overrides",
		Image:           "profileImage/" + markerLogo,
	}
	data, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return nil, err
	}
	entries[curseForgeManifestName] = append(data, '\n')
	entries[profile.Image] = markerIcon
	entries["modlist.html"] = curseForgeModlist(names, files)
	if err := b.addIdentity(entries); err != nil {
		return nil, err
	}
	if err := writeArchive(opts.Output, curseForgeManifestName, entries); err != nil {
		return nil, err
	}
	return report, nil
}

// curseForgeMods returns the profile's files and the names that pair with them
// in the modlist, in the order the archive lists them.
func (b *Builder) curseForgeMods(t *mrpackSide, opts CurseForgeOptions, report *CurseForgeReport) ([]curseForgeFile, []string, error) {
	entries := b.curseForgeEntries(t)
	byKey := map[string]curseForgeFile{}
	byEntry := map[string]curseForgeEntry{}
	blobs := map[string][]byte{}
	var lookup []string
	var fingerprints []uint32
	for _, e := range entries {
		byEntry[e.key] = e
		if project, file, ok := curseForgeLocked(e); ok {
			byKey[e.key] = curseForgeFile{ProjectID: project, FileID: file, Required: true}
			continue
		}
		data, err := os.ReadFile(b.Cache.Object(e.sha512))
		if err != nil {
			return nil, nil, notInstalled(e.key)
		}
		blobs[e.key] = data
		lookup = append(lookup, e.key)
		fingerprints = append(fingerprints, curseforge.Fingerprint(data))
	}
	matches := map[uint32]curseforge.Match{}
	if len(lookup) > 0 {
		found, err := opts.Match(fingerprints)
		switch {
		case err == nil:
			matches = found
		case opts.Bundle:
			report.Warnings = append(report.Warnings, fmt.Sprintf("CurseForge lookup failed, so these are bundled: %s", out.AsError(err).Message))
		default:
			e := out.AsError(err)
			e.Items = lookup
			return nil, nil, e
		}
	}
	missing := &kindTally{}
	for i, key := range lookup {
		e := byEntry[key]
		if match, ok := matches[fingerprints[i]]; ok {
			byKey[key] = curseForgeFile{ProjectID: match.ModID, FileID: match.FileID, Required: true}
			report.Matched = append(report.Matched, key)
			continue
		}
		origin := mrpackOrigin(e.provider, e.url)
		if !opts.Bundle {
			missing.add(e.kind, key+" ("+origin+")")
			continue
		}
		t.files[e.path] = blobs[key]
		bundled := reportList(report, e.kind, true)
		*bundled = append(*bundled, key)
		report.Warnings = append(report.Warnings, fmt.Sprintf("bundled %s from %s into the archive; the CurseForge app will warn that it isn't on CurseForge", key, origin))
	}
	if missing.total() > 0 {
		verb := "aren't"
		if missing.total() == 1 {
			verb = "isn't"
		}
		return nil, nil, bundleNudge(out.Errorf("curseforge-not-found", "%s %s on CurseForge", kindCount(missing.counts), verb), missing.items, "shulker export curseforge --bundle")
	}
	files := []curseForgeFile{}
	names := []string{}
	for _, e := range entries {
		f, ok := byKey[e.key]
		if !ok {
			continue
		}
		files = append(files, f)
		names = append(names, e.key)
		locked := reportList(report, e.kind, false)
		*locked = append(*locked, e.key)
	}
	return files, names, nil
}

func reportList(report *CurseForgeReport, kind string, bundled bool) *[]string {
	switch {
	case kind == manifest.TypeResourcePack && bundled:
		return &report.BundledResourcePacks
	case kind == manifest.TypeResourcePack:
		return &report.ResourcePacks
	case kind == manifest.TypeShader && bundled:
		return &report.BundledShaders
	case kind == manifest.TypeShader:
		return &report.Shaders
	case bundled:
		return &report.BundledMods
	}
	return &report.Mods
}

func (b *Builder) curseForgeEntries(t *mrpackSide) []curseForgeEntry {
	ids := make([]string, 0, len(t.mods))
	for id := range t.mods {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	entries := make([]curseForgeEntry, 0, len(ids)+len(t.packs))
	for _, id := range ids {
		m := b.Lock.Mods[id]
		entries = append(entries, curseForgeEntry{key: id, kind: manifest.TypeMod, path: "mods/" + m.Filename, provider: m.Provider, sha512: m.Sha512, url: m.URL, project: m.Project, version: m.Version})
	}
	for _, ref := range b.packRefs() {
		if !t.packs[ref.key] {
			continue
		}
		entries = append(entries, curseForgeEntry{key: ref.key, kind: ref.kind, path: ref.path, provider: ref.pack.Provider, sha512: ref.pack.Sha512, url: ref.pack.URL, project: ref.pack.Project, version: ref.pack.Version})
	}
	return entries
}

func curseForgeLocked(e curseForgeEntry) (project, file int, ok bool) {
	if e.provider != "curseforge" {
		return 0, 0, false
	}
	project, okProject := lockInt(e.project)
	file, okFile := lockInt(e.version)
	return project, file, okProject && okFile
}

func lockInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case json.Number:
		i, err := strconv.Atoi(n.String())
		return i, err == nil
	case float64:
		return int(n), n == float64(int(n))
	}
	return 0, false
}

func curseForgeModlist(names []string, files []curseForgeFile) []byte {
	var sb strings.Builder
	sb.WriteString("<ul>\n")
	for i, f := range files {
		page := curseforge.ProjectPage(strconv.Itoa(f.ProjectID))
		fmt.Fprintf(&sb, "<li><a href=\"%s\">%s</a></li>\n", html.EscapeString(page), html.EscapeString(names[i]))
	}
	sb.WriteString("</ul>\n")
	return []byte(sb.String())
}

// bundleNudge lists the mods an export can't point at and suggests shipping them in the archive.
func bundleNudge(e *out.Error, mods []string, command string) *out.Error {
	e.Items = mods
	lead := "Ship them inside the archive instead"
	if len(mods) == 1 {
		lead = "Ship it inside the archive instead"
	}
	e.Nudge = out.Nudge{Lead: lead, Command: command}
	return e
}
