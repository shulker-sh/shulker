package mcver

import (
	"fmt"
	"regexp"
	"strconv"
)

var (
	dateBasedRe = regexp.MustCompile(`^(\d{2}\.\d+(?:\.\d+)?)-(snapshot|pre|rc)-(\d+)$`)
	legacyRe    = regexp.MustCompile(`^(1\.\d+(?:\.\d+)?)-(pre|rc)(\d+)$`)
)

// FabricForm is the semantic version Fabric Loader's McVersionLookup gives the game, which is
// what a mod's minecraft range is matched against: 24w33a is 1.21.2-alpha.24.33.a, and 1.21-pre1
// is 1.21-beta.1. An id it doesn't recognise comes back unchanged.
func FabricForm(id string) string {
	if m := weeklyRe.FindStringSubmatch(id); m != nil {
		year, _ := strconv.Atoi(m[1])
		week, _ := strconv.Atoi(m[2])
		release, ok := weeklyRelease(year*100 + week)
		if !ok {
			return id
		}
		return fmt.Sprintf("%s-alpha.%d.%d.%s", release, year, week, m[3])
	}
	if m := dateBasedRe.FindStringSubmatch(id); m != nil {
		kind := m[2]
		if kind == "snapshot" {
			kind = "alpha"
		}
		return m[1] + "-" + kind + "." + m[3]
	}
	m := legacyRe.FindStringSubmatch(id)
	if m == nil {
		return id
	}
	release, build := MustParse(m[1]), m[3]
	legacy := Compare(release, MustParse("1.16")) <= 0
	switch {
	case m[2] == "pre" && legacy:
		return m[1] + "-rc." + build
	case m[2] == "pre":
		return m[1] + "-beta." + build
	case m[1] == "1.16":
		// Fabric numbers 1.16's release candidates on from its eight pre-releases.
		n, _ := strconv.Atoi(build)
		return m[1] + "-rc." + strconv.Itoa(8+n)
	}
	return m[1] + "-rc." + build
}
