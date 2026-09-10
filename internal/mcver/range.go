package mcver

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
	switch {
	case tok == "*":
		return nil, nil
	case strings.HasPrefix(tok, "~"):
		v, err := parseVersion(tok[1:])
		if err != nil {
			return nil, err
		}
		upper := Version{Major: v.Major, Minor: v.Minor + 1, Kind: Snapshot, Num: -1}
		return []comparator{{opGte, v}, {opLt, upper}}, nil
	case strings.HasPrefix(tok, "^"):
		v, err := parseVersion(tok[1:])
		if err != nil {
			return nil, err
		}
		var upper Version
		switch {
		case v.Major > 0:
			upper = Version{Major: v.Major + 1, Kind: Snapshot, Num: -1}
		case v.Minor > 0:
			upper = Version{Minor: v.Minor + 1, Kind: Snapshot, Num: -1}
		default:
			upper = Version{Patch: v.Patch + 1, Kind: Snapshot, Num: -1}
		}
		return []comparator{{opGte, v}, {opLt, upper}}, nil
	case strings.HasPrefix(tok, ">="):
		v, err := parseVersion(tok[2:])
		return []comparator{{opGte, v}}, err
	case strings.HasPrefix(tok, "<="):
		v, err := parseVersion(tok[2:])
		return []comparator{{opLte, v}}, err
	case strings.HasPrefix(tok, ">"):
		v, err := parseVersion(tok[1:])
		return []comparator{{opGt, v}}, err
	case strings.HasPrefix(tok, "<"):
		v, err := parseVersion(tok[1:])
		return []comparator{{opLt, v}}, err
	case strings.HasPrefix(tok, "="):
		v, err := parseVersion(tok[1:])
		return []comparator{{opEq, v}}, err
	default:
		v, err := parseVersion(tok)
		return []comparator{{opEq, v}}, err
	}
}

func parseVersion(s string) (Version, error) {
	v, err := Parse(s)
	if err != nil {
		return Version{}, err
	}
	return v, nil
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
		if !c.v.IsRelease() && c.v.core() == v.core() {
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
	sort.Slice(matched, func(i, j int) bool { return Compare(matched[i], matched[j]) > 0 })
	return matched[0], true
}

func (r Range) Contains(v Version) bool {
	if r.IsAny() {
		return true
	}
	for _, set := range r.sets {
		all := true
		for _, c := range set {
			if !c.matches(v) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}
