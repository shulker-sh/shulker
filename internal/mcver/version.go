package mcver

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type Kind int

const (
	Snapshot Kind = iota
	Pre
	RC
	Other
	Release
)

type Version struct {
	ID    string
	Major int
	Minor int
	Patch int
	Kind  Kind
	Num   int
	Tag   string
}

var (
	coreRe   = regexp.MustCompile(`^(\d+)\.(\d+)(?:\.(\d+))?(?:-(.*))?$`)
	weeklyRe = regexp.MustCompile(`^(\d\d)w(\d\d)([a-z])$`)
	taggedRe = regexp.MustCompile(`^(snapshot|pre|rc)-?(\d+)$`)
)

func Parse(id string) (Version, error) {
	id = strings.TrimSpace(id)
	if m := weeklyRe.FindStringSubmatch(id); m != nil {
		year, _ := strconv.Atoi(m[1])
		week, _ := strconv.Atoi(m[2])
		release, ok := weeklyRelease(year*100 + week)
		if !ok {
			return Version{}, fmt.Errorf("weekly snapshot %q has no known release", id)
		}
		v := MustParse(release)
		v.ID, v.Kind, v.Num, v.Tag = id, Snapshot, year*100+week, id
		return v, nil
	}
	m := coreRe.FindStringSubmatch(id)
	if m == nil {
		return Version{}, fmt.Errorf("unrecognised version %q", id)
	}
	v := Version{ID: id, Kind: Release}
	v.Major, _ = strconv.Atoi(m[1])
	v.Minor, _ = strconv.Atoi(m[2])
	if m[3] != "" {
		v.Patch, _ = strconv.Atoi(m[3])
	}
	if strings.HasSuffix(id, "-") {
		v.Kind, v.Num = Snapshot, -1
	} else if m[4] != "" {
		v.Tag = m[4]
		if t := taggedRe.FindStringSubmatch(m[4]); t != nil {
			v.Num, _ = strconv.Atoi(t[2])
			switch t[1] {
			case "snapshot":
				v.Kind = Snapshot
			case "pre":
				v.Kind = Pre
			case "rc":
				v.Kind = RC
			}
		} else {
			v.Kind = Other
		}
	}
	return v, nil
}

func MustParse(id string) Version {
	v, err := Parse(id)
	if err != nil {
		panic(err)
	}
	return v
}

func (v Version) IsRelease() bool { return v.Kind == Release }

func (v Version) String() string { return v.ID }

func (v Version) core() [3]int { return [3]int{v.Major, v.Minor, v.Patch} }

func Compare(a, b Version) int {
	if c := compareInts(a.core(), b.core()); c != 0 {
		return c
	}
	if a.Kind != b.Kind {
		if a.Kind < b.Kind {
			return -1
		}
		return 1
	}
	switch a.Kind {
	case Release:
		return 0
	case Other:
		return comparePrerelease(a.Tag, b.Tag)
	}
	if a.Num != b.Num {
		if a.Num < b.Num {
			return -1
		}
		return 1
	}
	return strings.Compare(a.Tag, b.Tag)
}

func compareInts(a, b [3]int) int {
	for i := range a {
		if a[i] != b[i] {
			if a[i] < b[i] {
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
