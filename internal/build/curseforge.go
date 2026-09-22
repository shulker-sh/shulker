package build

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"maps"
	"os"
	"sort"
	"strconv"
	"strings"

	"shulker.sh/shulker/internal/cfpack"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/provider/curseforge"
)

type CurseForgeOptions struct {
	Side     string
	Version  string
	Output   string
	Bundle   bool
	OS       string
	Features map[string]bool
	// Match looks fingerprints up on CurseForge. It only runs when a mod isn't locked from CurseForge.
	Match func(fingerprints []uint32) (map[uint32]curseforge.Match, error)
	// Records looks the file-ID projects up for modlist.html.
	Records func(projectIDs []int) (map[int]curseforge.Record, error)
	// Lookalike finds the CurseForge file with the same name and size as a file
	// whose fingerprint missed, and downloads it. ok is false when there is none.
	Lookalike func(miss CurseForgeMiss) (match curseforge.Match, data []byte, ok bool, err error)
}

// CurseForgeMiss is a file CurseForge has no fingerprint for.
type CurseForgeMiss struct {
	Key       string
	Kind      string
	Provider  string
	Project   any
	Alias     int
	Filename  string
	Size      int64
	Minecraft string
	Loaders   []string
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
	filename string
	// providerFilename is the provider's own name for the file, which a
	// lookalike on CurseForge would share.
	providerFilename string
	alias            int
	loaders          []string
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
	files, names, fileNames, err := b.curseForgeMods(t, opts, report)
	if err != nil {
		return nil, err
	}
	enableByCurseForgeNames(t, fileNames, b.Manifest.OptionsPath())
	entries := map[string][]byte{}
	for path, data := range t.files {
		entries["overrides/"+path] = data
		report.Overrides = append(report.Overrides, path)
	}
	sort.Strings(report.Overrides)
	modLoaders := []cfpack.ModLoader{}
	if l, ok := loader.Lookup(b.Lock.Loader.Type); ok {
		modLoaders = append(modLoaders, cfpack.ModLoader{ID: l.CurseForgeModLoader(b.Lock.Minecraft, b.Lock.Loader.Version), Primary: true})
	}
	profile := cfpack.Manifest{
		Minecraft:       cfpack.Minecraft{Version: b.Lock.Minecraft, ModLoaders: modLoaders},
		ManifestType:    cfpack.ManifestType,
		ManifestVersion: cfpack.ManifestVersion,
		Name:            report.Name,
		Version:         opts.Version,
		Author:          strings.Join(b.Manifest.Authors, ", "),
		Files:           files,
		Overrides:       "overrides",
	}
	if b.Manifest.UsesMarker() {
		profile.Image = "profileImage/" + markerLogo
	}
	data, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return nil, err
	}
	entries[cfpack.ManifestName] = append(data, '\n')
	if profile.Image != "" {
		entries[profile.Image] = markerIcon
	}
	entries["modlist.html"] = curseForgeModlist(names, files, curseForgeRecords(files, opts, report))
	if err := b.addIdentity(entries); err != nil {
		return nil, err
	}
	if err := writeArchive(opts.Output, cfpack.ManifestName, entries); err != nil {
		return nil, err
	}
	return report, nil
}

// curseForgeMods returns the profile's files and the names that pair with them
// in the modlist, in the order the archive lists them, and the fileName
// CurseForge gives each placed path that goes by file ID.
func (b *Builder) curseForgeMods(t *mrpackSide, opts CurseForgeOptions, report *CurseForgeReport) ([]cfpack.File, []string, map[string]string, error) {
	entries := b.curseForgeEntries(t)
	byKey := map[string]cfpack.File{}
	fileNames := map[string]string{}
	byEntry := map[string]curseForgeEntry{}
	blobs := map[string][]byte{}
	var lookup []string
	var fingerprints []uint32
	for _, e := range entries {
		byEntry[e.key] = e
		if project, file, ok := curseForgeLocked(e); ok {
			byKey[e.key] = cfpack.File{ProjectID: project, FileID: file, Required: true}
			fileNames[e.path] = e.filename
			continue
		}
		data, err := os.ReadFile(b.Cache.Object(e.sha512))
		if err != nil {
			return nil, nil, nil, notInstalled(e.key)
		}
		blobs[e.key] = data
		lookup = append(lookup, e.key)
		fingerprints = append(fingerprints, curseforge.Fingerprint(data))
	}
	matches := map[uint32]curseforge.Match{}
	lookalikes := opts.Lookalike != nil
	if len(lookup) > 0 {
		found, err := opts.Match(fingerprints)
		switch {
		case err == nil:
			matches = found
		case opts.Bundle:
			lookalikes = false
			report.Warnings = append(report.Warnings, fmt.Sprintf("CurseForge lookup failed, so these are bundled: %s", out.AsError(err).Message))
		default:
			e := out.AsError(err)
			e.Items = lookup
			return nil, nil, nil, e
		}
	}
	missing := &kindTally{}
	for i, key := range lookup {
		e := byEntry[key]
		match, ok := matches[fingerprints[i]]
		if !ok && lookalikes {
			var err error
			if match, ok, err = b.curseForgeLookalike(e, blobs[key], opts.Lookalike); err != nil {
				if !opts.Bundle {
					fail := out.AsError(err)
					fail.Items = []string{key}
					return nil, nil, nil, fail
				}
				report.Warnings = append(report.Warnings, fmt.Sprintf("CurseForge lookup for %s failed, so it is bundled: %s", key, out.AsError(err).Message))
			} else if ok {
				report.Warnings = append(report.Warnings, fmt.Sprintf("%s matches CurseForge file %d by contents, but its bytes differ from the locked file", key, match.FileID))
			}
		}
		if ok {
			byKey[key] = cfpack.File{ProjectID: match.ModID, FileID: match.FileID, Required: true}
			fileNames[e.path] = match.FileName
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
		return nil, nil, nil, bundleNudge(out.Errorf("curseforge-not-found", "%s %s on CurseForge", kindCount(missing.counts), verb), missing.items, "shulker export curseforge --bundle")
	}
	files := []cfpack.File{}
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
	return files, names, fileNames, nil
}

// curseForgeLookalike accepts a CurseForge file with the locked file's name and
// size only when every zip entry unpacks to the same bytes, since the same build
// uploaded twice differs in entry timestamps.
func (b *Builder) curseForgeLookalike(e curseForgeEntry, locked []byte, lookalike func(CurseForgeMiss) (curseforge.Match, []byte, bool, error)) (curseforge.Match, bool, error) {
	if e.providerFilename == "" {
		return curseforge.Match{}, false, nil
	}
	match, data, ok, err := lookalike(CurseForgeMiss{
		Key:       e.key,
		Kind:      e.kind,
		Provider:  e.provider,
		Project:   e.project,
		Alias:     e.alias,
		Filename:  e.providerFilename,
		Size:      int64(len(locked)),
		Minecraft: b.Lock.Minecraft,
		Loaders:   e.loaders,
	})
	if err != nil || !ok {
		return curseforge.Match{}, false, err
	}
	return match, sameZipContents(locked, data), nil
}

func sameZipContents(a, b []byte) bool {
	left, err := zipContents(a)
	if err != nil {
		return false
	}
	right, err := zipContents(b)
	if err != nil {
		return false
	}
	return maps.EqualFunc(left, right, bytes.Equal)
}

func zipContents(data []byte) (map[string][]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	contents := make(map[string][]byte, len(zr.File))
	for _, f := range zr.File {
		if _, dup := contents[f.Name]; dup {
			return nil, fmt.Errorf("zip entry %s appears twice", f.Name)
		}
		r, err := f.Open()
		if err != nil {
			return nil, err
		}
		content, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			return nil, err
		}
		contents[f.Name] = content
	}
	return contents, nil
}

// enableByCurseForgeNames points the options file and the shader loader's config
// at the names the launcher saves file-ID packs under, which are CurseForge's own
// file names rather than the <key>.zip a build places.
func enableByCurseForgeNames(t *mrpackSide, fileNames map[string]string, optionsPath string) {
	renamed := map[string]string{}
	for placed, name := range fileNames {
		if base, ok := strings.CutPrefix(placed, "resourcepacks/"); ok && name != "" {
			renamed[base] = name
		}
	}
	rewriteProperty(t.files, optionsPath, resourcePacksKey+":", func(list string) string { return renamePacks(list, renamed) })
	for _, config := range shaderConfigs {
		rewriteProperty(t.files, config, "shaderPack=", func(v string) string {
			if name := fileNames["shaderpacks/"+v]; name != "" {
				return name
			}
			return v
		})
	}
}

func rewriteProperty(files map[string][]byte, path, prefix string, rewrite func(string) string) {
	data, ok := files[path]
	if !ok {
		return
	}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if v, ok := strings.CutPrefix(line, prefix); ok {
			lines[i] = prefix + rewrite(v)
		}
	}
	files[path] = []byte(strings.Join(lines, "\n"))
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
		entries = append(entries, curseForgeEntry{key: id, kind: manifest.TypeMod, path: "mods/" + m.Filename, provider: m.Provider, sha512: m.Sha512, url: m.URL, project: m.Project, version: m.Version, providerFilename: m.Filename, alias: m.Aliases.CurseForge, loaders: []string{b.Lock.Loader.Type}})
	}
	for _, ref := range b.packRefs() {
		if !t.packs[ref.key] {
			continue
		}
		var loaders []string
		if ref.pack.Loader != "" {
			loaders = []string{ref.pack.Loader}
		}
		entries = append(entries, curseForgeEntry{key: ref.key, kind: ref.kind, path: ref.path, provider: ref.pack.Provider, sha512: ref.pack.Sha512, url: ref.pack.URL, project: ref.pack.Project, version: ref.pack.Version, filename: ref.pack.ProviderFilename, providerFilename: ref.pack.ProviderFilename, loaders: loaders})
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

func curseForgeRecords(files []cfpack.File, opts CurseForgeOptions, report *CurseForgeReport) map[int]curseforge.Record {
	if len(files) == 0 || opts.Records == nil {
		return nil
	}
	ids := make([]int, len(files))
	for i, f := range files {
		ids[i] = f.ProjectID
	}
	records, err := opts.Records(ids)
	if err != nil {
		report.Warnings = append(report.Warnings, fmt.Sprintf("CurseForge project lookup failed, so modlist.html links project IDs: %s", out.AsError(err).Message))
		return nil
	}
	return records
}

// curseForgeModlist writes each line as the CurseForge app does, less its byte-order mark, and
// falls back to the project ID and shulker key for a project it has no record of.
func curseForgeModlist(names []string, files []cfpack.File, records map[int]curseforge.Record) []byte {
	var sb strings.Builder
	sb.WriteString("<ul>\n")
	for i, f := range files {
		page, text := curseforge.ProjectPage(strconv.Itoa(f.ProjectID)), names[i]
		if r, ok := records[f.ProjectID]; ok && r.WebsiteURL != "" && r.Name != "" {
			page, text = r.WebsiteURL, r.Name
			if r.Author != "" {
				text += " (by " + r.Author + ")"
			}
		}
		fmt.Fprintf(&sb, "<li><a href=\"%s\">%s</a></li>\n", html.EscapeString(page), html.EscapeString(text))
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
