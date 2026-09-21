// Package verrange is the version range syntax Minecraft and loader versions share: *, ~v, ^v, >=,
// <=, >, <, =, a space for "and", || for "or". Each version kind brings its own ordering and its own
// upper bounds for ~ and ^.
package verrange

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Version is what a range needs from a version kind.
type Version[V any] interface {
	Compare(V) int
	IsRelease() bool
	// SameCore reports whether two versions differ only in their prerelease.
	SameCore(V) bool
	// TildeUpper and CaretUpper are the exclusive upper bounds of ~v and ^v.
	TildeUpper() V
	CaretUpper() V
}

type op int

const (
	opEq op = iota
	opGt
	opGte
	opLt
	opLte
)

type comparator[V Version[V]] struct {
	op op
	v  V
}

// Range is a parsed range. A prerelease matches only when a comparator in the same set names a
// prerelease of the same core version. The zero Range is *.
type Range[V Version[V]] struct {
	Raw  string
	sets [][]comparator[V]
}

func Parse[V Version[V]](raw string, parse func(string) (V, error)) (Range[V], error) {
	r := Range[V]{Raw: raw}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "*" {
		return r, nil
	}
	for _, alt := range strings.Split(trimmed, "||") {
		var set []comparator[V]
		for _, tok := range strings.Fields(alt) {
			cs, err := parseComparator(tok, parse)
			if err != nil {
				return Range[V]{}, fmt.Errorf("range %q: %w", raw, err)
			}
			set = append(set, cs...)
		}
		if len(set) == 0 {
			return Range[V]{}, fmt.Errorf("range %q: empty alternative", raw)
		}
		r.sets = append(r.sets, set)
	}
	return r, nil
}

func parseComparator[V Version[V]](tok string, parse func(string) (V, error)) ([]comparator[V], error) {
	switch {
	case tok == "*":
		return nil, nil
	case strings.HasPrefix(tok, "~"):
		v, err := parse(tok[1:])
		if err != nil {
			return nil, err
		}
		return []comparator[V]{{opGte, v}, {opLt, v.TildeUpper()}}, nil
	case strings.HasPrefix(tok, "^"):
		v, err := parse(tok[1:])
		if err != nil {
			return nil, err
		}
		return []comparator[V]{{opGte, v}, {opLt, v.CaretUpper()}}, nil
	}
	for _, p := range []struct {
		prefix string
		op     op
	}{{">=", opGte}, {"<=", opLte}, {">", opGt}, {"<", opLt}, {"=", opEq}} {
		if rest, ok := strings.CutPrefix(tok, p.prefix); ok {
			v, err := parse(rest)
			return []comparator[V]{{p.op, v}}, err
		}
	}
	v, err := parse(tok)
	return []comparator[V]{{opEq, v}}, err
}

// IsAny reports whether the range is *, which Matches reads as every release and Contains as every
// version.
func (r Range[V]) IsAny() bool { return len(r.sets) == 0 }

// Matches reports whether v is in the range, where * holds only releases.
func (r Range[V]) Matches(v V) bool {
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

func matchSet[V Version[V]](set []comparator[V], v V) bool {
	for _, c := range set {
		if !c.matches(v) {
			return false
		}
	}
	if v.IsRelease() {
		return true
	}
	for _, c := range set {
		if !c.v.IsRelease() && c.v.SameCore(v) {
			return true
		}
	}
	return false
}

// Contains reports whether v satisfies every comparator of some set, prerelease or not, where *
// holds everything.
func (r Range[V]) Contains(v V) bool {
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

func (c comparator[V]) matches(v V) bool {
	cmp := v.Compare(c.v)
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

// Newest is the highest candidate the range matches.
func Newest[V Version[V]](candidates []V, r Range[V]) (V, bool) {
	var matched []V
	for _, v := range candidates {
		if r.Matches(v) {
			matched = append(matched, v)
		}
	}
	if len(matched) == 0 {
		var zero V
		return zero, false
	}
	sort.SliceStable(matched, func(i, j int) bool { return matched[i].Compare(matched[j]) > 0 })
	return matched[0], true
}

// ComparePrerelease orders two dotted prerelease tags: numeric identifiers numerically and before
// alphanumeric ones, which compare as text, and a tag that is a prefix of another first.
func ComparePrerelease(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		an, aerr := strconv.Atoi(as[i])
		bn, berr := strconv.Atoi(bs[i])
		switch {
		case aerr == nil && berr == nil:
			if an != bn {
				if an < bn {
					return -1
				}
				return 1
			}
		case aerr == nil:
			return -1
		case berr == nil:
			return 1
		default:
			if c := strings.Compare(as[i], bs[i]); c != 0 {
				return c
			}
		}
	}
	return len(as) - len(bs)
}
