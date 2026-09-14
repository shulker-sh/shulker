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
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/provider/curseforge"
)

const curseForgeManifestName = "manifest.json"

type CurseForgeOptions struct {
	Target   string
	Version  string
	Output   string
	Bundle   bool
	OS       string
	Features map[string]bool
	// Match looks fingerprints up on CurseForge. It only runs when a mod isn't locked from CurseForge.
	Match func(fingerprints []uint32) (map[uint32]curseforge.Match, error)
}

type CurseForgeReport struct {
	Path      string   `json:"path"`
	Version   string   `json:"version"`
	Name      string   `json:"name"`
	Target    string   `json:"target"`
	Mods      []string `json:"mods"`
	Matched   []string `json:"matched"`
	Bundled   []string `json:"bundled"`
	Overrides []string `json:"overrides"`
	Warnings  []string `json:"-"`
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

func (b *Builder) ExportCurseForge(opts CurseForgeOptions) (*CurseForgeReport, error) {
	targets, err := b.mrpackTargets([]string{opts.Target})
	if err != nil {
		return nil, err
	}
	t := targets[0]
	report := &CurseForgeReport{Path: opts.Output, Version: opts.Version, Name: b.mrpackName(targets), Target: t.name, Mods: []string{}, Matched: []string{}, Bundled: []string{}, Overrides: []string{}}
	if report.Warnings, err = b.mrpackCollect(t, opts.OS, opts.Features); err != nil {
		return nil, err
	}
	files, err := b.curseForgeMods(t, opts, report)
	if err != nil {
		return nil, err
	}
	entries := map[string][]byte{}
	for path, data := range t.files {
		entries["overrides/"+path] = data
		report.Overrides = append(report.Overrides, path)
	}
	sort.Strings(report.Overrides)
	l, _ := loader.Lookup(b.Lock.Loader.Type)
	manifest := curseForgeManifest{
		Minecraft:       curseForgeMinecraft{Version: b.Lock.Minecraft, ModLoaders: []curseForgeModLoader{{ID: l.CurseForgeModLoader(b.Lock.Minecraft, b.Lock.Loader.Version), Primary: true}}},
		ManifestType:    "minecraftModpack",
		ManifestVersion: 1,
		Name:            report.Name,
		Version:         opts.Version,
		Author:          strings.Join(b.Manifest.Authors, ", "),
		Files:           files,
		Overrides:       "overrides",
		Image:           "profileImage/" + markerLogo,
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	entries[curseForgeManifestName] = append(data, '\n')
	entries[manifest.Image] = markerIcon
	entries["modlist.html"] = curseForgeModlist(report.Mods, files)
	if err := writeArchive(opts.Output, curseForgeManifestName, entries); err != nil {
		return nil, err
	}
	return report, nil
}

func (b *Builder) curseForgeMods(t *mrpackTarget, opts CurseForgeOptions, report *CurseForgeReport) ([]curseForgeFile, error) {
	ids := make([]string, 0, len(t.mods))
	for id := range t.mods {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	byMod := map[string]curseForgeFile{}
	jars := map[string][]byte{}
	var lookup []string
	var fingerprints []uint32
	for _, id := range ids {
		m := b.Lock.Mods[id]
		if project, file, ok := curseForgeLocked(m); ok {
			byMod[id] = curseForgeFile{ProjectID: project, FileID: file, Required: true}
			continue
		}
		data, err := os.ReadFile(b.Cache.Object(m.Sha512))
		if err != nil {
			return nil, out.Errorf("not-installed", "%s is not in the cache; run `shulker install`", id)
		}
		jars[id] = data
		lookup = append(lookup, id)
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
			return nil, e
		}
	}
	var missing []string
	for i, id := range lookup {
		m := b.Lock.Mods[id]
		if match, ok := matches[fingerprints[i]]; ok {
			byMod[id] = curseForgeFile{ProjectID: match.ModID, FileID: match.FileID, Required: true}
			report.Matched = append(report.Matched, id)
			continue
		}
		origin := mrpackOrigin(m.Provider, m.URL)
		if !opts.Bundle {
			missing = append(missing, id+" ("+origin+")")
			continue
		}
		t.files["mods/"+m.Filename] = jars[id]
		report.Bundled = append(report.Bundled, id)
		report.Warnings = append(report.Warnings, fmt.Sprintf("bundled %s from %s into the archive; the CurseForge app will warn that it isn't on CurseForge", id, origin))
	}
	if len(missing) > 0 {
		return nil, bundleNudge(out.Errorf("curseforge-not-found", "%s on CurseForge", modCount(len(missing), "isn't", "aren't")), missing, "shulker export curseforge --bundle")
	}
	files := []curseForgeFile{}
	for _, id := range ids {
		if f, ok := byMod[id]; ok {
			files = append(files, f)
			report.Mods = append(report.Mods, id)
		}
	}
	return files, nil
}

func curseForgeLocked(m lock.Mod) (project, file int, ok bool) {
	if m.Provider != "curseforge" {
		return 0, 0, false
	}
	project, okProject := lockInt(m.Project)
	file, okFile := lockInt(m.Version)
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

func curseForgeModlist(mods []string, files []curseForgeFile) []byte {
	var sb strings.Builder
	sb.WriteString("<ul>\n")
	for i, f := range files {
		page := curseforge.ProjectPage(strconv.Itoa(f.ProjectID))
		fmt.Fprintf(&sb, "<li><a href=\"%s\">%s</a></li>\n", html.EscapeString(page), html.EscapeString(mods[i]))
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

func modCount(n int, one, many string) string {
	if n == 1 {
		return "1 mod " + one
	}
	return fmt.Sprintf("%d mods %s", n, many)
}
