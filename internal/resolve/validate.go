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
}

var suggestionKinds = []string{"recommends", "suggests", "optional"}

// Recommended leaves optional dependencies out: they are mostly integrations and would bury the rest.
func (v *Validation) Recommended() []string {
	lines := []string{}
	for _, s := range v.Suggestions {
		if s.Kind != "optional" {
			lines = append(lines, fmt.Sprintf("%s %s %s", s.Mod, s.Kind, s.On))
		}
	}
	return lines
}

func builtin(id string) bool {
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

func (r *Resolver) Validate() (*Validation, error) {
	v := &Validation{Problems: []Problem{}, Warnings: []string{}, Suggestions: []Suggestion{}}
	installed := map[string]string{"minecraft": r.Lock.Minecraft, "java": fmt.Sprintf("%d.0", r.Lock.Java.Major)}
	if l, ok := loader.Lookup(r.Lock.Loader.Type); ok {
		installed[l.DependencyID] = r.Lock.Loader.Version
	}
	for id, version := range r.Lock.Loader.Provides {
		installed[id] = version
	}
	infos := map[string]*jarmeta.Info{}
	for _, id := range r.lockIDs() {
		m := r.Lock.Mods[id]
		if !r.Cache.Has(m.Sha512) {
			v.Warnings = append(v.Warnings, fmt.Sprintf("%s is not downloaded; its metadata was not checked", id))
			continue
		}
		info, err := jarmeta.Read(r.Cache.Object(m.Sha512), r.Lock.Loader.Type)
		if err != nil {
			return nil, err
		}
		infos[id] = info
		installed[r.Lock.JarID(id)] = info.Version
		for pid, pv := range info.Provides {
			if _, taken := installed[pid]; !taken {
				installed[pid] = pv
			}
		}
	}
	ignores := r.ignores()
	used := map[int]bool{}
	for _, id := range sortedKeys(infos) {
		info := infos[id]
		for _, on := range sortedKeys(info.Depends) {
			declared := info.Depends[on]
			found, ok := installed[on]
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
				v.Suggestions = append(v.Suggestions, Suggestion{Mod: id, Kind: "optional", On: on, Declared: declared})
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
					v.Suggestions = append(v.Suggestions, Suggestion{Mod: id, Kind: kind, On: on, Declared: set[on]})
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
		if p.Rule == "depends" && p.Found == "" && !builtin(p.On) {
			fmt.Fprintf(&b, "\n      Fix: shulker add %s", p.On)
			row.Children = append(row.Children, out.Detail{Label: "Fix", Text: "shulker add " + p.On, Command: true})
		}
		ignore := p.ignoreCommand()
		fmt.Fprintf(&b, "\n      Ignore: %s", ignore)
		row.Children = append(row.Children, out.Detail{Label: "Ignore", Text: ignore, Command: true})
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
	if info.MavenRanges {
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
