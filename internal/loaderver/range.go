package loaderver

import "shulker.sh/shulker/internal/verrange"

type Range = verrange.Range[Version]

func ParseRange(raw string) (Range, error) { return verrange.Parse(raw, Parse) }

func Newest(candidates []Version, r Range) (Version, bool) { return verrange.Newest(candidates, r) }

func (v Version) Compare(o Version) int { return Compare(v, o) }

func (v Version) SameCore(o Version) bool { return compareCore(v, o) == 0 }

// TildeUpper bumps the minor part, or the only part there is.
func (v Version) TildeUpper() Version { return bump(v, min(1, len(v.Parts)-1)) }

// CaretUpper bumps the first part that isn't zero, or the last part when all are.
func (v Version) CaretUpper() Version {
	i := len(v.Parts) - 1
	for j, p := range v.Parts {
		if p != 0 {
			i = j
			break
		}
	}
	return bump(v, i)
}

func bump(v Version, i int) Version {
	parts := make([]int, i+1)
	copy(parts, v.Parts[:i+1])
	parts[i]++
	return Version{Parts: parts}
}
