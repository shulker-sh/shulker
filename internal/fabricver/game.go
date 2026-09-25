package fabricver

import (
	"fmt"
	"regexp"
	"strconv"

	"shulker.sh/shulker/internal/version/minecraft"
)

var (
	weeklyRe    = regexp.MustCompile(`^(\d\d)w(\d\d)([a-z])$`)
	dateBasedRe = regexp.MustCompile(`^(\d{2}\.\d+(?:\.\d+)?)-(snapshot|pre|rc)-(\d+)$`)
	legacyRe    = regexp.MustCompile(`^(1\.\d+(?:\.\d+)?)-(pre|rc)(\d+)$`)
)

// Game is the semantic version Fabric Loader's McVersionLookup gives the game, which is what a
// mod's minecraft range is matched against: 24w33a is 1.21.2-alpha.24.33.a, and 1.21-pre1 is
// 1.21-beta.1. An id it doesn't recognise comes back unchanged.
func Game(id string) string {
	if m := weeklyRe.FindStringSubmatch(id); m != nil {
		release, ok := minecraft.WeeklyRelease(id)
		if !ok {
			return id
		}
		year, _ := strconv.Atoi(m[1])
		week, _ := strconv.Atoi(m[2])
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
	release, build := minecraft.MustParse(m[1]), m[3]
	legacy := minecraft.Compare(release, minecraft.MustParse("1.16")) <= 0
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
