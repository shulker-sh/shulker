package game

import (
	"os"
	"strings"
)

// legacyJVM is what a version from before the arguments list existed left unsaid: every launcher
// hard-codes these two for it.
var legacyJVM = []Argument{
	{Values: []string{"-Djava.library.path=${natives_directory}"}},
	{Values: []string{"-cp", "${classpath}"}},
}

// Vars are the substitutions a launch makes that don't depend on who is playing. The account's
// own — its name, uuid, access token and user type — are added on top when the game is started,
// and never appear in a dry run.
func (a Assembly) Vars(s Store, launcher, version, gameDir, nativesDir string) map[string]string {
	assetIndex := ""
	if a.Version.AssetIndex != nil {
		assetIndex = a.Version.AssetIndex.ID
	}
	return map[string]string{
		"version_name":        a.Version.ID,
		"version_type":        a.Version.Type,
		"game_directory":      gameDir,
		"natives_directory":   nativesDir,
		"assets_root":         s.Assets(),
		"game_assets":         s.Assets(),
		"assets_index_name":   assetIndex,
		"classpath":           strings.Join(a.Classpath(s), string(os.PathListSeparator)),
		"classpath_separator": string(os.PathListSeparator),
		"library_directory":   s.Libraries(),
		"launcher_name":       launcher,
		"launcher_version":    version,
		"user_properties":     "{}",
	}
}

// Args templates the version's jvm and game arguments for one launch. The game arguments carry
// the session's access token, so what comes back is never printed, logged or recorded.
func Args(v Version, p Platform, features map[string]bool, vars map[string]string) (jvm, game []string) {
	args := v.Arguments
	if args == nil {
		args = &Arguments{Game: v.gameArguments()}
	}
	jvmArgs := args.JVM
	if len(jvmArgs) == 0 {
		jvmArgs = legacyJVM
	}
	return expand(jvmArgs, p, features, vars), expand(args.Game, p, features, vars)
}

func expand(args []Argument, p Platform, features map[string]bool, vars map[string]string) []string {
	var out []string
	for _, a := range args {
		if !Allows(a.Rules, p, features) {
			continue
		}
		for _, value := range a.Values {
			out = append(out, substitute(value, vars))
		}
	}
	return out
}

// substitute fills the ${name} placeholders it has values for and leaves the rest as they are,
// which is what the Mojang launcher does with a placeholder it doesn't know.
func substitute(value string, vars map[string]string) string {
	if !strings.Contains(value, "${") {
		return value
	}
	for name, v := range vars {
		value = strings.ReplaceAll(value, "${"+name+"}", v)
	}
	return value
}

// Declares reports whether any of the version's game arguments is gated on the launcher feature,
// which is how a launcher learns what the version understands.
func (v Version) Declares(feature string) bool {
	if v.Arguments == nil {
		return false
	}
	for _, a := range v.Arguments.Game {
		for _, r := range a.Rules {
			if _, ok := r.Features[feature]; ok {
				return true
			}
		}
	}
	return false
}

// GameDirOf is the --gameDir a launcher's argv names, in either of the forms Java takes it.
func GameDirOf(argv []string) (string, bool) {
	for i, arg := range argv {
		if arg == "--gameDir" && i+1 < len(argv) {
			return argv[i+1], true
		}
		if v, ok := strings.CutPrefix(arg, "--gameDir="); ok {
			return v, true
		}
	}
	return "", false
}
