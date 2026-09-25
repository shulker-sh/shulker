// Package fabric parses, orders and matches versions the way Fabric Loader does: a semantic
// version has any number of numeric components, and anything else is a plain string version that
// only equality can match.
package fabric

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Version is a version as Fabric Loader reads it from a mod's metadata.
type Version struct {
	raw        string
	semantic   bool
	components []int
	// prerelease is nil for none: "1.2-" has an empty one, which sorts below every other.
	prerelease *string
	build      *string
}

var (
	dotSeparatedID  = regexp.MustCompile(`^(|[-0-9A-Za-z]+(\.[-0-9A-Za-z]+)*)$`)
	unsignedInteger = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)
)

// Parse reads s as a semantic version, or as a string version when it isn't one.
func Parse(s string) Version {
	v, err := parseSemantic(s, false)
	if err != nil {
		return Version{raw: s}
	}
	return v
}

// IsSemantic reports whether the version parsed as semantic rather than falling back to a string.
func (v Version) IsSemantic() bool { return v.semantic }

func (v Version) String() string { return v.raw }

const wildcard = -1

func parseSemantic(s string, allowWildcard bool) (Version, error) {
	if s == "" {
		return Version{}, errors.New("version must be a non-empty string")
	}
	v := Version{raw: s, semantic: true}
	if i := strings.IndexByte(s, '+'); i >= 0 {
		build := s[i+1:]
		v.build, s = &build, s[:i]
	}
	if i := strings.IndexByte(s, '-'); i >= 0 {
		pre := s[i+1:]
		v.prerelease, s = &pre, s[:i]
		if !dotSeparatedID.MatchString(pre) {
			return Version{}, fmt.Errorf("invalid prerelease string %q", pre)
		}
	}
	if strings.HasPrefix(s, ".") || strings.HasSuffix(s, ".") {
		return Version{}, errors.New("missing version component")
	}
	firstWildcard := -1
	for i, part := range strings.Split(s, ".") {
		if allowWildcard && (part == "x" || part == "X" || part == "*") {
			if v.prerelease != nil {
				return Version{}, errors.New("pre-release versions are not allowed to use X-ranges")
			}
			v.components = append(v.components, wildcard)
			if firstWildcard < 0 {
				firstWildcard = i
			}
			continue
		}
		if firstWildcard >= 0 {
			return Version{}, errors.New("interjacent wildcards (1.x.2) are disallowed")
		}
		n, err := strconv.ParseInt(part, 10, 32)
		if err != nil || n < 0 {
			return Version{}, fmt.Errorf("could not parse version number component %q", part)
		}
		v.components = append(v.components, int(n))
	}
	if firstWildcard == 0 {
		return Version{}, errors.New("versions of form 'x' or 'X' are not allowed")
	}
	if firstWildcard > 0 {
		v.components = v.components[:firstWildcard+1]
	}
	return v, nil
}

func (v Version) component(i int) int {
	if i < len(v.components) {
		return v.components[i]
	}
	if v.components[len(v.components)-1] == wildcard {
		return wildcard
	}
	return 0
}

func (v Version) hasWildcard() bool {
	return len(v.components) > 0 && v.components[len(v.components)-1] == wildcard
}

// friendly is the version as Fabric Loader writes it back, the form string versions compare by.
func (v Version) friendly() string {
	if !v.semantic {
		return v.raw
	}
	parts := make([]string, len(v.components))
	for i, c := range v.components {
		parts[i] = strconv.Itoa(c)
		if c == wildcard {
			parts[i] = "x"
		}
	}
	s := strings.Join(parts, ".")
	if v.prerelease != nil {
		s += "-" + *v.prerelease
	}
	if v.build != nil {
		s += "+" + *v.build
	}
	return s
}

// Compare orders a and b: component by component, a missing one counting as 0, then by
// prerelease, where having none sorts highest. A string version compares by its text.
func Compare(a, b Version) int {
	if !a.semantic || !b.semantic {
		return strings.Compare(a.friendly(), b.friendly())
	}
	for i := range max(len(a.components), len(b.components)) {
		x, y := a.component(i), b.component(i)
		if x == wildcard || y == wildcard {
			continue
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	switch {
	case a.prerelease == nil && b.prerelease == nil:
		return 0
	case b.prerelease == nil:
		if b.hasWildcard() {
			return 0
		}
		return -1
	case a.prerelease == nil:
		if a.hasWildcard() {
			return 0
		}
		return 1
	}
	return comparePrerelease(*a.prerelease, *b.prerelease)
}

// comparePrerelease follows Fabric Loader, which ranks numeric parts by length before value and
// any numeric part below any other.
func comparePrerelease(a, b string) int {
	as, bs := tokens(a), tokens(b)
	for i, x := range as {
		if i >= len(bs) {
			return 1
		}
		y := bs[i]
		xNum, yNum := unsignedInteger.MatchString(x), unsignedInteger.MatchString(y)
		switch {
		case xNum && yNum:
			if len(x) != len(y) {
				if len(x) < len(y) {
					return -1
				}
				return 1
			}
		case xNum:
			return -1
		case yNum:
			return 1
		}
		if c := strings.Compare(x, y); c != 0 {
			return c
		}
	}
	if len(bs) > len(as) {
		return -1
	}
	return 0
}

// tokens splits like Java's StringTokenizer, which skips empty tokens.
func tokens(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == '.' })
}
