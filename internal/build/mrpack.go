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
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/mrpack"
	"shulker.sh/shulker/internal/out"
)

var MrpackHosts = []string{"cdn.modrinth.com", "github.com", "raw.githubusercontent.com", "gitlab.com"}

type MrpackOptions struct {
	Targets   []string
	VersionID string
	Output    string
	Bundle    bool
	OS        string
	Features  map[string]bool
}

type MrpackReport struct {
	Path      string   `json:"path"`
	VersionID string   `json:"versionId"`
	Name      string   `json:"name"`
	Targets   []string `json:"targets"`
	Mods      []string `json:"mods"`
	Bundled   []string `json:"bundled"`
	Overrides []string `json:"overrides"`
	Warnings  []string `json:"-"`
}

type mrpackTarget struct {
	name  string
	side  string
	files map[string][]byte
	mods  map[string]bool
}

func (b *Builder) ExportMrpack(opts MrpackOptions) (*MrpackReport, error) {
	targets, err := b.mrpackTargets(opts.Targets)
	if err != nil {
		return nil, err
	}
	report := &MrpackReport{Path: opts.Output, VersionID: opts.VersionID, Name: b.mrpackName(targets), Targets: []string{}, Mods: []string{}, Bundled: []string{}, Overrides: []string{}, Warnings: []string{}}
	for _, t := range targets {
		report.Targets = append(report.Targets, t.name)
		if err := b.mrpackCollect(t, opts, report); err != nil {
			return nil, err
		}
	}
	files, err := b.mrpackMods(targets, opts.Bundle, report)
	if err != nil {
		return nil, err
	}
	entries := mrpackSplit(targets)
	for path := range entries {
		report.Overrides = append(report.Overrides, path)
	}
	sort.Strings(report.Overrides)
	l, _ := loader.Lookup(b.Lock.Loader.Type)
	index := mrpack.Index{
		FormatVersion: mrpack.FormatVersion,
		Game:          mrpack.Game,
		VersionID:     opts.VersionID,
		Name:          report.Name,
		Summary:       mrpackSummary(b.Manifest),
		Files:         files,
		Dependencies:  map[string]string{"minecraft": b.Lock.Minecraft, l.MrpackKey: b.Lock.Loader.Version},
	}
	indexData, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return nil, err
	}
	entries[mrpack.IndexName] = append(indexData, '\n')
	if err := writeMrpack(opts.Output, entries); err != nil {
		return nil, err
	}
	return report, nil
}

func (b *Builder) mrpackTargets(names []string) ([]*mrpackTarget, error) {
	if len(names) == 0 {
		for n := range b.Manifest.Targets {
			names = append(names, n)
		}
		sort.Strings(names)
	}
	var targets []*mrpackTarget
	bySide := map[string]string{}
	for _, n := range names {
		t, err := b.Manifest.Target(n)
		if err != nil {
			return nil, err
		}
		if other, dup := bySide[t.Side]; dup {
			e := out.Errorf("ambiguous-target", "targets %s and %s are both %s side; pass --target", other, n, t.Side)
			e.Candidates = names
			return nil, e
		}
		bySide[t.Side] = n
		targets = append(targets, &mrpackTarget{name: n, side: t.Side, files: map[string][]byte{}})
	}
	return targets, nil
}

func (b *Builder) mrpackName(targets []*mrpackTarget) string {
	pick := targets[0]
	for _, t := range targets {
		if t.side == "client" {
			pick = t
		}
	}
	if name := b.Manifest.Targets[pick.name].Name; name != "" {
		return name
	}
	return b.Manifest.Name
}

func (b *Builder) mrpackCollect(t *mrpackTarget, opts MrpackOptions, report *MrpackReport) error {
	rep := &Report{}
	desired, _, err := b.collect(t.name, b.Manifest.Targets[t.name], Options{OS: opts.OS, NoOS: opts.OS == "", Features: opts.Features}, rep)
	if err != nil {
		return err
	}
	report.Warnings = append(report.Warnings, rep.Warnings...)
	t.mods = map[string]bool{}
	for id, m := range b.Lock.Mods {
		if _, ok := desired["mods/"+m.Filename]; ok {
			t.mods[id] = true
		}
	}
	for _, e := range rep.Excluded {
		if strings.Contains(e, "(needs os ") {
			report.Warnings = append(report.Warnings, fmt.Sprintf("%s: left out of %s; pass --os to export that variation", e, t.name))
		}
	}
	for path, s := range desired {
		if s.sha512 != "" {
			continue
		}
		if s.owned != nil {
			data, err := s.owned.render(nil, nil, nil)
			if err != nil {
				return err
			}
			t.files[path] = data
			continue
		}
		t.files[path] = s.data
	}
	return nil
}

func (b *Builder) mrpackMods(targets []*mrpackTarget, bundle bool, report *MrpackReport) ([]mrpack.File, error) {
	files := []mrpack.File{}
	var blocked []string
	ids := make([]string, 0, len(b.Lock.Mods))
	for id := range b.Lock.Mods {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		m := b.Lock.Mods[id]
		var owners []*mrpackTarget
		for _, t := range targets {
			if t.mods[id] {
				owners = append(owners, t)
			}
		}
		if len(owners) == 0 {
			continue
		}
		data, err := os.ReadFile(b.Cache.Path(m.Sha512))
		if err != nil {
			return nil, out.Errorf("not-installed", "%s is not in the cache; run `shulker install`", id)
		}
		if m.URL != nil && mrpackHostAllowed(*m.URL) {
			sum := sha1.Sum(data)
			files = append(files, mrpack.File{
				Path:      "mods/" + m.Filename,
				Hashes:    map[string]string{"sha1": hex.EncodeToString(sum[:]), "sha512": m.Sha512},
				Env:       mrpack.Env(m.Side),
				Downloads: []string{*m.URL},
				FileSize:  int64(len(data)),
			})
			report.Mods = append(report.Mods, id)
			continue
		}
		if !bundle {
			blocked = append(blocked, id+" ("+mrpackOrigin(m.Provider, m.URL)+")")
			continue
		}
		for _, t := range owners {
			t.files["mods/"+m.Filename] = data
		}
		report.Bundled = append(report.Bundled, id)
		report.Warnings = append(report.Warnings, fmt.Sprintf("bundled %s from %s into the archive; recipients receive the file itself, not a download link", id, mrpackOrigin(m.Provider, m.URL)))
	}
	if len(blocked) > 0 {
		e := out.Errorf("mrpack-host-not-allowed", "Modrinth launchers only download from %s; pass --bundle to ship these mods inside the archive instead: %s", strings.Join(MrpackHosts, ", "), strings.Join(blocked, ", "))
		e.Items = blocked
		return nil, e
	}
	return files, nil
}

func mrpackOrigin(provider string, u *string) string {
	if u == nil {
		return provider + ", manual download"
	}
	parsed, err := url.Parse(*u)
	if err != nil || parsed.Host == "" {
		return provider
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

func mrpackSplit(targets []*mrpackTarget) map[string][]byte {
	entries := map[string][]byte{}
	if len(targets) == 1 {
		for path, data := range targets[0].files {
			entries["overrides/"+path] = data
		}
		return entries
	}
	for path, data := range targets[0].files {
		shared := true
		for _, t := range targets[1:] {
			if other, ok := t.files[path]; !ok || !bytes.Equal(other, data) {
				shared = false
				break
			}
		}
		if shared {
			entries["overrides/"+path] = data
		}
	}
	for _, t := range targets {
		for path, data := range t.files {
			if _, ok := entries["overrides/"+path]; ok {
				continue
			}
			entries[t.side+"-overrides/"+path] = data
		}
	}
	return entries
}

func writeMrpack(output string, entries map[string][]byte) error {
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for n := range entries {
		if n != mrpack.IndexName {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	names = append([]string{mrpack.IndexName}, names...)
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

func mrpackSummary(m *manifest.Manifest) string {
	var parts []string
	for _, p := range []string{m.Description, m.Note} {
		if p = strings.TrimSpace(p); p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, "\n\n")
}
