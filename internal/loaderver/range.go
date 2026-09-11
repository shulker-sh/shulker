package loaderver

import (
	"fmt"
	"sort"
	"strings"
)

type op int

const (
	opEq op = iota
	opGt
	opGte
	opLt
	opLte
)

type comparator struct {
	op op
	v  Version
}

// Range uses the same syntax as Minecraft ranges: *, ~v, ^v, >=, <=, >, <, =, a space for "and", || for "or".
// A prerelease matches only when a comparator in the same set names a prerelease of the same core version.
type Range struct {
	Raw  string
	sets [][]comparator
}

func ParseRange(raw string) (Range, error) {
	r := Range{Raw: raw}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "*" {
		return r, nil
	}
	for _, alt := range strings.Split(trimmed, "||") {
		var set []comparator
		for _, tok := range strings.Fields(alt) {
			cs, err := parseComparator(tok)
			if err != nil {
				return Range{}, fmt.Errorf("range %q: %w", raw, err)
			}
			set = append(set, cs...)
		}
		if len(set) == 0 {
			return Range{}, fmt.Errorf("range %q: empty alternative", raw)
		}
		r.sets = append(r.sets, set)
	}
	return r, nil
}

func parseComparator(tok string) ([]comparator, error) {
	for _, p := range []struct {
		prefix string
		op     op
	}{{">=", opGte}, {"<=", opLte}, {">", opGt}, {"<", opLt}, {"=", opEq}} {
		if rest, ok := strings.CutPrefix(tok, p.prefix); ok {
			v, err := Parse(rest)
			return []comparator{{p.op, v}}, err
		}
	}
	switch {
	case tok == "*":
		return nil, nil
	case strings.HasPrefix(tok, "~"):
		v, err := Parse(tok[1:])
		if err != nil {
			return nil, err
		}
		return []comparator{{opGte, v}, {opLt, bump(v, min(1, len(v.Parts)-1))}}, nil
	case strings.HasPrefix(tok, "^"):
		v, err := Parse(tok[1:])
		if err != nil {
			return nil, err
		}
		i := len(v.Parts) - 1
		for j, p := range v.Parts {
			if p != 0 {
				i = j
				break
			}
		}
		return []comparator{{opGte, v}, {opLt, bump(v, i)}}, nil
	}
	v, err := Parse(tok)
	return []comparator{{opEq, v}}, err
}

func bump(v Version, i int) Version {
	parts := make([]int, i+1)
	copy(parts, v.Parts[:i+1])
	parts[i]++
	return Version{Parts: parts}
}

func (r Range) IsAny() bool { return len(r.sets) == 0 }

func (r Range) Matches(v Version) bool {
	if r.IsAny() {
		return v.IsRelease()
	}
	for _, set := range r.sets {
		if matchSet(set, v) {
			return true
		}
	}
	return false
}

func matchSet(set []comparator, v Version) bool {
	for _, c := range set {
		if !c.matches(v) {
			return false
		}
	}
	if v.IsRelease() {
		return true
	}
	for _, c := range set {
		if !c.v.IsRelease() && compareCore(c.v, v) == 0 {
			return true
		}
	}
	return false
}

func (c comparator) matches(v Version) bool {
	cmp := Compare(v, c.v)
	switch c.op {
	case opEq:
		return cmp == 0
	case opGt:
		return cmp > 0
	case opGte:
		return cmp >= 0
	case opLt:
		return cmp < 0
	case opLte:
		return cmp <= 0
	}
	return false
}

func Newest(candidates []Version, r Range) (Version, bool) {
	var matched []Version
	for _, v := range candidates {
		if r.Matches(v) {
			matched = append(matched, v)
		}
	}
	if len(matched) == 0 {
		return Version{}, false
	}
	sort.SliceStable(matched, func(i, j int) bool { return Compare(matched[i], matched[j]) > 0 })
	return matched[0], true
}
