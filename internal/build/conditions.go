package build

import (
	"fmt"
	"maps"
	"runtime"
	"slices"
	"sort"
	"strings"

	"shulker.sh/shulker/internal/manifest"
)

// DetectOS is this machine's OS in a manifest's words: macos, windows or linux.
func DetectOS() string {
	if runtime.GOOS == "darwin" {
		return "macos"
	}
	return runtime.GOOS
}

type conditions struct {
	os          string
	admitsAnyOS bool
	features    map[string]bool
}

// ValidOS reports whether name is an OS a manifest can name.
func ValidOS(name string) bool {
	return name == "macos" || name == "windows" || name == "linux"
}

func (b *Builder) conditions(opts Options) conditions {
	c := conditions{os: opts.OS, features: map[string]bool{}}
	if !opts.NoOS && c.os == "" {
		c.os = DetectOS()
	}
	for _, m := range b.featureDecls() {
		for name, f := range m.Features {
			c.features[name] = f.Default
		}
	}
	for name, on := range opts.Features {
		c.features[name] = on
	}
	return c
}

// featureDecls lists the manifests declaring features, each pulled pack before
// the project, so a name the project also declares takes the project's default.
func (b *Builder) featureDecls() []*manifest.Manifest {
	decls := make([]*manifest.Manifest, 0, len(b.Packs)+1)
	for _, p := range b.Packs {
		decls = append(decls, p.Manifest)
	}
	return append(decls, b.Manifest)
}

// Feature is one feature with the mods it gates, a "!" before a mod the feature turns off.
// Origin is the modpack that declares it, empty when the project does.
type Feature struct {
	Name    string   `json:"name"`
	Mods    []string `json:"mods"`
	Default bool     `json:"default"`
	Origin  string   `json:"origin,omitempty"`
}

// Features is every feature the project and its modpacks declare or gate a mod on, by name.
func (b *Builder) Features() []Feature {
	byName := map[string]*Feature{}
	get := func(name string) *Feature {
		if f, ok := byName[name]; ok {
			return f
		}
		f := &Feature{Name: name, Mods: []string{}}
		byName[name] = f
		return f
	}
	gate := func(id string, m manifest.Require) {
		for _, item := range m.Feature {
			name, negated := strings.CutPrefix(item, "!")
			label := id
			if negated {
				label = "!" + id
			}
			f := get(name)
			if !slices.Contains(f.Mods, label) {
				f.Mods = append(f.Mods, label)
			}
		}
	}
	own := b.Manifest.Mods()
	for id, m := range own {
		gate(id, m)
	}
	for _, p := range b.Packs {
		for id, m := range p.Manifest.Mods() {
			if _, ok := own[id]; !ok {
				gate(id, m)
			}
		}
	}
	for _, p := range b.Packs {
		for name, decl := range p.Manifest.Features {
			f := get(name)
			f.Default, f.Origin = decl.Default, p.Name
		}
	}
	for name, decl := range b.Manifest.Features {
		f := get(name)
		f.Default, f.Origin = decl.Default, ""
	}
	res := make([]Feature, 0, len(byName))
	for _, f := range byName {
		sort.Slice(f.Mods, func(i, j int) bool {
			return strings.TrimPrefix(f.Mods[i], "!") < strings.TrimPrefix(f.Mods[j], "!")
		})
		res = append(res, *f)
	}
	sort.Slice(res, func(i, j int) bool { return res[i].Name < res[j].Name })
	return res
}

func (c conditions) admits(m manifest.Require) (bool, string) {
	if ok, why := matches(m.OS, "os", func(name string) bool { return name == c.os }); !ok && !c.admitsAnyOS {
		return false, why
	}
	return matches(m.Feature, "feature", func(name string) bool { return c.features[name] })
}

func matches(list manifest.StringList, kind string, has func(string) bool) (bool, string) {
	var wanted []string
	anyWanted := false
	for _, item := range list {
		if name, negated := strings.CutPrefix(item, "!"); negated {
			if has(name) {
				if kind == "os" {
					return false, "os is " + name
				}
				return false, fmt.Sprintf("feature %s is on", name)
			}
			continue
		}
		wanted = append(wanted, item)
		if has(item) {
			anyWanted = true
		}
	}
	if len(wanted) > 0 && !anyWanted {
		return false, fmt.Sprintf("needs %s %s", kind, strings.Join(wanted, " or "))
	}
	return true, ""
}

type selection struct {
	included map[string]bool
	excluded []string
	warnings []string
}

func (b *Builder) directEntries(c conditions) map[string]manifest.Require {
	own := b.Manifest.Mods()
	direct := map[string]manifest.Require{}
	for id, m := range own {
		direct[id] = m
	}
	for _, p := range b.Packs {
		for id, m := range p.Manifest.Mods() {
			if _, ok := own[id]; ok {
				continue
			}
			if cur, seen := direct[id]; seen {
				if ok, _ := c.admits(cur); ok {
					continue
				}
			}
			direct[id] = m
		}
	}
	return direct
}

func (b *Builder) mentionsOS() bool {
	for _, m := range b.directEntries(conditions{}) {
		if len(m.OS) > 0 {
			return true
		}
	}
	return false
}

func (c conditions) osLabel() string {
	switch c.os {
	case "macos":
		return "macOS"
	case "windows":
		return "Windows"
	case "linux":
		return "Linux"
	case "":
		return "any"
	}
	return c.os
}

func (c conditions) featureLabels() []string {
	var on []string
	for name, enabled := range c.features {
		if enabled {
			on = append(on, name)
		}
	}
	sort.Strings(on)
	return on
}

func (c conditions) admittedBy(m manifest.Require) string {
	var parts []string
	for _, name := range m.OS {
		if name == c.os {
			parts = append(parts, c.osLabel())
			break
		}
	}
	var features []string
	for _, name := range m.Feature {
		if c.features[name] {
			features = append(features, name)
		}
	}
	switch len(features) {
	case 0:
	case 1:
		parts = append(parts, "feature: "+features[0])
	default:
		parts = append(parts, "features: "+strings.Join(features, ", "))
	}
	return strings.Join(parts, ", ")
}

func (b *Builder) selectMods(c conditions) selection {
	direct := b.directEntries(c)
	admitted := map[string]bool{}
	reasons := map[string]string{}
	for id := range b.Lock.Mods {
		m, isDirect := direct[id]
		if !isDirect {
			continue
		}
		ok, why := c.admits(m)
		admitted[id] = ok
		reasons[id] = why
	}
	included := map[string]bool{}
	for id, ok := range admitted {
		if ok {
			included[id] = true
		}
	}
	for changed := true; changed; {
		changed = false
		for id, m := range b.Lock.Mods {
			if included[id] {
				continue
			}
			for _, by := range m.RequiredBy {
				if _, isMod := b.Lock.Mods[by]; isMod && included[by] {
					included[id] = true
					changed = true
					break
				}
			}
		}
	}
	sel := selection{included: included}
	for id, m := range b.Lock.Mods {
		if included[id] {
			if ok, isDirect := admitted[id]; isDirect && !ok {
				sel.warnings = append(sel.warnings, fmt.Sprintf("%s is gated off (%s) but %s requires it; included.", id, reasons[id], strings.Join(includedRequirers(m.RequiredBy, included), ", ")))
			}
			continue
		}
		if _, isDirect := admitted[id]; isDirect {
			sel.excluded = append(sel.excluded, fmt.Sprintf("%s (%s)", id, reasons[id]))
		} else if len(m.RequiredBy) > 0 {
			sel.excluded = append(sel.excluded, fmt.Sprintf("%s (only required by %s)", id, strings.Join(m.RequiredBy, ", ")))
		} else {
			sel.included[id] = true
		}
	}
	sort.Strings(sel.excluded)
	sort.Strings(sel.warnings)
	return sel
}

func includedRequirers(by []string, included map[string]bool) []string {
	var names []string
	for _, id := range by {
		if included[id] {
			names = append(names, id)
		}
	}
	return names
}

// Placement is where a locked mod lands by the project's own settings: the
// features on by default, with OS conditions reported rather than checked, so
// every machine gives the same answer.
type Placement struct {
	Sides   []string
	OS      manifest.StringList
	Feature manifest.StringList
}

// Placements is where each locked mod and pack lands, by key.
func (b *Builder) Placements() map[string]Placement {
	placements := map[string]Placement{}
	c := conditions{admitsAnyOS: true, features: map[string]bool{}}
	for _, m := range b.featureDecls() {
		for name, f := range m.Features {
			c.features[name] = f.Default
		}
	}
	for _, side := range b.Manifest.Sides() {
		for id := range b.selectMods(c).included {
			if !b.Lock.Mods[id].PlacedOn(side) {
				continue
			}
			p := placements[id]
			p.Sides = append(p.Sides, side)
			placements[id] = p
		}
		for key, entry := range b.Lock.Datapacks {
			if entry.Side != "both" && entry.Side != side {
				continue
			}
			listed, isListed := b.Manifest.Datapacks()[key]
			if isListed {
				if admitted, _ := c.admits(listed); !admitted {
					continue
				}
			}
			p := placements[key]
			p.Sides = append(p.Sides, side)
			if isListed {
				p.OS, p.Feature = listed.OS, listed.Feature
			}
			placements[key] = p
		}
		if side != "client" {
			continue
		}
		for _, ref := range b.packRefs() {
			if ref.hybrid {
				continue
			}
			entry, isListed := ref.listed(b.Manifest)
			if isListed {
				if admitted, _ := c.admits(entry); !admitted {
					continue
				}
			}
			p := placements[ref.key]
			p.Sides = append(p.Sides, side)
			if isListed {
				p.OS, p.Feature = entry.OS, entry.Feature
			}
			placements[ref.key] = p
		}
	}
	for id, m := range b.directEntries(conditions{admitsAnyOS: true}) {
		if _, locked := b.Lock.Mods[id]; !locked {
			continue
		}
		p := placements[id]
		p.OS, p.Feature = m.OS, m.Feature
		placements[id] = p
	}
	return placements
}

// warnFeatureConflict reports two enabled features writing the same file. The
// alphabetical layer order makes the winner deterministic but arbitrary to the
// author, so it is named. A feature overriding a base layer is the point of
// features and says nothing.
func warnFeatureConflict(report *Report, was, now, rel string) {
	if report == nil || was == "" || now == "" || was == now {
		return
	}
	report.Warnings = append(report.Warnings, fmt.Sprintf("%s and %s both write %s; %s wins.", was, now, rel, now))
}

// FeatureOverrides is every feature decision in force for a build: the layered decisions, then
// each feature --with turns on and --without turns off.
func FeatureOverrides(decisions map[string]bool, with, without []string) map[string]bool {
	overrides := MergeDecisions(decisions)
	for _, name := range with {
		overrides[name] = true
	}
	for _, name := range without {
		overrides[name] = false
	}
	return overrides
}

// MergeDecisions layers feature decisions, a later layer's choice winning over an earlier one's.
func MergeDecisions(layers ...map[string]bool) map[string]bool {
	merged := map[string]bool{}
	for _, l := range layers {
		maps.Copy(merged, l)
	}
	return merged
}
