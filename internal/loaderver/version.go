// Package loaderver compares mod loader versions, which are dotted numbers of any length with an
// optional semver-style prerelease and build suffix: 0.17.3, 0.31.0-beta.4, 26.2.0.87,
// 26.1.0.0-alpha.11+snapshot-7, 65.1.3.
package loaderver

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type Version struct {
	ID    string
	Parts []int
	Pre   string
}

var versionRe = regexp.MustCompile(`^(\d+(?:\.\d+)*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)

func Parse(id string) (Version, error) {
	id = strings.TrimSpace(id)
	m := versionRe.FindStringSubmatch(id)
	if m == nil {
		return Version{}, fmt.Errorf("unrecognised loader version %q", id)
	}
	v := Version{ID: id, Pre: m[2]}
	for _, p := range strings.Split(m[1], ".") {
		n, err := strconv.Atoi(p)
		if err != nil {
			return Version{}, fmt.Errorf("unrecognised loader version %q", id)
		}
		v.Parts = append(v.Parts, n)
	}
	return v, nil
}

func (v Version) IsRelease() bool { return v.Pre == "" }

func (v Version) String() string { return v.ID }

func (v Version) part(i int) int {
	if i < len(v.Parts) {
		return v.Parts[i]
	}
	return 0
}

func Compare(a, b Version) int {
	if c := compareCore(a, b); c != 0 {
		return c
	}
	switch {
	case a.Pre == b.Pre:
		return 0
	case a.Pre == "":
		return 1
	case b.Pre == "":
		return -1
	}
	return comparePrerelease(a.Pre, b.Pre)
}

func compareCore(a, b Version) int {
	n := max(len(a.Parts), len(b.Parts))
	for i := range n {
		if x, y := a.part(i), b.part(i); x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

func comparePrerelease(a, b string) int {
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
