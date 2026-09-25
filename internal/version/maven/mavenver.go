// Package maven orders versions and matches version ranges the way Maven's ComparableVersion
// and VersionRange do, which is how NeoForge and Forge check mod dependencies.
package maven

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

type kind int

const (
	number kind = iota
	qualifier
	list
)

type item struct {
	kind  kind
	value string
	items []*item
}

type Version struct {
	ID   string
	root *item
}

func (v Version) String() string { return v.ID }

// Parse accepts any string, as Maven does.
func Parse(id string) Version {
	s := strings.ToLower(id)
	root := &item{kind: list}
	cur := root
	stack := []*item{root}
	sublist := func() {
		l := &item{kind: list}
		cur.items = append(cur.items, l)
		cur = l
		stack = append(stack, l)
	}
	digit := false
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '.' || c == '-':
			if i == start {
				cur.items = append(cur.items, &item{kind: number, value: "0"})
			} else {
				cur.items = append(cur.items, parseItem(digit, s[start:i]))
			}
			start = i + 1
			if c == '-' {
				sublist()
			}
		case c >= '0' && c <= '9':
			if !digit && i > start {
				cur.items = append(cur.items, qualifierItem(s[start:i], true))
				start = i
				sublist()
			}
			digit = true
		default:
			if digit && i > start {
				cur.items = append(cur.items, parseItem(true, s[start:i]))
				start = i
				sublist()
			}
			digit = false
		}
	}
	if len(s) > start {
		cur.items = append(cur.items, parseItem(digit, s[start:]))
	}
	for i := len(stack) - 1; i >= 0; i-- {
		stack[i].normalize()
	}
	return Version{ID: id, root: root}
}

func parseItem(digit bool, s string) *item {
	if digit {
		s = strings.TrimLeft(s, "0")
		if s == "" {
			s = "0"
		}
		return &item{kind: number, value: s}
	}
	return qualifierItem(s, false)
}

var (
	qualifiers = []string{"alpha", "beta", "milestone", "rc", "snapshot", "", "sp"}
	aliases    = map[string]string{"ga": "", "final": "", "release": "", "cr": "rc"}
	release    = strconv.Itoa(slices.Index(qualifiers, ""))
)

func qualifierItem(s string, followedByDigit bool) *item {
	if followedByDigit && len(s) == 1 {
		switch s {
		case "a":
			s = "alpha"
		case "b":
			s = "beta"
		case "m":
			s = "milestone"
		}
	}
	if alias, ok := aliases[s]; ok {
		s = alias
	}
	return &item{kind: qualifier, value: s}
}

func qualifierRank(q string) string {
	if i := slices.Index(qualifiers, q); i >= 0 {
		return strconv.Itoa(i)
	}
	return strconv.Itoa(len(qualifiers)) + "-" + q
}

func (it *item) isNull() bool {
	switch it.kind {
	case number:
		return it.value == "0"
	case qualifier:
		return qualifierRank(it.value) == release
	}
	return len(it.items) == 0
}

func (it *item) normalize() {
	for i := len(it.items) - 1; i >= 0; i-- {
		last := it.items[i]
		if last.isNull() {
			it.items = slices.Delete(it.items, i, i+1)
		} else if last.kind != list {
			break
		}
	}
}

// compare orders a against b; a nil b stands for the missing item past the end of a shorter list.
func (it *item) compare(b *item) int {
	switch it.kind {
	case number:
		if b == nil {
			if it.isNull() {
				return 0
			}
			return 1
		}
		if b.kind != number {
			return 1
		}
		if c := len(it.value) - len(b.value); c != 0 {
			return c
		}
		return strings.Compare(it.value, b.value)
	case qualifier:
		if b == nil {
			return strings.Compare(qualifierRank(it.value), release)
		}
		if b.kind != qualifier {
			return -1
		}
		return strings.Compare(qualifierRank(it.value), qualifierRank(b.value))
	}
	if b == nil {
		if len(it.items) == 0 {
			return 0
		}
		return it.items[0].compare(nil)
	}
	switch b.kind {
	case number:
		return -1
	case qualifier:
		return 1
	}
	for i := 0; i < max(len(it.items), len(b.items)); i++ {
		var l, r *item
		if i < len(it.items) {
			l = it.items[i]
		}
		if i < len(b.items) {
			r = b.items[i]
		}
		var c int
		switch {
		case l == nil && r == nil:
		case l == nil:
			c = -r.compare(nil)
		default:
			c = l.compare(r)
		}
		if c != 0 {
			return c
		}
	}
	return 0
}

func Compare(a, b Version) int {
	c := a.root.compare(b.root)
	switch {
	case c < 0:
		return -1
	case c > 0:
		return 1
	}
	return 0
}

type restriction struct {
	lower, upper                 *Version
	includesLower, includesUpper bool
}

func (r restriction) contains(v Version) bool {
	if r.lower != nil {
		c := Compare(*r.lower, v)
		if c > 0 || c == 0 && !r.includesLower {
			return false
		}
	}
	if r.upper != nil {
		c := Compare(*r.upper, v)
		if c < 0 || c == 0 && !r.includesUpper {
			return false
		}
	}
	return true
}

// Range is a Maven version range: `[1.0,2.0)`, `(,1.0]`, `[1.2]`, or a union such as
// `[1,2),[3,)`. A bare version is Maven's soft requirement and matches everything.
type Range struct {
	restrictions []restriction
	matchesAll   bool
}

// ParseRange reads a Maven range. * and a bare version both match everything, as Maven's soft
// requirement does.
func ParseRange(spec string) (Range, error) {
	rest := strings.TrimSpace(spec)
	if rest == "" || rest == "*" {
		return Range{matchesAll: true}, nil
	}
	var r Range
	var upper *Version
	for strings.HasPrefix(rest, "[") || strings.HasPrefix(rest, "(") {
		end := strings.IndexAny(rest, ")]")
		if end < 0 {
			return Range{}, fmt.Errorf("range %q is unbounded", spec)
		}
		res, err := parseRestriction(rest[:end+1])
		if err != nil {
			return Range{}, fmt.Errorf("range %q: %w", spec, err)
		}
		if upper != nil && (res.lower == nil || Compare(*res.lower, *upper) < 0) {
			return Range{}, fmt.Errorf("range %q has overlapping sets", spec)
		}
		r.restrictions = append(r.restrictions, res)
		upper = res.upper
		rest = strings.TrimSpace(rest[end+1:])
		rest = strings.TrimSpace(strings.TrimPrefix(rest, ","))
	}
	if rest != "" {
		if len(r.restrictions) > 0 {
			return Range{}, fmt.Errorf("range %q mixes a bare version with sets", spec)
		}
		return Range{matchesAll: true}, nil
	}
	return r, nil
}

func parseRestriction(spec string) (restriction, error) {
	res := restriction{includesLower: spec[0] == '[', includesUpper: spec[len(spec)-1] == ']'}
	body := strings.TrimSpace(spec[1 : len(spec)-1])
	lower, upper, found := strings.Cut(body, ",")
	if !found {
		if !res.includesLower || !res.includesUpper {
			return restriction{}, fmt.Errorf("a single version must be written [%s]", body)
		}
		v := Parse(body)
		res.lower, res.upper = &v, &v
		return res, nil
	}
	lower, upper = strings.TrimSpace(lower), strings.TrimSpace(upper)
	if strings.Contains(upper, ",") {
		return restriction{}, fmt.Errorf("%s has more than two bounds", spec)
	}
	if lower != "" {
		v := Parse(lower)
		res.lower = &v
	}
	if upper != "" {
		v := Parse(upper)
		res.upper = &v
	}
	if res.lower != nil && res.upper != nil && Compare(*res.upper, *res.lower) < 0 {
		return restriction{}, fmt.Errorf("%s has its bounds reversed", spec)
	}
	return res, nil
}

func (r Range) Contains(v Version) bool {
	if r.matchesAll {
		return true
	}
	for _, res := range r.restrictions {
		if res.contains(v) {
			return true
		}
	}
	return false
}
