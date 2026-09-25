package resolve

import (
	"cmp"
	"fmt"
	"slices"
	"sort"
	"strings"

	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/jarmeta"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/mavenver"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/version/fabric"
)

type Problem struct {
	Rule       string `json:"rule"`
	Mod        string `json:"mod"`
	ModVersion string `json:"modVersion"`
	On         string `json:"on"`
	Declared   string `json:"declared"`
	Found      string `json:"found,omitempty"`
	// Side is the sides whose builds have the problem, when not every checked side does.
	Side string `json:"side,omitempty"`
	// LockedAs is the lock key of a mod that provides On but that this side doesn't place.
	LockedAs  string `json:"lockedAs,omitempty"`
	StaleNote string `json:"staleNote,omitempty"`
}

type Validation struct {
	Problems []Problem
	Warnings []string
	// Undownloaded are the warnings, also in Warnings, for jars whose metadata went unchecked
	// because the cache doesn't have them.
	Undownloaded []string
	Suggestions  []Suggestion
}

type Suggestion struct {
	Mod      string `json:"mod"`
	Kind     string `json:"kind"`
	On       string `json:"on"`
	Declared string `json:"declared"`
	// InstalledAs is the lock key of a mod whose provider slug or key matches On
	// though no jar declares that id. The loader still sees nothing installed.
	InstalledAs string `json:"installedAs,omitempty"`
}

var suggestionKinds = []string{"recommends", "suggests", "optional"}

// Recommended leaves optional dependencies out: they are mostly integrations and would bury the rest.
func (v *Validation) Recommended() []string {
	lines := []string{}
	for _, s := range v.Suggestions {
		if s.Kind != "optional" && s.InstalledAs == "" {
			lines = append(lines, fmt.Sprintf("%s %s %s", s.Mod, s.Kind, s.On))
		}
	}
	return lines
}

func isBuiltin(id string) bool {
	if id == "minecraft" || id == "java" {
		return true
	}
	for _, l := range loader.All {
		if l.DependencyID == id {
			return true
		}
	}
	return false
}

// Validate checks the locked mods' own metadata against each other: dependencies, their versions
// and conflicts. Each side is checked against the mods its build places, so a client mod that
// needs a server-only one fails. With sides given, only those are checked, and a mod none of them
// use goes unchecked without a warning; otherwise the sides the project declares are, or both
// when it declares none.
func (r *Resolver) Validate(sides ...string) (*Validation, error) {
	if r.Lock.Minecraft == "" {
		e := out.Errorf("minecraft-required", "this project sets no minecraft and has no locked modpack to take one from")
		e.Help = "set one with `shulker set minecraft <version>`"
		return nil, e
	}
	v := &Validation{Problems: []Problem{}, Warnings: []string{}, Suggestions: []Suggestion{}}
	builtin := map[string]string{"minecraft": r.Lock.Minecraft, "java": fmt.Sprintf("%d.0", r.Lock.Java.Major)}
	l := loader.Running(r.Lock)
	if l.DependencyID != "" {
		builtin[l.DependencyID] = r.Lock.Loader.Version
	}
	for id, version := range r.Lock.Loader.Provides {
		builtin[id] = version
	}
	infos := map[string]*jarmeta.Info{}
	unread := map[string]bool{}
	for _, id := range r.lockIDs() {
		m := r.Lock.Mods[id]
		if !r.Cache.Has(m.Sha512) {
			if usedBy(m.Side, sides) {
				w := fmt.Sprintf("%s is not downloaded; its metadata was not checked", id)
				v.Warnings, v.Undownloaded = append(v.Warnings, w), append(v.Undownloaded, w)
			}
			unread[id] = true
			continue
		}
		info, err := r.readJar(r.Cache.Object(m.Sha512), m.Filename)
		if err != nil {
			return nil, prefixed("mod "+id, err)
		}
		infos[id] = info
	}
	checked := sides
	if len(checked) == 0 {
		checked = r.Manifest.Sides()
	}
	if len(checked) == 0 {
		checked = []string{"client", "server"}
	}
	overrides, err := r.dependencyOverrides(l, checked)
	if err != nil {
		return nil, err
	}
	sc := sideCheck{infos: infos, unread: unread, builtin: builtin, topLevelOnly: l.TopLevelMandatory, byName: map[string]string{}}
	for _, id := range r.lockIDs() {
		sc.byName[id] = id
		if slug := r.Lock.Mods[id].Slug; slug != "" {
			sc.byName[slug] = id
		}
	}
	onSides := map[Problem][]string{}
	var problems []Problem
	for _, side := range checked {
		sv := r.validateSide(side, overrides[side], sc)
		for _, p := range sv.Problems {
			if onSides[p] == nil {
				problems = append(problems, p)
			}
			onSides[p] = append(onSides[p], side)
		}
		for _, w := range sv.Warnings {
			if !slices.Contains(v.Warnings, w) {
				v.Warnings = append(v.Warnings, w)
			}
		}
		for _, sg := range sv.Suggestions {
			if !slices.Contains(v.Suggestions, sg) {
				v.Suggestions = append(v.Suggestions, sg)
			}
		}
	}
	ignores := r.ignores()
	used := map[int]bool{}
	for _, p := range problems {
		if p.LockedAs != "" || len(onSides[p]) < len(checked) {
			p.Side = strings.Join(onSides[p], ", ")
		}
		v.record(ignores, used, p)
	}
	slices.SortStableFunc(v.Problems, func(a, b Problem) int {
		return cmp.Or(strings.Compare(a.Mod, b.Mod), strings.Compare(a.On, b.On), strings.Compare(a.Side, b.Side))
	})
	for i, ig := range r.Manifest.Ignore {
		if !used[i] {
			v.Warnings = append(v.Warnings, fmt.Sprintf("ignore entry %d (%s: %s on %s) matched nothing", i+1, ig.Rule, ig.Mod, ig.On))
		}
	}
	sort.Slice(v.Suggestions, func(i, j int) bool {
		a, b := v.Suggestions[i], v.Suggestions[j]
		if a.Mod != b.Mod {
			return a.Mod < b.Mod
		}
		if a.Kind != b.Kind {
			return slices.Index(suggestionKinds, a.Kind) < slices.Index(suggestionKinds, b.Kind)
		}
		return a.On < b.On
	})
	return v, nil
}

// sideCheck is what Validate reads once for every side it checks.
type sideCheck struct {
	infos map[string]*jarmeta.Info
	// unread are the locked mods whose jars aren't downloaded, by lock key.
	unread       map[string]bool
	builtin      map[string]string
	topLevelOnly bool
	// byName is each locked mod's key by its key and its provider slug.
	byName map[string]string
}

// validateSide checks the mods side's build places against each other, with that side's
// dependency overrides applied. Its problems carry no Side yet, so Validate can merge the sides'.
func (r *Resolver) validateSide(side string, overrides jarmeta.DependencyOverrides, sc sideCheck) *Validation {
	v := &Validation{}
	placed := map[string]*jarmeta.Info{}
	unreadIDs := map[string]bool{}
	elsewhere := map[string]string{}
	cands := candidates{}
	for _, id := range r.lockIDs() {
		jarID := r.Lock.JarID(id)
		if !r.Lock.Mods[id].PlacedOn(side) {
			elsewhere[jarID] = id
			if info, ok := sc.infos[id]; ok {
				for provided := range info.Provides {
					elsewhere[provided] = id
				}
			}
			continue
		}
		if sc.unread[id] {
			unreadIDs[jarID] = true
			continue
		}
		if info, ok := sc.infos[id]; ok {
			placed[id] = overrides.Apply(info)
			cands.add(jarID, info, 0)
		}
	}
	installed := cands.pick(placed, sc.topLevelOnly)
	for id, version := range sc.builtin {
		installed[id] = version
	}
	for _, id := range sortedKeys(placed) {
		info := placed[id]
		for _, on := range sortedKeys(info.Depends) {
			declared := info.Depends[on]
			found, ok := installed[on]
			if !ok && unreadIDs[on] {
				continue
			}
			if ok {
				match, err := satisfies(info, on, found, declared)
				if err != nil {
					v.Warnings = append(v.Warnings, fmt.Sprintf("%s depends on %s %s but %s: not checked", id, on, declared, err))
					continue
				}
				if match {
					continue
				}
			}
			p := Problem{Rule: "depends", Mod: id, ModVersion: info.Version, On: on, Declared: declared, Found: found}
			if !ok {
				p.LockedAs = elsewhere[on]
			}
			v.Problems = append(v.Problems, p)
		}
		for _, on := range sortedKeys(info.Optional) {
			declared := info.Optional[on]
			found, ok := installed[on]
			if !ok {
				v.Suggestions = append(v.Suggestions, Suggestion{Mod: id, Kind: "optional", On: on, Declared: declared, InstalledAs: sc.byName[on]})
				continue
			}
			match, err := satisfies(info, on, found, declared)
			if err != nil {
				v.Warnings = append(v.Warnings, fmt.Sprintf("%s optionally depends on %s %s but %s: not checked", id, on, declared, err))
				continue
			}
			if !match {
				v.Problems = append(v.Problems, Problem{Rule: "depends", Mod: id, ModVersion: info.Version, On: on, Declared: declared, Found: found})
			}
		}
		for _, on := range sortedKeys(info.Breaks) {
			declared := info.Breaks[on]
			found, ok := installed[on]
			if !ok {
				continue
			}
			match, err := satisfies(info, on, found, declared)
			if err != nil {
				v.Warnings = append(v.Warnings, fmt.Sprintf("%s breaks %s %s but %s: not checked", id, on, declared, err))
				continue
			}
			if match {
				v.Problems = append(v.Problems, Problem{Rule: "breaks", Mod: id, ModVersion: info.Version, On: on, Declared: declared, Found: found})
			}
		}
		for _, on := range sortedKeys(info.Conflicts) {
			found, ok := installed[on]
			if !ok {
				continue
			}
			if match, err := satisfies(info, on, found, info.Conflicts[on]); err == nil && match {
				v.Warnings = append(v.Warnings, fmt.Sprintf("%s %s conflicts with %s %s (installed %s)", id, info.Version, on, info.Conflicts[on], found))
			}
		}
		for kind, set := range map[string]map[string]string{"recommends": info.Recommends, "suggests": info.Suggests} {
			for _, on := range sortedKeys(set) {
				if _, ok := installed[on]; !ok {
					v.Suggestions = append(v.Suggestions, Suggestion{Mod: id, Kind: kind, On: on, Declared: set[on], InstalledAs: sc.byName[on]})
				}
			}
		}
	}
	return v
}

// sideOverrides is the loader's dependency overrides file each validated side's build ships, nil
// for a side that ships none.
type sideOverrides map[string]jarmeta.DependencyOverrides

func (r *Resolver) dependencyOverrides(l loader.Loader, sides []string) (sideOverrides, error) {
	if l.DependencyOverrides == "" {
		return nil, nil
	}
	b := &build.Builder{Dir: r.Dir, Manifest: r.Manifest, Lock: r.Lock, Cache: r.Cache, Packs: r.Packs}
	so := sideOverrides{}
	for _, side := range sides {
		data, ok, err := b.OverrideFile(side, l.DependencyOverrides)
		if err != nil {
			return nil, err
		}
		if !ok {
			so[side] = nil
			continue
		}
		if so[side], err = jarmeta.ParseDependencyOverrides(data); err != nil {
			return nil, out.Errorf("dependency-overrides-invalid", "%s can't read the %s the %s build places", l.Title, l.DependencyOverrides, side).WithCause(l.DependencyOverrides, err)
		}
	}
	return so, nil
}

// candidate is one copy of a mod id the loader could load: a locked jar, a jar nested in one at
// any depth, or an id either provides.
type candidate struct {
	version string
	depth   int
	maven   bool
}

type candidates map[string][]candidate

func (c candidates) add(id string, info *jarmeta.Info, depth int) {
	c[id] = append(c[id], candidate{info.Version, depth, info.UsesMavenRanges})
	for pid, pv := range info.Provides {
		c[pid] = append(c[pid], candidate{pv, depth, info.UsesMavenRanges})
	}
	for _, n := range info.Nested {
		c.add(n.ID, n, depth+1)
	}
}

// pick chooses the copy of each id the loader would load, as Fabric Loader's resolver prefers them:
// a locked jar over a nested one, then the newest version, then the least nested. The first copy
// every locked mod's ranges on the id accept wins, so an older copy stands in when the newest fails
// them; with none accepted the most preferred is kept, for the problems to name. With topLevelOnly
// a locked jar is never replaced, as Quilt loads every jar in mods/.
func (c candidates) pick(infos map[string]*jarmeta.Info, topLevelOnly bool) map[string]string {
	picked := map[string]string{}
	for id, list := range c {
		slices.SortStableFunc(list, func(a, b candidate) int {
			if (a.depth == 0) != (b.depth == 0) {
				if a.depth == 0 {
					return -1
				}
				return 1
			}
			if n := compareVersions(b, a); n != 0 {
				return n
			}
			return a.depth - b.depth
		})
		if topLevelOnly && list[0].depth == 0 {
			list = slices.DeleteFunc(slices.Clone(list), func(c candidate) bool { return c.depth > 0 })
		}
		picked[id] = list[0].version
		for _, cand := range list {
			if accepts(infos, id, cand.version) {
				picked[id] = cand.version
				break
			}
		}
	}
	return picked
}

func accepts(infos map[string]*jarmeta.Info, id, version string) bool {
	for _, info := range infos {
		for _, set := range []map[string]string{info.Depends, info.Optional} {
			if declared, ok := set[id]; ok {
				if match, err := satisfies(info, id, version, declared); err == nil && !match {
					return false
				}
			}
		}
		if declared, ok := info.Breaks[id]; ok {
			if match, err := satisfies(info, id, version, declared); err == nil && match {
				return false
			}
		}
	}
	return true
}

func compareVersions(a, b candidate) int {
	if a.maven && b.maven {
		return mavenver.Compare(mavenver.Parse(a.version), mavenver.Parse(b.version))
	}
	return fabric.Compare(fabric.Parse(a.version), fabric.Parse(b.version))
}

type ignoreEntry struct {
	manifest.Ignore
	label string
}

func (r *Resolver) ignores() []ignoreEntry {
	var list []ignoreEntry
	for i, ig := range r.Manifest.Ignore {
		list = append(list, ignoreEntry{ig, fmt.Sprintf("ignore entry %d", i+1)})
	}
	for _, p := range r.Packs {
		for i, ig := range p.Manifest.Ignore {
			list = append(list, ignoreEntry{ig, fmt.Sprintf("ignore entry %d of modpack %s", i+1, p.Name)})
		}
	}
	return list
}

func (v *Validation) record(ignores []ignoreEntry, used map[int]bool, p Problem) {
	for i, ig := range ignores {
		if ig.Rule != p.Rule || ig.Mod != p.Mod || ig.On != p.On {
			continue
		}
		used[i] = true
		if ig.Declared == p.Declared {
			return
		}
		p.StaleNote = fmt.Sprintf("%s is stale: it was written for %q", ig.label, ig.Declared)
	}
	v.Problems = append(v.Problems, p)
}

// Err is the validation-failed error for the problems found, nil when there are none.
func (v *Validation) Err() error {
	if len(v.Problems) == 0 {
		return nil
	}
	var b strings.Builder
	var items []string
	var rows []out.Detail
	fmt.Fprintf(&b, "%d problem(s) in the locked mods:", len(v.Problems))
	for i, p := range v.Problems {
		line := p.line()
		items = append(items, line)
		row := out.Detail{Text: line}
		fmt.Fprintf(&b, "\n  Problem %d\n    - %s", i+1, line)
		if p.StaleNote != "" {
			fmt.Fprintf(&b, "\n      %s", p.StaleNote)
			row.Children = append(row.Children, out.Detail{Text: p.StaleNote})
		}
		if fix := p.fix(); fix != "" {
			fmt.Fprintf(&b, "\n      Fix: %s", fix)
			row.Children = append(row.Children, out.Detail{Label: "Fix", Text: fix, IsCommand: true})
		}
		ignore := p.ignoreCommand()
		fmt.Fprintf(&b, "\n      Ignore: %s", ignore)
		row.Children = append(row.Children, out.Detail{Label: "Ignore", Text: ignore, IsCommand: true})
		rows = append(rows, row)
	}
	e := out.Errorf("validation-failed", "%s", b.String())
	e.Items = items
	e.Rows = rows
	return e
}

func (p Problem) line() string {
	var line string
	switch {
	case p.Rule == "breaks":
		line = fmt.Sprintf("%s %s breaks %s %s, found %s %s", p.Mod, p.ModVersion, p.On, p.Declared, p.On, p.Found)
	case p.LockedAs != "":
		return fmt.Sprintf("%s %s requires %s %s, which the %s doesn't place", p.Mod, p.ModVersion, p.On, p.Declared, p.Side)
	case p.Found == "":
		line = fmt.Sprintf("%s %s requires %s %s, not installed", p.Mod, p.ModVersion, p.On, p.Declared)
	default:
		line = fmt.Sprintf("%s %s requires %s %s, found %s %s", p.Mod, p.ModVersion, p.On, p.Declared, p.On, p.Found)
	}
	if p.Side != "" {
		line += " on the " + p.Side
	}
	return line
}

// fix is the command that clears the problem, empty when there isn't one to suggest.
func (p Problem) fix() string {
	switch {
	case p.Rule != "depends" || p.Found != "" || isBuiltin(p.On):
		return ""
	case p.LockedAs != "":
		return fmt.Sprintf("shulker set requires.%s.side both", p.LockedAs)
	}
	return "shulker add " + p.On
}

func (p Problem) ignoreCommand() string {
	return fmt.Sprintf(`shulker ignore %s %s --rule %s --declared "%s" --note "why this is safe"`, p.Mod, p.On, p.Rule, p.Declared)
}

func satisfies(info *jarmeta.Info, on, version, declared string) (bool, error) {
	if info.UsesMavenRanges {
		rng, err := mavenver.ParseRange(declared)
		if err != nil {
			return false, err
		}
		return rng.Contains(mavenver.Parse(version)), nil
	}
	if on == "minecraft" {
		version = fabric.Game(version)
	}
	return fabricSatisfies(version, declared)
}

// fabricSatisfies matches as Fabric Loader does. Alternatives are joined by "||", the way an array
// of ranges in fabric.mod.json and a Quilt any-of are held.
func fabricSatisfies(version, declared string) (bool, error) {
	var alts []fabric.Predicate
	for _, alt := range strings.Split(declared, "||") {
		p, err := fabric.ParsePredicate(strings.TrimSpace(alt))
		if err != nil {
			return false, fmt.Errorf("range %q is not understood", declared)
		}
		alts = append(alts, p)
	}
	v := fabric.Parse(version)
	return slices.ContainsFunc(alts, func(p fabric.Predicate) bool { return p.Test(v) }), nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
