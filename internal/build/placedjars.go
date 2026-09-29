package build

import (
	"fmt"
	"path"
	"strings"

	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/security"
)

// ChangedJar is a jar shulker placed in mods/ whose bytes changed after it was placed. A
// self-updating mod, a hand edit and malware all look the same from here, so the build keeps it
// and says so, without guessing why.
type ChangedJar struct {
	Path string `json:"path"`
	// Key is the lock entry the jar was placed for; empty for the marker jar or an override.
	Key string `json:"key,omitempty"`
}

func isPlacedJar(rel string) bool {
	return path.Dir(rel) == "mods" && strings.EqualFold(path.Ext(rel), ".jar")
}

// modKey is the lock key of the mod placed at rel, or empty when no locked mod is.
func (b *Builder) modKey(rel string) string {
	for key, m := range b.Lock.Mods {
		if "mods/"+m.Filename == rel {
			return key
		}
	}
	return ""
}

func changedJarsWarning(jars []ChangedJar, force string) out.SecurityWarning {
	var rows, keys []string
	for _, j := range jars {
		rows = append(rows, j.Path)
		if j.Key != "" {
			keys = append(keys, j.Key)
		}
	}
	headline := "1 jar no longer matches the copy shulker locked; the build kept it as it is."
	them, copies := "it", "copy"
	if len(jars) > 1 {
		headline = fmt.Sprintf("%d jars no longer match the copies shulker locked; the build kept them as they are.", len(jars))
		them, copies = "them", "copies"
	}
	var nudges []out.Nudge
	if len(keys) > 0 {
		nudges = append(nudges, out.Nudge{Lead: "Look at " + them, Command: "shulker audit " + strings.Join(keys, " ")})
	}
	if force != "" {
		nudges = append(nudges, out.Nudge{Lead: "Put the locked " + copies + " back", Command: force})
	}
	return security.Warn(security.PlacedJars, headline+"\n"+strings.Join(rows, "\n"), jars, nudges...)
}
