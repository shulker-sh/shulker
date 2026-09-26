package resolve

import "shulker.sh/shulker/internal/jarmeta"

// Where a locked mod's side came from, the lock's sideFrom. The default both has none.
const (
	sideFromRequires     = "requires"
	sideFromProvider     = "provider"
	sideFromJar          = "jar"
	sideFromDependencies = "dependencies"
)

// jarSide is the side info's metadata gives the mod, and where it came from.
func jarSide(info *jarmeta.Info) (side, from string) {
	switch {
	case info.SideFromDependencies:
		return info.Side, sideFromDependencies
	case info.Side != "" && info.Side != "both":
		return info.Side, sideFromJar
	}
	return "both", ""
}
