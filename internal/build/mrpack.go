package build

import (
	"archive/zip"
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/mrpack"
	"shulker.sh/shulker/internal/out"
)

// MrpackHosts are the hosts a Modrinth launcher downloads a pack's files from. A file hosted
// anywhere else has to be bundled.
var MrpackHosts = []string{"cdn.modrinth.com", "github.com", "raw.githubusercontent.com", "gitlab.com"}

type MrpackOptions struct {
	Sides     []string
	VersionID string
	Output    string
	Bundle    bool
	OS        string
	Features  map[string]bool
}

type MrpackReport struct {
	Path                 string   `json:"path"`
	VersionID            string   `json:"versionId"`
	Name                 string   `json:"name"`
	Sides                []string `json:"sides"`
	Mods                 []string `json:"mods"`
	ResourcePacks        []string `json:"resourcepacks"`
	Shaders              []string `json:"shaders"`
	BundledMods          []string `json:"bundledMods"`
	BundledResourcePacks []string `json:"bundledResourcepacks"`
	BundledShaders       []string `json:"bundledShaders"`
	Overrides            []string `json:"overrides"`
	Warnings             []string `json:"-"`
}

type mrpackSide struct {
	side  string
	files map[string][]byte
	mods  map[string]bool
	packs map[string]bool
}

// ExportMrpack writes the project as a Modrinth modpack.
func (b *Builder) ExportMrpack(opts MrpackOptions) (*MrpackReport, error) {
	sides, err := b.mrpackSides(opts.Sides)
	if err != nil {
		return nil, err
	}
	report := &MrpackReport{Path: opts.Output, VersionID: opts.VersionID, Name: b.mrpackName(sides), Sides: []string{}, Mods: []string{}, ResourcePacks: []string{}, Shaders: []string{}, BundledMods: []string{}, BundledResourcePacks: []string{}, BundledShaders: []string{}, Overrides: []string{}, Warnings: []string{}}
	for _, t := range sides {
		report.Sides = append(report.Sides, t.side)
		warnings, err := b.mrpackCollect(t, opts.OS, opts.Features)
		if err != nil {
			return nil, err
		}
		report.Warnings = append(report.Warnings, warnings...)
	}
	files, bundled, err := b.mrpackMods(sides, opts.Bundle, report)
	if err != nil {
		return nil, err
	}
	entries := mrpackSplit(sides)
	for path := range entries {
		if bundled[path] {
			continue
		}
		report.Overrides = append(report.Overrides, path)
	}
	sort.Strings(report.Overrides)
	dependencies := map[string]string{"minecraft": b.Lock.Minecraft}
	if l, ok := loader.Lookup(b.Lock.Loader.Type); ok {
		dependencies[l.MrpackKey] = b.Lock.Loader.Version
	}
	index := mrpack.Index{
		FormatVersion: mrpack.FormatVersion,
		Game:          mrpack.Game,
		VersionID:     opts.VersionID,
		Name:          report.Name,
		Summary:       mrpackSummary(b.Manifest),
		Files:         files,
		Dependencies:  dependencies,
	}
	indexData, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return nil, err
	}
	entries[mrpack.IndexName] = append(indexData, '\n')
	if err := b.addIdentity(entries); err != nil {
		return nil, err
	}
	if err := writeArchive(opts.Output, mrpack.IndexName, entries); err != nil {
		return nil, err
	}
	return report, nil
}

// addIdentity puts the project's own manifest and lock at the archive root, so an
// export imports back as the project it came from. The marker jar carries them
// too, but only a client-side export of a project with a loader has one.
func (b *Builder) addIdentity(entries map[string][]byte) error {
	manifestData, err := os.ReadFile(filepath.Join(b.Dir, manifest.FileName))
	if err != nil {
		return err
	}
	lockData, err := os.ReadFile(b.LockPath)
	if err != nil {
		return err
	}
	entries[manifest.FileName] = manifestData
	entries[lock.FileName] = lockData
	return nil
}

func (b *Builder) mrpackSides(names []string) ([]*mrpackSide, error) {
	if len(names) == 0 {
		names = b.Manifest.Sides()
	}
	var sides []*mrpackSide
	seen := map[string]bool{}
	for _, name := range names {
		if !manifest.IsSide(name) {
			return nil, manifest.NotASide(name)
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		sides = append(sides, &mrpackSide{side: name, files: map[string][]byte{}})
	}
	return sides, nil
}

func (b *Builder) mrpackName(sides []*mrpackSide) string {
	pick := sides[0].side
	for _, t := range sides {
		if t.side == "client" {
			pick = t.side
		}
	}
	return b.Manifest.DisplayName(pick)
}

// mrpackCollect fills a side's override files and the mods it ships, returning the warnings.
func (b *Builder) mrpackCollect(t *mrpackSide, osName string, features map[string]bool) ([]string, error) {
	rep := &Report{}
	desired, _, err := b.collect(t.side, Options{OS: osName, NoOS: osName == "", Features: features}, rep)
	if err != nil {
		return nil, err
	}
	warnings := append([]string{}, rep.Warnings...)
	t.mods = map[string]bool{}
	for id, m := range b.Lock.Mods {
		if _, ok := desired["mods/"+m.Filename]; ok {
			t.mods[id] = true
		}
	}
	t.packs = map[string]bool{}
	for _, ref := range b.packRefs() {
		if _, ok := desired[ref.path]; ok {
			t.packs[ref.key] = true
		}
	}
	for _, e := range rep.Excluded {
		if strings.Contains(e, "(needs os ") {
			warnings = append(warnings, fmt.Sprintf("%s: left out of %s; pass --os to export that variation", e, t.side))
		}
	}
	for path, s := range desired {
		if s.sha512 != "" {
			continue
		}
		if s.owned != nil {
			data, err := s.owned.render(nil, nil, nil)
			if err != nil {
				return nil, err
			}
			t.files[path] = data
			continue
		}
		t.files[path] = s.data
	}
	return warnings, nil
}

func (b *Builder) mrpackMods(sides []*mrpackSide, bundle bool, report *MrpackReport) ([]mrpack.File, map[string]bool, error) {
	files := []mrpack.File{}
	bundledPaths := map[string]bool{}
	blocked := &kindTally{}
	add := func(key, kind, filePath, side, provider, sum512 string, u *string, owners []*mrpackSide, locked, bundled *[]string) error {
		data, err := os.ReadFile(b.Cache.Object(sum512))
		if err != nil {
			return notInstalled(key)
		}
		if u != nil && mrpackHostAllowed(*u) {
			sum := sha1.Sum(data)
			files = append(files, mrpack.File{
				Path:      filePath,
				Hashes:    map[string]string{"sha1": hex.EncodeToString(sum[:]), "sha512": sum512},
				Env:       mrpack.Env(side),
				Downloads: []string{*u},
				FileSize:  int64(len(data)),
			})
			*locked = append(*locked, key)
			return nil
		}
		if !bundle {
			blocked.add(kind, key+" ("+mrpackOrigin(provider, u)+")")
			return nil
		}
		for _, t := range owners {
			t.files[filePath] = data
			// Both spellings mrpackSplit can give a bundled file, so the override
			// tally can drop it either way and no file is counted twice.
			bundledPaths["overrides/"+filePath] = true
			bundledPaths[t.side+"-overrides/"+filePath] = true
		}
		*bundled = append(*bundled, key)
		report.Warnings = append(report.Warnings, fmt.Sprintf("bundled %s from %s into the archive; recipients receive the file itself, not a download link", key, mrpackOrigin(provider, u)))
		return nil
	}
	ids := make([]string, 0, len(b.Lock.Mods))
	for id := range b.Lock.Mods {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		m := b.Lock.Mods[id]
		owners := mrpackOwners(sides, func(t *mrpackSide) bool { return t.mods[id] })
		if len(owners) == 0 {
			continue
		}
		if err := add(id, manifest.TypeMod, "mods/"+m.Filename, m.Side, m.Provider, m.Sha512, m.URL, owners, &report.Mods, &report.BundledMods); err != nil {
			return nil, nil, err
		}
	}
	for _, ref := range b.packRefs() {
		owners := mrpackOwners(sides, func(t *mrpackSide) bool { return t.packs[ref.key] })
		if len(owners) == 0 {
			continue
		}
		locked, bundled := &report.ResourcePacks, &report.BundledResourcePacks
		if ref.kind == manifest.TypeShader {
			locked, bundled = &report.Shaders, &report.BundledShaders
		}
		if err := add(ref.key, ref.kind, ref.path, "client", ref.pack.Provider, ref.pack.Sha512, ref.pack.URL, owners, locked, bundled); err != nil {
			return nil, nil, err
		}
	}
	if blocked.total() > 0 {
		return nil, nil, bundleNudge(out.Errorf("mrpack-host-not-allowed", "%s can't be downloaded by Modrinth launchers", kindCount(blocked.counts)), blocked.items, "shulker export mrpack --bundle")
	}
	return files, bundledPaths, nil
}

func mrpackOwners(sides []*mrpackSide, ships func(*mrpackSide) bool) []*mrpackSide {
	var owners []*mrpackSide
	for _, t := range sides {
		if ships(t) {
			owners = append(owners, t)
		}
	}
	return owners
}

// providerDomains are where a provider serves its own files; a mod downloaded from one of them is
// named by its provider alone.
var providerDomains = map[string][]string{"modrinth": {"modrinth.com"}, "curseforge": {"forgecdn.net", "curseforge.com"}}

func mrpackOrigin(provider string, u *string) string {
	if u == nil {
		return provider + ", manual download"
	}
	parsed, err := url.Parse(*u)
	if err != nil || parsed.Host == "" {
		return provider
	}
	host := parsed.Hostname()
	for _, domain := range providerDomains[provider] {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return provider
		}
	}
	return provider + ", " + parsed.Host
}

func mrpackHostAllowed(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	for _, h := range MrpackHosts {
		if parsed.Hostname() == h {
			return true
		}
	}
	return false
}

func mrpackSplit(sides []*mrpackSide) map[string][]byte {
	entries := map[string][]byte{}
	if len(sides) == 1 {
		for path, data := range sides[0].files {
			entries["overrides/"+path] = data
		}
		return entries
	}
	for path, data := range sides[0].files {
		shared := true
		for _, t := range sides[1:] {
			if other, ok := t.files[path]; !ok || !bytes.Equal(other, data) {
				shared = false
				break
			}
		}
		if shared {
			entries["overrides/"+path] = data
		}
	}
	for _, t := range sides {
		for path, data := range t.files {
			if _, ok := entries["overrides/"+path]; ok {
				continue
			}
			entries[t.side+"-overrides/"+path] = data
		}
	}
	return entries
}

// writeArchive zips entries with first at the front and the rest in name order.
func writeArchive(output, first string, entries map[string][]byte) error {
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for n := range entries {
		if n != first {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	names = append([]string{first}, names...)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, n := range names {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: n, Method: zip.Deflate, Modified: markerTime})
		if err != nil {
			return err
		}
		if _, err := w.Write(entries[n]); err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return fsutil.Write(output, buf.Bytes())
}

func MrpackFileName(m *manifest.Manifest, versionID string) string {
	return m.Name + "-" + versionID + ".mrpack"
}

func CurseForgeFileName(m *manifest.Manifest, version string) string {
	return m.Name + "-" + version + ".zip"
}

func mrpackSummary(m *manifest.Manifest) string {
	var parts []string
	for _, p := range []string{m.Description, m.Note} {
		if p = strings.TrimSpace(p); p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, "\n\n")
}
