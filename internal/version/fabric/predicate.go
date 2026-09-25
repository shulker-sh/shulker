package fabric

import (
	"fmt"
	"strings"
)

type operator int

const (
	greaterEqual operator = iota
	lessEqual
	greater
	less
	equal
	sameToNextMinor
	sameToNextMajor
)

// operators are in the order Fabric Loader tries them, so ">=" is matched before ">".
var operators = []struct {
	text string
	op   operator
}{{">=", greaterEqual}, {"<=", lessEqual}, {">", greater}, {"<", less}, {"=", equal}, {"~", sameToNextMinor}, {"^", sameToNextMajor}}

func (o operator) inclusive() bool { return o != greater && o != less }

type term struct {
	op  operator
	ref Version
}

// Predicate is a space-separated list of terms, every one of which a version must meet.
type Predicate []term

// ParsePredicate reads a predicate as Fabric Loader's VersionPredicateParser does. An X-range
// becomes the bounds it stands for, and a term naming a string version matches it by equality.
func ParsePredicate(s string) (Predicate, error) {
	p := Predicate{}
	if s == "" || s == "*" {
		return p, nil
	}
	for _, part := range strings.Split(s, " ") {
		part = strings.TrimSpace(part)
		if part == "" || part == "*" {
			continue
		}
		op := equal
		for _, o := range operators {
			if strings.HasPrefix(part, o.text) {
				op, part = o.op, part[len(o.text):]
				break
			}
		}
		ref, err := parseSemantic(part, true)
		if err != nil {
			if part == "" {
				return nil, fmt.Errorf("invalid predicate %q: a term has no version", s)
			}
			if !op.inclusive() {
				return nil, fmt.Errorf("invalid predicate %q: a bound that excludes itself needs a semantic version", s)
			}
			p = append(p, term{equal, Version{raw: part}})
			continue
		}
		if !ref.hasWildcard() {
			p = append(p, term{op, ref})
			continue
		}
		if op != equal {
			return nil, fmt.Errorf("invalid predicate %q: a wildcard (.x) takes no operator", s)
		}
		empty := ""
		core := ref.components[:len(ref.components)-1]
		lower := Version{raw: part, semantic: true, components: core, prerelease: &empty}
		switch len(ref.components) {
		case 2:
			p = append(p, term{sameToNextMajor, lower})
		case 3:
			p = append(p, term{sameToNextMinor, lower})
		default:
			upper := append([]int(nil), core...)
			upper[len(upper)-1]++
			p = append(p, term{greaterEqual, lower}, term{less, Version{raw: part, semantic: true, components: upper, prerelease: &empty}})
		}
	}
	return p, nil
}

// Test reports whether v meets every term.
func (p Predicate) Test(v Version) bool {
	for _, t := range p {
		if !t.test(v) {
			return false
		}
	}
	return true
}

func (t term) test(v Version) bool {
	if !v.semantic || !t.ref.semantic {
		return t.op.inclusive() && v.friendly() == t.ref.friendly()
	}
	c := Compare(v, t.ref)
	switch t.op {
	case greaterEqual:
		return c >= 0
	case lessEqual:
		return c <= 0
	case greater:
		return c > 0
	case less:
		return c < 0
	case equal:
		return c == 0
	case sameToNextMinor:
		return c >= 0 && v.component(0) == t.ref.component(0) && v.component(1) == t.ref.component(1)
	}
	return c >= 0 && v.component(0) == t.ref.component(0)
}
