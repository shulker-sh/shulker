package mcver

import "shulker.sh/shulker/internal/verrange"

type Range = verrange.Range[Version]

func ParseRange(raw string) (Range, error) { return verrange.Parse(raw, Parse) }

func Newest(candidates []Version, r Range) (Version, bool) { return verrange.Newest(candidates, r) }

func (v Version) Compare(o Version) int { return Compare(v, o) }

func (v Version) SameCore(o Version) bool { return v.core() == o.core() }

// TildeUpper is the first snapshot of the next minor version.
func (v Version) TildeUpper() Version {
	return Version{Major: v.Major, Minor: v.Minor + 1, Kind: Snapshot, Num: -1}
}

// CaretUpper is the first snapshot after the first part that isn't zero.
func (v Version) CaretUpper() Version {
	switch {
	case v.Major > 0:
		return Version{Major: v.Major + 1, Kind: Snapshot, Num: -1}
	case v.Minor > 0:
		return Version{Minor: v.Minor + 1, Kind: Snapshot, Num: -1}
	}
	return Version{Patch: v.Patch + 1, Kind: Snapshot, Num: -1}
}
