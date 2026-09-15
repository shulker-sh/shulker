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

func DetectOS() string {
	if runtime.GOOS == "darwin" {
		return "macos"
	}
	return runtime.GOOS
}

type conditions struct {
	os       string
	anyOS    bool
	features map[string]bool
}

func ValidOS(name string) bool {
	return name == "macos" || name == "windows" || name == "linux"
}

func (b *Builder) conditions(target manifest.Target, opts Options) conditions {
	c := conditions{os: opts.OS, features: map[string]bool{}}
	if !opts.NoOS && c.os == "" {
		c.os = DetectOS()
	}
	for _, f := range target.Features {
		c.features[f] = true
	}
	for name, on := range opts.Features {
		c.features[name] = on
	}
	return c
}

type Feature struct {
	Name     string   `json:"name"`
	Mods     []string `json:"mods"`
	Defaults []string `json:"defaultTargets"`
}

func (b *Builder) Features() []Feature {
	byName := map[string]*Feature{}
	get := func(name string) *Feature {
		if f, ok := byName[name]; ok {
			return f
		}
		f := &Feature{Name: name, Mods: []string{}, Defaults: []string{}}
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
	for name, t := range b.Manifest.Targets {
		for _, f := range t.Features {
			get(f).Defaults = append(get(f).Defaults, name)
		}
	}
	res := make([]Feature, 0, len(byName))
	for _, f := range byName {
		sort.Slice(f.Mods, func(i, j int) bool {
			return strings.TrimPrefix(f.Mods[i], "!") < strings.TrimPrefix(f.Mods[j], "!")
		})
		sort.Strings(f.Defaults)
		res = append(res, *f)
	}
	sort.Slice(res, func(i, j int) bool { return res[i].Name < res[j].Name })
	return res
}

func (c conditions) admits(m manifest.Require) (bool, string) {
	if ok, why := matches(m.OS, "os", func(name string) bool { return name == c.os }); !ok && !c.anyOS {
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
				sel.warnings = append(sel.warnings, fmt.Sprintf("%s is gated off (%s) but %s requires it; included", id, reasons[id], strings.Join(includedRequirers(m.RequiredBy, included), ", ")))
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

// Placement is where a locked mod lands by the project's own settings: each
// target's default features, with OS conditions reported rather than checked,
// so every machine gives the same answer.
type Placement struct {
	Targets []string
	OS      manifest.StringList
	Feature manifest.StringList
}

func (b *Builder) Placements() map[string]Placement {
	placements := map[string]Placement{}
	for _, name := range slices.Sorted(maps.Keys(b.Manifest.Targets)) {
		target := b.Manifest.Targets[name]
		c := conditions{anyOS: true, features: map[string]bool{}}
		for _, f := range target.Features {
			c.features[f] = true
		}
		for id := range b.selectMods(c).included {
			if m := b.Lock.Mods[id]; m.Side != "both" && m.Side != target.Side {
				continue
			}
			p := placements[id]
			p.Targets = append(p.Targets, name)
			placements[id] = p
		}
	}
	for id, m := range b.directEntries(conditions{anyOS: true}) {
		if _, locked := b.Lock.Mods[id]; !locked {
			continue
		}
		p := placements[id]
		p.OS, p.Feature = m.OS, m.Feature
		placements[id] = p
	}
	return placements
}
