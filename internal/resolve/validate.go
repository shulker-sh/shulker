package resolve

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"shulker.sh/shulker/internal/jarmeta"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/mavenver"
	"shulker.sh/shulker/internal/mcver"
	"shulker.sh/shulker/internal/out"
)

type Problem struct {
	Rule       string `json:"rule"`
	Mod        string `json:"mod"`
	ModVersion string `json:"modVersion"`
	On         string `json:"on"`
	Declared   string `json:"declared"`
	Found      string `json:"found,omitempty"`
	StaleNote  string `json:"staleNote,omitempty"`
}

type Validation struct {
	Problems    []Problem
	Warnings    []string
	Suggestions []Suggestion
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
// and conflicts. With sides given, a mod none of them use goes unchecked without a warning.
func (r *Resolver) Validate(sides ...string) (*Validation, error) {
	if r.Lock.Minecraft == "" {
		e := out.Errorf("minecraft-required", "this project sets no minecraft and has no locked modpack to take one from")
		e.Help = "set one with `shulker set minecraft <version>`"
		return nil, e
	}
	v := &Validation{Problems: []Problem{}, Warnings: []string{}, Suggestions: []Suggestion{}}
	builtin := map[string]string{"minecraft": r.Lock.Minecraft, "java": fmt.Sprintf("%d.0", r.Lock.Java.Major)}
	l, _ := loader.Lookup(r.Lock.Loader.Type)
	if l.DependencyID != "" {
		builtin[l.DependencyID] = r.Lock.Loader.Version
	}
	for id, version := range r.Lock.Loader.Provides {
		builtin[id] = version
	}
	infos := map[string]*jarmeta.Info{}
	unread := map[string]bool{}
	cands := candidates{}
	for _, id := range r.lockIDs() {
		m := r.Lock.Mods[id]
		if !r.Cache.Has(m.Sha512) {
			if usedBy(m.Side, sides) {
				v.Warnings = append(v.Warnings, fmt.Sprintf("%s is not downloaded; its metadata was not checked", id))
			}
			unread[r.Lock.JarID(id)] = true
			continue
		}
		info, err := jarmeta.Read(r.Cache.Object(m.Sha512), m.Filename, r.Lock.Loader.Type)
		if err != nil {
			return nil, prefixed("mod "+id, err)
		}
		infos[id] = info
		cands.add(r.Lock.JarID(id), info, 0)
	}
	installed := cands.pick(infos, l.TopLevelMandatory)
	for id, version := range builtin {
		installed[id] = version
	}
	byName := map[string]string{}
	for _, id := range r.lockIDs() {
		byName[id] = id
		if slug := r.Lock.Mods[id].Slug; slug != "" {
			byName[slug] = id
		}
	}
	ignores := r.ignores()
	used := map[int]bool{}
	for _, id := range sortedKeys(infos) {
		info := infos[id]
		for _, on := range sortedKeys(info.Depends) {
			declared := info.Depends[on]
			found, ok := installed[on]
			if !ok && unread[on] {
				continue
			}
			if ok {
				match, err := satisfies(info, found, declared)
				if err != nil {
					v.Warnings = append(v.Warnings, fmt.Sprintf("%s depends on %s %s but %s: not checked", id, on, declared, err))
					continue
				}
				if match {
					continue
				}
			}
			v.record(ignores, used, Problem{Rule: "depends", Mod: id, ModVersion: info.Version, On: on, Declared: declared, Found: found})
		}
		for _, on := range sortedKeys(info.Optional) {
			declared := info.Optional[on]
			found, ok := installed[on]
			if !ok {
				v.Suggestions = append(v.Suggestions, Suggestion{Mod: id, Kind: "optional", On: on, Declared: declared, InstalledAs: byName[on]})
				continue
			}
			match, err := satisfies(info, found, declared)
			if err != nil {
				v.Warnings = append(v.Warnings, fmt.Sprintf("%s optionally depends on %s %s but %s: not checked", id, on, declared, err))
				continue
			}
			if !match {
				v.record(ignores, used, Problem{Rule: "depends", Mod: id, ModVersion: info.Version, On: on, Declared: declared, Found: found})
			}
		}
		for _, on := range sortedKeys(info.Breaks) {
			declared := info.Breaks[on]
			found, ok := installed[on]
			if !ok {
				continue
			}
			match, err := satisfies(info, found, declared)
			if err != nil {
				v.Warnings = append(v.Warnings, fmt.Sprintf("%s breaks %s %s but %s: not checked", id, on, declared, err))
				continue
			}
			if match {
				v.record(ignores, used, Problem{Rule: "breaks", Mod: id, ModVersion: info.Version, On: on, Declared: declared, Found: found})
			}
		}
		for _, on := range sortedKeys(info.Conflicts) {
			found, ok := installed[on]
			if !ok {
				continue
			}
			if match, err := satisfies(info, found, info.Conflicts[on]); err == nil && match {
				v.Warnings = append(v.Warnings, fmt.Sprintf("%s %s conflicts with %s %s (installed %s)", id, info.Version, on, info.Conflicts[on], found))
			}
		}
		for kind, set := range map[string]map[string]string{"recommends": info.Recommends, "suggests": info.Suggests} {
			for _, on := range sortedKeys(set) {
				if _, ok := installed[on]; !ok {
					v.Suggestions = append(v.Suggestions, Suggestion{Mod: id, Kind: kind, On: on, Declared: set[on], InstalledAs: byName[on]})
				}
			}
		}
	}
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
				if match, err := satisfies(info, version, declared); err == nil && !match {
					return false
				}
			}
		}
		if declared, ok := info.Breaks[id]; ok {
			if match, err := satisfies(info, version, declared); err == nil && match {
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
	va, errA := mcver.Parse(normalizeVersion(a.version))
	vb, errB := mcver.Parse(normalizeVersion(b.version))
	if errA == nil && errB == nil {
		return va.Compare(vb)
	}
	return mavenver.Compare(mavenver.Parse(normalizeVersion(a.version)), mavenver.Parse(normalizeVersion(b.version)))
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
		if p.Rule == "depends" && p.Found == "" && !isBuiltin(p.On) {
			fmt.Fprintf(&b, "\n      Fix: shulker add %s", p.On)
			row.Children = append(row.Children, out.Detail{Label: "Fix", Text: "shulker add " + p.On, IsCommand: true})
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
	if p.Rule == "breaks" {
		return fmt.Sprintf("%s %s breaks %s %s, found %s %s", p.Mod, p.ModVersion, p.On, p.Declared, p.On, p.Found)
	}
	if p.Found == "" {
		return fmt.Sprintf("%s %s requires %s %s, not installed", p.Mod, p.ModVersion, p.On, p.Declared)
	}
	return fmt.Sprintf("%s %s requires %s %s, found %s %s", p.Mod, p.ModVersion, p.On, p.Declared, p.On, p.Found)
}

func (p Problem) ignoreCommand() string {
	return fmt.Sprintf(`shulker ignore %s %s --rule %s --declared "%s" --note "why this is safe"`, p.Mod, p.On, p.Rule, p.Declared)
}

var (
	bareInt  = regexp.MustCompile(`^\d+$`)
	minorX   = regexp.MustCompile(`^(\d+)\.[xX*]$`)
	patchX   = regexp.MustCompile(`^(\d+)\.(\d+)\.[xX*]$`)
	rangeOps = []string{">=", "<=", ">", "<", "=", "~", "^"}
)

func satisfies(info *jarmeta.Info, version, declared string) (bool, error) {
	if info.UsesMavenRanges {
		rng, err := mavenver.ParseRange(declared)
		if err != nil {
			return false, err
		}
		return rng.Contains(mavenver.Parse(version)), nil
	}
	return fabricSatisfies(version, declared)
}

func fabricSatisfies(version, declared string) (bool, error) {
	v, err := mcver.Parse(normalizeVersion(version))
	if err != nil {
		return false, fmt.Errorf("installed version %q is not semver", version)
	}
	rng, err := fabricRange(declared)
	if err != nil {
		return false, err
	}
	return rng.Contains(v), nil
}

func normalizeVersion(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i]
	}
	if bareInt.MatchString(s) {
		s += ".0"
	}
	return s
}

func fabricRange(declared string) (mcver.Range, error) {
	var alts []string
	for _, alt := range strings.Split(declared, "||") {
		var toks []string
		for _, tok := range strings.Fields(alt) {
			norm, err := normalizeToken(tok)
			if err != nil {
				return mcver.Range{}, fmt.Errorf("range %q is not understood", declared)
			}
			toks = append(toks, norm)
		}
		if len(toks) == 0 {
			toks = []string{"*"}
		}
		alts = append(alts, strings.Join(toks, " "))
	}
	rng, err := mcver.ParseRange(strings.Join(alts, " || "))
	if err != nil {
		return mcver.Range{}, fmt.Errorf("range %q is not understood", declared)
	}
	return rng, nil
}

func normalizeToken(tok string) (string, error) {
	if tok == "*" {
		return tok, nil
	}
	op := ""
	for _, candidate := range rangeOps {
		if strings.HasPrefix(tok, candidate) {
			op = candidate
			break
		}
	}
	ver := normalizeVersion(tok[len(op):])
	switch {
	case minorX.MatchString(ver):
		if op != "" {
			return "", fmt.Errorf("wildcard with operator")
		}
		major := atoi(minorX.FindStringSubmatch(ver)[1])
		return fmt.Sprintf(">=%d.0.0 <%d.0.0", major, major+1), nil
	case patchX.MatchString(ver):
		if op != "" {
			return "", fmt.Errorf("wildcard with operator")
		}
		m := patchX.FindStringSubmatch(ver)
		return fmt.Sprintf(">=%d.%d.0 <%d.%d.0", atoi(m[1]), atoi(m[2]), atoi(m[1]), atoi(m[2])+1), nil
	}
	if _, err := mcver.Parse(ver); err != nil {
		return "", err
	}
	return op + ver, nil
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
