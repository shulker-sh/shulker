package build

import (
	"fmt"
	"runtime"
	"sort"
	"strings"

	"github.com/shulker-sh/shulker/internal/manifest"
)

func DetectOS() string {
	if runtime.GOOS == "darwin" {
		return "macos"
	}
	return runtime.GOOS
}

type conditions struct {
	os       string
	features map[string]bool
}

func (b *Builder) conditions(target manifest.Target, opts Options) conditions {
	c := conditions{os: opts.OS, features: opts.Features}
	if !opts.NoOS && c.os == "" {
		c.os = DetectOS()
	}
	if c.features == nil {
		c.features = map[string]bool{}
		for _, f := range target.Features {
			c.features[f] = true
		}
	}
	return c
}

func (c conditions) admits(m manifest.Mod) (bool, string) {
	if ok, why := matches(m.OS, "os", func(name string) bool { return name == c.os }); !ok {
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

func (b *Builder) directEntries(c conditions) map[string]manifest.Mod {
	direct := map[string]manifest.Mod{}
	for id, m := range b.Manifest.Mods {
		direct[id] = m
	}
	for _, p := range b.Packs {
		for id, m := range p.Manifest.Mods {
			if _, ok := b.Manifest.Mods[id]; ok {
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

func (b *Builder) mentionsFeatures() bool {
	for _, m := range b.directEntries(conditions{}) {
		if len(m.Feature) > 0 {
			return true
		}
	}
	for _, t := range b.Manifest.Targets {
		if len(t.Features) > 0 {
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

func (c conditions) admittedBy(m manifest.Mod) string {
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
