// Package mcver orders Minecraft versions: releases, their snapshots, pre-releases and release
// candidates, and weekly snapshots such as 24w14a, which sort as snapshots of the release their cycle
// led to.
package mcver

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"shulker.sh/shulker/internal/version"
)

// Kind is what a version is relative to its release. The constants are in the order they sort.
type Kind int

const (
	Snapshot Kind = iota
	Pre
	RC
	// Other is a tag shulker doesn't recognise, ordered by the tag itself.
	Other
	Release
)

type Version struct {
	ID    string
	Major int
	Minor int
	Patch int
	Kind  Kind
	// Num orders versions of one Kind: the snapshot, pre or rc number, year*100+week for a weekly
	// snapshot, and -1 for a bound below every snapshot of its release.
	Num int
	Tag string
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
	switch {
	case strings.HasSuffix(id, "-"):
		v.Kind, v.Num = Snapshot, -1
	case m[4] != "":
		v.Tag = m[4]
		v.Kind, v.Num = tagKind(m[4])
	}
	return v, nil
}

func tagKind(tag string) (Kind, int) {
	t := taggedRe.FindStringSubmatch(tag)
	if t == nil {
		return Other, 0
	}
	num, _ := strconv.Atoi(t[2])
	switch t[1] {
	case "snapshot":
		return Snapshot, num
	case "pre":
		return Pre, num
	}
	return RC, num
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
		return version.ComparePrerelease(a.Tag, b.Tag)
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
